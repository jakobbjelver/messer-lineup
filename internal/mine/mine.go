// Package mine runs a sweep and writes down what happened.
//
// It exists so the numbers in docs/coverage.md are output rather than recollection: one command walks a set of
// towns, asks every adapter for candidates, fetches a detail page per candidate, projects each show, merges the
// sources and — unless asked to measure only — writes an index directory. The report is the same either way.
package mine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/index"
	"github.com/jakobbjelver/messer-lineup/internal/match"
	"github.com/jakobbjelver/messer-lineup/internal/model"
	"github.com/jakobbjelver/messer-lineup/internal/resolve"
	"github.com/jakobbjelver/messer-lineup/internal/state"
	"github.com/jakobbjelver/messer-lineup/internal/venues"

	"github.com/jakobbjelver/messer-lineup/internal/clock"
)

// Adapter is the whole contract a platform has to meet. The miner does not care which platform it is talking
// to, only that a town can be listed and an event can be read — which is what keeps a new source a package
// rather than a change to the sweep.
type Adapter interface {
	ID() string
	TownCandidates(ctx context.Context, townSlug string) ([]model.Candidate, error)
	Detail(ctx context.Context, candidate model.Candidate) (model.Show, error)
}

// Expander is an adapter whose page can hold more than one event: a tour stop with two nights, a festival with five
// slots, a dance night on four dates. The miner prefers Details when it is implemented and treats each returned show
// exactly like a single Detail result — same window, same music policy, same counters, once per show.
//
// It is a separate interface rather than a changed Adapter so the two sources that return one show per page keep
// their signature, and a future source with a multi-date page has one obvious method to implement.
type Expander interface {
	Details(ctx context.Context, candidate model.Candidate) ([]model.Show, error)
}

// TownScoped is an adapter that knows which towns it covers. Without it, a source with no opinion about a town (a
// Swedish platform asked for Copenhagen) answers an error that reads like a broken run; with it, the miner skips the
// pair and the report simply does not claim coverage that does not exist.
type TownScoped interface {
	Towns() []string
}

func coversTown(adapter Adapter, slug string) bool {
	scoped, ok := adapter.(TownScoped)
	if !ok {
		return true
	}
	for _, town := range scoped.Towns() {
		if town == slug {
			return true
		}
	}
	return false
}

// detailsOf asks the adapter for every show on a page. A page that returns none and no error is a page with no event,
// which is not a failure the retry queue should spend an attempt on.
func detailsOf(ctx context.Context, adapter Adapter, candidate model.Candidate) ([]model.Show, error) {
	if expander, ok := adapter.(Expander); ok {
		return expander.Details(ctx, candidate)
	}
	show, err := adapter.Detail(ctx, candidate)
	if err != nil {
		return nil, err
	}
	return []model.Show{show}, nil
}

// Options is one run.
type Options struct {
	Adapters   []Adapter     // every platform this run reads
	Towns      []string      // platform town slugs, e.g. copenhagen, berlin
	Countries  []string      // keep only towns whose id ends in one of these (dk, se)
	DryRun     bool          // measure and report; write no index
	IndexDir   string        // where the index is written (empty: nowhere)
	Window     model.Window  // the published window; zero means the default 120 days from Now
	Prune      bool          // delete shards the new manifest does not name
	Interval   time.Duration // per-request gap handed to the client (0 = the client default)
	MaxPerTown int           // 0 means every candidate the town page shows
	Now        time.Time     // the clock, injected
	Log        io.Writer     // progress; may be nil

	// State is the memory between runs: the day's rate budget and each town's recent yield. Nil means a run
	// with no memory, which measures but learns nothing and is capped by nothing.
	State        *state.State
	BudgetPerDay int // requests per source per day; 0 means no cap

	// YieldFloor is the fraction of a town's median a sweep may fall below before the run is flagged as a
	// likely parser break (plan 1.7): a quarter is the plan's own line.
	YieldFloor float64

	// Venues is the music policy: the rooms that do not programme music, and the event forms that are never a
	// gig. Nil means no filter — a measurement of a market before its rooms are curated.
	Venues *venues.Registry

	// Resolver rewrites an artist string that carries a show title into the artist, before the merge. Nil means
	// names are published as the source spelled them.
	Resolver *resolve.Resolver
	// ResolveLimit bounds the provider calls one run may make (0 = all).
	ResolveLimit int
	// ArtistStatePath is where the resolver's decisions are saved; empty means do not persist them.
	ArtistStatePath string
}

// Run sweeps, merges and (unless DryRun) writes an index. It always returns a report when it returns at all: a
// run that died half-way is a fact worth writing down.
func Run(ctx context.Context, opts Options) (Report, error) {
	if len(opts.Towns) == 0 {
		return Report{}, fmt.Errorf("mine: no towns — no platform publishes a country index, so a run names its towns (`--town copenhagen`)")
	}
	if len(opts.Adapters) == 0 {
		return Report{}, fmt.Errorf("mine: no source — a run reads at least one platform (`--source dice`)")
	}
	if opts.Now.IsZero() {
		return Report{}, fmt.Errorf("mine: the run needs its clock")
	}
	logf := func(format string, args ...any) {
		if opts.Log != nil {
			fmt.Fprintf(opts.Log, format+"\n", args...)
		}
	}

	window := opts.Window
	if window.From.IsZero() {
		from := startOfDay(opts.Now)
		window = model.Window{From: from, To: from.AddDate(0, 0, index.DefaultWindowDays), Days: index.DefaultWindowDays}
	}

	countries := countryFilter(opts.Countries)
	report := Report{
		Sources:   adapterIDs(opts.Adapters),
		StartedAt: opts.Now.UTC(),
		DryRun:    opts.DryRun,
		Window:    window,
	}
	if countries != nil {
		report.Countries = sortedKeys(countries)
	}

	townSet := index.NewTownSet()
	var shows []model.Show

	for _, slug := range opts.Towns {
		slug = strings.TrimSpace(slug)
		if slug == "" {
			continue
		}
		town := TownReport{Slug: slug}
		for _, adapter := range opts.Adapters {
			if !coversTown(adapter, slug) {
				// Not a failure: this source does not publish that town, so there is nothing to ask it.
				continue
			}
			started := clock.Now()
			src := SourceReport{ID: adapter.ID(), Failures: map[string]int{}}

			// The yield guard reads the history *before* the sweep: a run compared against a baseline that
			// the run itself just wrote would never see a collapse.
			baseline, haveBaseline := 0.0, false
			if opts.State != nil {
				baseline, haveBaseline = opts.State.Median(adapter.ID(), slug, state.HistoryDepth)
			}

			if err := opts.take(adapter.ID()); err != nil {
				src.Warning = err.Error()
				town.Sources = append(town.Sources, src)
				logf("%-14s %-10s %v", slug, src.ID, err)
				continue
			}
			candidates, err := adapter.TownCandidates(ctx, slug)
			if err != nil {
				src.Error = err.Error()
				town.Sources = append(town.Sources, src)
				logf("%-14s %-10s error: %v", slug, src.ID, err)
				continue
			}
			if opts.MaxPerTown > 0 && len(candidates) > opts.MaxPerTown {
				candidates = candidates[:opts.MaxPerTown]
			}

			// Then whatever this town's last runs could not read for a reason that might have changed. A retry is an
			// ordinary candidate: the same projection, the same town check, the same failure accounting.
			retried := map[string]bool{}
			if opts.State != nil {
				listed := map[string]bool{}
				for _, candidate := range candidates {
					listed[candidate.URL] = true
				}
				for _, job := range opts.State.DueJobs(opts.Now, maxRetriesPerTown) {
					if job.Source != adapter.ID() || job.Town != slug || job.Target == "" {
						continue
					}
					// The target is this run's problem either way, so its failures count against the job — but if the
					// town page lists it too, fetching it twice would double the row and the budget for one show.
					retried[job.Target] = true
					src.Retried++
					if listed[job.Target] {
						continue
					}
					candidates = append([]model.Candidate{{URL: job.Target, Source: job.Source}}, candidates...)
				}
			}
			src.Candidates = len(candidates)

			for i, candidate := range candidates {
				if err := opts.take(adapter.ID()); err != nil {
					src.Warning = fmt.Sprintf("%v — stopped after %d of %d candidates", err, i, len(candidates))
					break
				}
				pageShows, derr := detailsOf(ctx, adapter, candidate)
				if derr != nil {
					if errors.Is(derr, model.ErrNotAnArtist) {
						// Parsed, but the platform used an admission label as the event name. Counted apart
						// from a failure: nothing went wrong, the row was never a show.
						src.NotAnArtist++
						continue
					}
					if errors.Is(derr, model.ErrNotMusic) {
						// The platform's own category says this is not a gig. A fact, not a failure: a retry would
						// spend a second attempt on an answer that was already correct.
						src.NotMusic++
						if src.Why == nil {
							src.Why = map[string]int{}
						}
						src.Why["category"]++
						continue
					}
					if errors.Is(derr, model.ErrGone) {
						// The sitemap still lists a link the platform no longer serves as an event. Counted, never
						// retried: the page is not coming back.
						src.Gone++
						continue
					}
					kind := classify(derr)
					src.Failures[kind]++
					if opts.State != nil {
						id := state.JobID(adapter.ID(), state.JobDetail, candidate.URL)
						switch {
						case retried[candidate.URL] && !retryable(kind):
							// It failed again, and this time for a reason that will not change: stop spending the
							// budget on it.
							opts.State.SuccessJob(id)
							src.GivenUp++
						case retried[candidate.URL]:
							if opts.State.FailJob(id, derr, opts.Now) {
								src.GivenUp++
							}
						case retryable(kind):
							if opts.State.EnqueueJob(state.Job{
								Source:    adapter.ID(),
								Kind:      state.JobDetail,
								Target:    candidate.URL,
								Town:      slug,
								NotBefore: opts.Now.Add(state.FirstRetry),
								LastError: derr.Error(),
							}) {
								src.Queued++
							}
						}
					}
					continue
				}
				if opts.State != nil && retried[candidate.URL] {
					opts.State.SuccessJob(state.JobID(adapter.ID(), state.JobDetail, candidate.URL))
				}
				for _, show := range pageShows {
					if _, perr := model.Project(show); perr != nil {
						src.Failures["not publishable: "+perr.Error()]++
						continue
					}
					if countries != nil && !countries[countryOf(show.Town)] {
						src.Filtered++
						continue
					}
					if townSlug(show.Town) != slug {
						// A platform's town page can carry events from anywhere in the country (Tickster's
						// Copenhagen list does); they belong to another town's run, not to this one.
						src.Elsewhere++
						continue
					}
					if !inWindow(show.Start, window) {
						src.Outside++
						continue
					}
					// The music policy runs last, after the window: a room that never programmes music is a fact
					// about the room, but the count is only interesting for shows this town would have published.
					unclassified := ""
					if opts.Venues != nil {
						switch verdict := opts.Venues.Decide(show.VenueKey, show.Artist); {
						case !verdict.Keep:
							src.NotMusic++
							if src.Why == nil {
								src.Why = map[string]int{}
							}
							src.Why[verdict.Reason]++
							if verdict.Reason == "venue" {
								if src.DroppedVenues == nil {
									src.DroppedVenues = map[string]int{}
								}
								src.DroppedVenues[show.Venue]++
							}
							continue
						case verdict.Reason == "unclassified":
							unclassified = show.Venue
						}
					}
					if len(show.Sources) == 0 {
						show.Sources = []string{adapter.ID()}
					}
					src.Rows++
					if unclassified != "" {
						src.Unclassified++
						if src.UnclassifiedVenues == nil {
							src.UnclassifiedVenues = map[string]int{}
						}
						src.UnclassifiedVenues[unclassified]++
					}
					shows = append(shows, show)
					townSet.Add(show)
				}
			}
			src.Seconds = time.Since(started).Seconds()
			if warn := yieldWarning(opts.yieldFloor(), baseline, haveBaseline, src); warn != "" && src.Warning == "" {
				src.Warning = warn
			}
			if opts.State != nil {
				_ = opts.State.Record(adapter.ID(), slug, state.Run{
					At:         opts.Now.UTC(),
					Day:        opts.Now.Format("2006-01-02"),
					Candidates: src.Candidates,
					Rows:       src.Rows,
				})
			}
			town.Sources = append(town.Sources, src)
			logf("%-14s %-10s %2d candidates → %2d rows (%2d not music), %2d filtered, %2d failures, %.0fs",
				slug, src.ID, src.Candidates, src.Rows, src.NotMusic, src.Filtered, failures(src), src.Seconds)
		}
		report.Towns = append(report.Towns, town)
	}

	// Names, before the merge: two spellings of the same bill must dedupe as one key, so the resolver runs on the
	// shows rather than on the rows it is about to publish.
	if opts.Resolver != nil {
		names := make([]string, len(shows))
		for index, show := range shows {
			names[index] = show.Artist
		}
		resolved, err := opts.Resolver.Resolve(ctx, names, opts.ResolveLimit)
		if err != nil {
			return report, fmt.Errorf("mine: resolving artist names: %w", err)
		}
		resolved.Applied = opts.Resolver.Apply(shows)
		report.Resolve = &resolved
		if opts.ArtistStatePath != "" {
			if err := opts.Resolver.State.Save(opts.ArtistStatePath); err != nil {
				return report, fmt.Errorf("mine: saving artist decisions: %w", err)
			}
		}
		logf("resolve %d lookup(s) · %d resolved · %d undecided · %d row(s) rewritten",
			resolved.Lookups, resolved.Resolved, resolved.Undecided, resolved.Applied)
	}

	merged, mergeStats := match.Merge(shows)
	report.Merge = mergeStats
	report.TownCount = townSet.Len()
	report.Totals = totalsOf(report.Towns)

	if !opts.DryRun {
		if opts.IndexDir == "" {
			return report, fmt.Errorf("mine: asked to write but no index directory was given")
		}
		manifest, buildStats, err := index.Write(opts.IndexDir, merged, index.WriteOptions{
			BuiltAt: opts.Now.UTC(),
			Window:  window,
			Sources: sourceHealth(report.Towns, merged),
			Towns:   townSet.Towns(opts.Now.UTC()).Towns,
			Prune:   opts.Prune,
		})
		if err != nil {
			return report, fmt.Errorf("mine: writing the index: %w", err)
		}
		report.Build = &BuildReport{
			Dir:       opts.IndexDir,
			Shards:    buildStats.Shards,
			Bytes:     buildStats.Bytes,
			Published: buildStats.Published,
			Outside:   buildStats.Outside,
			Refused:   buildStats.Refused,
			Towns:     len(manifest.Towns),
		}
		logf("wrote %s: %d shard(s), %d rows, %d bytes", opts.IndexDir, buildStats.Shards, buildStats.Published, buildStats.Bytes)
	} else {
		report.Note = "dry run: nothing written. Drop --dry-run (and name --index) to publish the index."
	}
	report.FinishedAt = clock.Now().UTC()
	if report.FinishedAt.Before(report.StartedAt) {
		report.FinishedAt = report.StartedAt
	}
	return report, nil
}

// Report is one run, in the shape the coverage record is rendered from.
type Report struct {
	Sources    []string        `json:"sources"`
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt time.Time       `json:"finished_at"`
	DryRun     bool            `json:"dry_run"`
	Countries  []string        `json:"countries,omitempty"`
	Window     model.Window    `json:"window"`
	Towns      []TownReport    `json:"towns"`
	TownCount  int             `json:"towns_discovered"`
	Totals     Totals          `json:"totals"`
	Merge      match.Stats     `json:"merge"`
	Resolve    *resolve.Report `json:"resolve,omitempty"`
	Build      *BuildReport    `json:"build,omitempty"`
	Note       string          `json:"note,omitempty"`
}

// TownReport is one town's part of a run, per source.
type TownReport struct {
	Slug    string         `json:"slug"`
	Sources []SourceReport `json:"sources"`
}

// SourceReport is one adapter's contribution to one town.
type SourceReport struct {
	ID          string         `json:"id"`
	Candidates  int            `json:"candidates"`
	Rows        int            `json:"rows"`
	Filtered    int            `json:"filtered"`
	Failures    map[string]int `json:"failures,omitempty"`
	Outside     int            `json:"outside_window"`
	NotAnArtist int            `json:"not_an_artist"`
	Retried     int            `json:"retried,omitempty"`  // queued targets this run picked up
	Queued      int            `json:"queued,omitempty"`   // new failures worth another attempt
	GivenUp     int            `json:"given_up,omitempty"` // retries that failed for good
	Elsewhere   int            `json:"other_town"`
	Seconds     float64        `json:"seconds"`
	Error       string         `json:"error,omitempty"`

	// NotMusic is what the music policy dropped, and Why says by which rule: "venue" for a room whose
	// programme is not music, "form:<token>" for an event form that is never a gig.
	NotMusic int            `json:"not_music,omitempty"`
	Why      map[string]int `json:"not_music_why,omitempty"`
	// Gone is a link a platform still lists but no longer serves as an event. A fact, not a failure.
	Gone int `json:"gone,omitempty"`
	// DroppedVenues is the curation list: which rooms lost rows to the venue rule, and how many.
	DroppedVenues map[string]int `json:"dropped_venues,omitempty"`
	// Unclassified is rows kept at a room the registry has never seen. They are published, and they are the
	// next curation pass rather than a silent hole.
	Unclassified       int            `json:"unclassified,omitempty"`
	UnclassifiedVenues map[string]int `json:"unclassified_venues,omitempty"`
	// Warning is a run that completed but should not be trusted: the budget ran out, or the yield collapsed
	// against this town's own median. A warning is never a reason to publish less than what was mined; it is
	// the reason to look.
	Warning string `json:"warning,omitempty"`
}

// BuildReport is what writing the index did.
type BuildReport struct {
	Dir       string `json:"dir"`
	Shards    int    `json:"shards"`
	Bytes     int    `json:"bytes"`
	Published int    `json:"published"`
	Outside   int    `json:"outside_window"`
	Refused   int    `json:"refused"`
	Towns     int    `json:"towns"`
}

// Totals is the run in one line.
type Totals struct {
	Candidates   int `json:"candidates"`
	Rows         int `json:"rows"`
	Filtered     int `json:"filtered"`
	Outside      int `json:"outside_window"`
	Elsewhere    int `json:"other_town"`
	NotAnArtist  int `json:"not_an_artist"`
	NotMusic     int `json:"not_music"`
	Gone         int `json:"gone"`
	Unclassified int `json:"unclassified"`
	Failures     int `json:"failures"`
	Retried      int `json:"retried"`
	Queued       int `json:"queued"`
	GivenUp      int `json:"given_up"`
}

// WriteJSON writes the report where a script or an agent can read it.
func (r Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteFile writes the report to path.
func (r Report) WriteFile(path string) error {
	if path == "" {
		return nil
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return r.WriteJSON(f)
}

func totalsOf(towns []TownReport) Totals {
	var totals Totals
	for _, town := range towns {
		for _, src := range town.Sources {
			totals.Candidates += src.Candidates
			totals.Rows += src.Rows
			totals.Filtered += src.Filtered
			totals.NotAnArtist += src.NotAnArtist
			totals.NotMusic += src.NotMusic
			totals.Gone += src.Gone
			totals.Unclassified += src.Unclassified
			totals.Outside += src.Outside
			totals.Elsewhere += src.Elsewhere
			totals.Failures += failures(src)
			totals.Retried += src.Retried
			totals.Queued += src.Queued
			totals.GivenUp += src.GivenUp
		}
	}
	return totals
}

// sourceHealth is the manifest's per-source block: whether it ran, what it yielded, and how many rows survived
// the merge. The coverage line is rendered from this, so a source that is off says so.
func sourceHealth(towns []TownReport, merged []model.Show) []model.Source {
	rows := map[string]int{}
	for _, show := range merged {
		for _, id := range show.Sources {
			rows[id]++
		}
	}
	byID := map[string]*model.Source{}
	for _, town := range towns {
		for _, src := range town.Sources {
			entry, ok := byID[src.ID]
			if !ok {
				entry = &model.Source{ID: src.ID, Index: "sitemap", OK: true}
				byID[src.ID] = entry
			}
			if src.Error != "" {
				entry.OK = false
			}
			entry.Rows += src.Rows
			if src.Candidates > 0 {
				yield := float64(src.Rows) / float64(src.Candidates)
				if entry.ParseYield == 0 || yield < entry.ParseYield {
					entry.ParseYield = yield
				}
			}
		}
	}
	out := make([]model.Source, 0, len(byID))
	for id, entry := range byID {
		entry.LastOK = clock.Now().UTC()
		if n := rows[id]; n > 0 {
			entry.Rows = n
		}
		out = append(out, *entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// take charges one request against the day's budget. A run with no state has no cap, by definition.
func (o Options) take(source string) error {
	if o.State == nil || o.BudgetPerDay <= 0 {
		return nil
	}
	return o.State.Take(source, o.Now, o.BudgetPerDay)
}

func (o Options) yieldFloor() float64 {
	if o.YieldFloor <= 0 {
		return 0.25
	}
	return o.YieldFloor
}

// yieldWarning is the parse-yield guard: zero rows, or a quarter of the town's own median, means the page
// changed shape or the source is answering something else — the failure mode that looks like success.
func yieldWarning(floor, baseline float64, haveBaseline bool, src SourceReport) string {
	if src.Candidates == 0 {
		return ""
	}
	if src.Rows == 0 {
		return fmt.Sprintf("parse-yield: 0 rows from %d candidates — the page shape may have changed", src.Candidates)
	}
	if haveBaseline && baseline > 0 && float64(src.Rows) < floor*baseline {
		return fmt.Sprintf("parse-yield: %d rows against a median of %.0f (%.0f %%)", src.Rows, baseline, 100*float64(src.Rows)/baseline)
	}
	return ""
}

func failures(s SourceReport) int {
	n := 0
	for _, count := range s.Failures {
		n += count
	}
	return n
}

func adapterIDs(adapters []Adapter) []string {
	out := make([]string, 0, len(adapters))
	for _, adapter := range adapters {
		out = append(out, adapter.ID())
	}
	sort.Strings(out)
	return out
}

// inWindow is the publication window, date-only: a rolling 120 days is a range of days, not of instants.
func inWindow(start time.Time, w model.Window) bool {
	if start.IsZero() || w.From.IsZero() {
		return false
	}
	day := start.Format("2006-01-02")
	return day >= w.From.Format("2006-01-02") && day <= w.To.Format("2006-01-02")
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func countryFilter(list []string) map[string]bool {
	var out map[string]bool
	for _, raw := range list {
		for _, part := range strings.Split(raw, ",") {
			part = strings.ToLower(strings.TrimSpace(part))
			if part == "" {
				continue
			}
			if out == nil {
				out = map[string]bool{}
			}
			out[part] = true
		}
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// townSlug strips the country off a town id, so a run's `--town copenhagen` matches `copenhagen-dk`.
func townSlug(townID string) string {
	if i := strings.LastIndex(townID, "-"); i > 0 {
		return townID[:i]
	}
	return townID
}

func countryOf(townID string) string {
	i := strings.LastIndex(townID, "-")
	if i < 0 || i == len(townID)-1 {
		return ""
	}
	return strings.ToLower(townID[i+1:])
}

// classify names why a page did not become a row, so a report distinguishes "the host refused us" from "the
// page had no event". A run that loses pages and cannot say why is not a measurement.
// QueueNote is queueNote for the CLI's own table: the retry accounting belongs on a town's line, and nowhere when the
// queue stayed out of the way.
func QueueNote(src SourceReport) string { return queueNote(src) }

// queueNote appends the retry accounting to a town's line, and says nothing when the queue stayed out of the way.
func queueNote(src SourceReport) string {
	if src.Retried == 0 && src.Queued == 0 && src.GivenUp == 0 {
		return ""
	}
	return fmt.Sprintf(" · %d retried, %d queued, %d given up", src.Retried, src.Queued, src.GivenUp)
}

// maxRetriesPerTown caps how many queued targets one town's run picks up, so a bad week cannot turn a sweep into a
// retry loop that crowds out the fresh candidates.
const maxRetriesPerTown = 50

// retryable says whether a failed fetch is worth another attempt. A 429 or a timeout is a bad minute; a 404, a
// robots refusal, a 403 or a page with no event block is a fact about the target, and re-fetching it spends budget to
// learn nothing.
func retryable(kind string) bool {
	switch {
	case strings.HasPrefix(kind, "rate limited"):
		return true
	case strings.Contains(kind, "timeout"), strings.Contains(kind, "connection"), strings.Contains(kind, "temporary"),
		strings.Contains(kind, "answered 5"), strings.Contains(kind, "answered 502"), strings.Contains(kind, "answered 503"):
		return true
	}
	return false
}

func classify(err error) string {
	if err == nil {
		return "ok"
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "disallowed by robots"):
		return "robots"
	case strings.Contains(msg, "answered 404"), strings.Contains(msg, "answered 410"):
		return "gone (404/410)"
	case strings.Contains(msg, "answered 403"):
		return "blocked (403)"
	case strings.Contains(msg, "answered 429"):
		return "rate limited (429)"
	case strings.Contains(msg, "no schema.org event"), strings.Contains(msg, "no event block"):
		return "no event block"
	case strings.Contains(msg, "start date"), strings.Contains(msg, "startDate"):
		return "no start date"
	case strings.Contains(msg, "town"):
		return "no town"
	case strings.Contains(msg, "venue"):
		return "no venue"
	case strings.Contains(msg, "context deadline"), strings.Contains(msg, "timeout"):
		return "timeout"
	}
	return "other: " + msg
}
