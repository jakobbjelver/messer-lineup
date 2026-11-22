package mine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/index"
	"github.com/jakobbjelver/messer-lineup/internal/match"
	"github.com/jakobbjelver/messer-lineup/internal/model"
	"github.com/jakobbjelver/messer-lineup/internal/resolve"
	"github.com/jakobbjelver/messer-lineup/internal/state"
	"github.com/jakobbjelver/messer-lineup/internal/venues"

	"github.com/jakobbjelver/messer-lineup/internal/clock"
)

// fakeAdapter is a platform in a map: a town lists candidates, a candidate URL is a show or an error.
type fakeAdapter struct {
	id      string
	towns   map[string][]string // slug → candidate URLs
	shows   map[string]model.Show
	failure map[string]error
}

func (f *fakeAdapter) ID() string { return f.id }

func (f *fakeAdapter) TownCandidates(_ context.Context, slug string) ([]model.Candidate, error) {
	urls, ok := f.towns[slug]
	if !ok {
		return nil, errors.New("answered 404")
	}
	out := make([]model.Candidate, 0, len(urls))
	for _, u := range urls {
		out = append(out, model.Candidate{URL: u, Source: f.id})
	}
	return out, nil
}

func (f *fakeAdapter) Detail(_ context.Context, c model.Candidate) (model.Show, error) {
	if err, ok := f.failure[c.URL]; ok {
		return model.Show{}, err
	}
	show, ok := f.shows[c.URL]
	if !ok {
		return model.Show{}, errors.New("no schema.org event")
	}
	return show, nil
}

func showAt(url, artist, venue, town string, day time.Time, src string) model.Show {
	return model.Show{
		Artist:    artist,
		ArtistKey: model.Key(artist),
		Venue:     venue,
		VenueKey:  model.Key(venue),
		Town:      town,
		Start:     day,
		Status:    model.OnSale,
		URL:       url,
		Sources:   []string{src},
	}
}

func baseAdapter() *fakeAdapter {
	day := time.Date(2026, 12, 2, 20, 0, 0, 0, time.UTC)
	return &fakeAdapter{
		id: "dice",
		towns: map[string][]string{
			"berlin":     {"https://d.test/b1", "https://d.test/b2"},
			"london":     {"https://d.test/l1", "https://d.test/l2"},
			"copenhagen": {"https://d.test/c1", "https://d.test/c2"},
		},
		shows: map[string]model.Show{
			"https://d.test/b1": showAt("https://d.test/b1", "Anna von Hausswolff", "Silent Green", "berlin-de", day, "dice"),
			"https://d.test/b2": showAt("https://d.test/b2", "Efterklang", "Kantine am Berghain", "berlin-de", day, "dice"),
			"https://d.test/l1": showAt("https://d.test/l1", "Trentemøller", "Brixton Academy", "london-gb", day, "dice"),
			"https://d.test/c1": showAt("https://d.test/c1", "Anna von Hausswolff", "Vega", "copenhagen-dk", day, "dice"),
			"https://d.test/c2": showAt("https://d.test/c2", "Efterklang", "Pumpehuset", "copenhagen-dk", day, "dice"),
		},
		failure: map[string]error{"https://d.test/l2": errors.New("the host answered 403")},
	}
}

func run(t *testing.T, opts Options) Report {
	t.Helper()
	if opts.Now.IsZero() {
		opts.Now = time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC)
	}
	report, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return report
}

func TestRunMeasuresAndReports(t *testing.T) {
	report := run(t, Options{
		Adapters: []Adapter{baseAdapter()},
		Towns:    []string{"berlin", "london"},
		DryRun:   true,
	})
	if report.Totals.Candidates != 4 || report.Totals.Rows != 3 {
		t.Fatalf("totals = %+v, want 4 candidates and 3 rows", report.Totals)
	}
	if report.Totals.Failures != 1 {
		t.Fatalf("failures = %d, want the one page that answered 403", report.Totals.Failures)
	}
	if report.TownCount != 2 {
		t.Fatalf("towns discovered = %d, want 2", report.TownCount)
	}
	if len(report.Sources) != 1 || report.Sources[0] != "dice" {
		t.Fatalf("sources = %v", report.Sources)
	}
	if report.Window.Days != index.DefaultWindowDays || !report.Window.From.Equal(time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("window = %+v", report.Window)
	}
	if report.Note == "" {
		t.Fatal("a dry run must say that nothing was written")
	}
	if report.Build != nil {
		t.Fatal("a dry run wrote a build")
	}
}

func TestRunNamesWhyPagesWereLost(t *testing.T) {
	fake := baseAdapter()
	report := run(t, Options{
		Adapters: []Adapter{fake},
		Towns:    []string{"nowhere", "berlin"},
		DryRun:   true,
	})
	var nowhere *SourceReport
	for i := range report.Towns {
		if report.Towns[i].Slug == "nowhere" {
			nowhere = &report.Towns[i].Sources[0]
		}
	}
	if nowhere == nil || nowhere.Error == "" {
		t.Fatalf("a town page that could not be read must carry the reason: %+v", report.Towns)
	}
	for _, town := range report.Towns {
		if town.Slug != "london" {
			continue
		}
		if got := town.Sources[0].Failures["blocked (403)"]; got != 1 {
			t.Fatalf("failures = %v, want one blocked page", town.Sources[0].Failures)
		}
	}
}

func TestRunMergesTwoSources(t *testing.T) {
	dice := baseAdapter()
	tickster := &fakeAdapter{
		id: "tickster",
		towns: map[string][]string{
			// The same two gigs, spelled the same way, plus one the other source does not have.
			"copenhagen": {"https://t.test/1", "https://t.test/2", "https://t.test/3"},
		},
		shows: map[string]model.Show{
			"https://t.test/1": showAt("https://t.test/1", "Anna von Hausswolff", "Vega", "copenhagen-dk", time.Date(2026, 12, 2, 20, 0, 0, 0, time.UTC), "tickster"),
			"https://t.test/2": showAt("https://t.test/2", "Efterklang", "Pumpehuset", "copenhagen-dk", time.Date(2026, 12, 2, 20, 0, 0, 0, time.UTC), "tickster"),
			"https://t.test/3": showAt("https://t.test/3", "Kalaha", "Ideal Bar", "copenhagen-dk", time.Date(2026, 12, 4, 20, 0, 0, 0, time.UTC), "tickster"),
		},
	}
	dir := t.TempDir()
	report := run(t, Options{
		Adapters: []Adapter{dice, tickster},
		Towns:    []string{"copenhagen"},
		IndexDir: dir,
		Prune:    true,
	})
	if report.Merge.Raw != 5 || report.Merge.Out != 3 {
		t.Fatalf("merge = %+v, want 5 raw (2 dice + 3 tickster) → 3 shows", report.Merge)
	}
	if report.Build == nil || report.Build.Shards == 0 {
		t.Fatalf("build = %+v", report.Build)
	}

	// The loop the product depends on: what mine wrote, verify accepts.
	verify, err := index.Verify(dir, time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !verify.OK() {
		t.Fatalf("verify refused the index mine wrote: %v", verify.Problems)
	}
	manifest, err := index.ReadManifest(dir)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if len(manifest.Sources) != 2 {
		t.Fatalf("manifest sources = %+v, want both platforms", manifest.Sources)
	}
	var cpIndex *model.Index
	for i := range manifest.Towns {
		if manifest.Towns[i].Town == "copenhagen-dk" {
			cpIndex = &manifest.Towns[i]
		}
	}
	if cpIndex == nil {
		t.Fatalf("manifest has no copenhagen shard: %+v", manifest.Towns)
	}
	rows, err := index.ReadShard(filepath.Join(dir, cpIndex.Shard))
	if err != nil {
		t.Fatalf("read shard: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("copenhagen rows = %d, want three (two merged pairs and the one show only one source had)", len(rows))
	}
	both := 0
	for _, row := range rows {
		switch len(row.Src) {
		case 2:
			both++
		case 1:
			if row.Src[0] != "tickster" {
				t.Fatalf("row %s carries only %v", row.K, row.Src)
			}
		default:
			t.Fatalf("row %s carries sources %v", row.K, row.Src)
		}
	}
	if both != 2 {
		t.Fatalf("%d rows carry both sources, want 2", both)
	}
}

func TestRunCountryFilter(t *testing.T) {
	report := run(t, Options{
		Adapters:  []Adapter{baseAdapter()},
		Towns:     []string{"berlin", "copenhagen"},
		Countries: []string{"dk"},
		DryRun:    true,
	})
	if report.Totals.Rows != 2 || report.Totals.Filtered != 2 {
		t.Fatalf("with --country dk: %+v, want 2 rows and 2 filtered", report.Totals)
	}
	if report.TownCount != 1 {
		t.Fatalf("towns discovered = %d, want only copenhagen", report.TownCount)
	}
	if len(report.Countries) != 1 || report.Countries[0] != "dk" {
		t.Fatalf("countries = %v", report.Countries)
	}
}

func TestRunRefusesARunWithNoTownsOrNoSource(t *testing.T) {
	if _, err := Run(context.Background(), Options{Adapters: []Adapter{baseAdapter()}, Now: clock.Now()}); err == nil {
		t.Fatal("a run with no towns must say so, because no platform has a country index")
	}
	if _, err := Run(context.Background(), Options{Towns: []string{"berlin"}, Now: clock.Now()}); err == nil {
		t.Fatal("a run with no source must say so")
	}
}

func TestRunRefusesToWriteWithoutADirectory(t *testing.T) {
	if _, err := Run(context.Background(), Options{
		Adapters: []Adapter{baseAdapter()},
		Towns:    []string{"berlin"},
		Now:      time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC),
	}); err == nil {
		t.Fatal("a non-dry run with no index directory must fail rather than pretend")
	}
}

func TestReportJSONRoundTrip(t *testing.T) {
	report := run(t, Options{Adapters: []Adapter{baseAdapter()}, Towns: []string{"berlin"}, DryRun: true})
	path := filepath.Join(t.TempDir(), "report.json")
	if err := report.WriteFile(path); err != nil {
		t.Fatalf("write: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var back Report
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if back.Totals.Rows != report.Totals.Rows || len(back.Towns) != len(report.Towns) {
		t.Fatalf("round trip lost data: %+v", back)
	}
	if back.Window.Days != report.Window.Days {
		t.Fatalf("window lost: %+v", back.Window)
	}
}

func TestCoverageBlockAndRewrite(t *testing.T) {
	report := Report{
		Sources:   []string{"dice", "tickster"},
		DryRun:    true,
		StartedAt: time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC),
		Towns: []TownReport{
			{Slug: "berlin", Sources: []SourceReport{{ID: "dice", Candidates: 30, Rows: 30, Seconds: 31}}},
			{Slug: "copenhagen", Sources: []SourceReport{
				{ID: "dice", Error: "answered 404"},
				{ID: "tickster", Candidates: 30, Rows: 28, Filtered: 1, Failures: map[string]int{"gone (404/410)": 1}, Seconds: 61},
			}},
		},
		Totals:    Totals{Candidates: 60, Rows: 58, Filtered: 1, Failures: 1},
		Merge:     match.Stats{Raw: 58, Out: 50, Merged: 8, DuplicateRate: 8.0 / 58},
		TownCount: 2,
		Note:      "dry run: nothing written. Drop --dry-run (and name --index) to publish the index.",
	}
	block := RenderCoverageBlock(report)
	for _, want := range []string{
		CoverageBegin, CoverageEnd,
		"| berlin | dice | 30 | 30 | 0 |",
		"| copenhagen | tickster | 30 | 28 | 0 | 1 | 1 |",
		"| **total** | | **60** | **58** | **0** |",
		"duplicate rate **13.8 %**",
		"gone (404/410) ×1",
		"Towns discovered: **2**",
		"`copenhagen` on `dice` could not be read: answered 404",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("block is missing %q:\n%s", want, block)
		}
	}

	path := filepath.Join(t.TempDir(), "coverage.md")
	prose := "# Coverage\n\nHand-written prose that must survive.\n\n" + CoverageBegin + "\nstale\n" + CoverageEnd + "\n\nMore prose.\n"
	if err := os.WriteFile(path, []byte(prose), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := UpdateCoverageFile(path, report); err != nil {
		t.Fatalf("update: %v", err)
	}
	updated, _ := os.ReadFile(path)
	if !strings.Contains(string(updated), "Hand-written prose that must survive.") ||
		!strings.Contains(string(updated), "More prose.") ||
		strings.Contains(string(updated), "stale") {
		t.Fatalf("rewrite damaged the file:\n%s", updated)
	}
	plain := filepath.Join(t.TempDir(), "plain.md")
	_ = os.WriteFile(plain, []byte("# nothing marked here\n"), 0o644)
	if err := UpdateCoverageFile(plain, report); err == nil {
		t.Fatal("a file with no markers was rewritten")
	}
}

func TestBudgetStopsARunAndSaysSo(t *testing.T) {
	mem, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	// A cap of two: one town page plus one detail, so the second candidate is never fetched.
	report := run(t, Options{
		Adapters:     []Adapter{baseAdapter()},
		Towns:        []string{"berlin"},
		DryRun:       true,
		State:        mem,
		BudgetPerDay: 2,
	})
	src := report.Towns[0].Sources[0]
	if src.Candidates != 2 || src.Rows != 1 {
		t.Fatalf("source = %+v, want a run cut short after the cap", src)
	}
	if !strings.Contains(src.Warning, "stopped after") {
		t.Fatalf("warning = %q, want the reason the sweep ended early", src.Warning)
	}
	// The next run the same day is refused outright rather than silently doing nothing.
	second := run(t, Options{
		Adapters:     []Adapter{baseAdapter()},
		Towns:        []string{"london"},
		DryRun:       true,
		State:        mem,
		BudgetPerDay: 2,
	})
	if !strings.Contains(second.Towns[0].Sources[0].Warning, "used its 2 requests") {
		t.Fatalf("second run = %+v, want a refusal", second.Towns[0].Sources[0])
	}
}

func TestYieldGuardFlagsACollapse(t *testing.T) {
	mem, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	// A baseline the town has earned: three healthy sweeps.
	for i := 0; i < 3; i++ {
		if err := mem.Record("dice", "berlin", state.Run{Day: "2026-11-14", Candidates: 2, Rows: 2}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	empty := baseAdapter()
	empty.shows = map[string]model.Show{} // every detail page stops parsing: the shape changed
	report := run(t, Options{
		Adapters: []Adapter{empty},
		Towns:    []string{"berlin"},
		DryRun:   true,
		State:    mem,
	})
	src := report.Towns[0].Sources[0]
	if src.Rows != 0 {
		t.Fatalf("rows = %d", src.Rows)
	}
	if !strings.Contains(src.Warning, "parse-yield: 0 rows") {
		t.Fatalf("warning = %q, want a flagged collapse", src.Warning)
	}
}

func TestYieldGuardStaysQuietWithoutABaseline(t *testing.T) {
	mem, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	// One run is not a baseline: the guard must not cry collapse on a town's first sweep.
	report := run(t, Options{Adapters: []Adapter{baseAdapter()}, Towns: []string{"berlin"}, DryRun: true, State: mem})
	if w := report.Towns[0].Sources[0].Warning; w != "" {
		t.Fatalf("warning on a first run: %q", w)
	}
	// A healthy second run against a small baseline is silent too.
	report = run(t, Options{Adapters: []Adapter{baseAdapter()}, Towns: []string{"berlin"}, DryRun: true, State: mem})
	if w := report.Towns[0].Sources[0].Warning; w != "" {
		t.Fatalf("warning on a healthy run: %q", w)
	}
}

func TestARunWithHistorySurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	mem, err := state.Open(path)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	run(t, Options{Adapters: []Adapter{baseAdapter()}, Towns: []string{"berlin"}, DryRun: true, State: mem})
	reopened, err := state.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := len(reopened.History("dice", "berlin")); got != 1 {
		t.Fatalf("history = %d entries after restart, want 1", got)
	}
}

func TestATicketLabelIsCountedApartFromAFailure(t *testing.T) {
	// A detail that parses but is not a show must not appear as a failure: nothing went wrong, and a report that
	// mixes the two makes a platform look broken when it is merely selling admission.
	dir := t.TempDir()
	day := time.Date(2026, 12, 2, 20, 0, 0, 0, time.UTC)
	fake := &fakeAdapter{
		id:      "fake",
		towns:   map[string][]string{"copenhagen": {"https://t.test/a", "https://t.test/b"}},
		shows:   map[string]model.Show{"https://t.test/a": showAt("https://t.test/a", "Efterklang", "Vega", "copenhagen-dk", day, "fake")},
		failure: map[string]error{"https://t.test/b": model.ErrNotAnArtist},
	}
	report, err := Run(context.Background(), Options{
		Adapters: []Adapter{fake},
		Towns:    []string{"copenhagen"},
		IndexDir: filepath.Join(dir, "index"),
		Now:      time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	src := report.Towns[0].Sources[0]
	if src.NotAnArtist != 1 {
		t.Fatalf("not_an_artist = %d, want 1", src.NotAnArtist)
	}
	if len(src.Failures) != 0 {
		t.Fatalf("a ticket label was counted as a failure: %v", src.Failures)
	}
	if src.Rows != 1 {
		t.Fatalf("rows = %d, want 1", src.Rows)
	}
}

// TestATransientFailureIsRetriedOnTheNextRun is the queue's whole reason to exist: a page that answered 429 is not a
// show the town does not have, it is a show this run could not read.
func TestATransientFailureIsRetriedOnTheNextRun(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	opened, err := state.Open(statePath)
	if err != nil {
		t.Fatalf("state: %v", err)
	}

	adapter := baseAdapter()
	adapter.towns["copenhagen"] = []string{"https://d.test/c1", "https://d.test/c2"}
	adapter.failure = map[string]error{"https://d.test/c2": errors.New("the host answered 429")}

	first := run(t, Options{Adapters: []Adapter{adapter}, Towns: []string{"copenhagen"}, DryRun: true, State: opened, Now: time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC)})
	if first.Totals.Rows != 1 {
		t.Fatalf("first run rows = %d, want 1 (the other page was rate limited)", first.Totals.Rows)
	}
	if first.Totals.Queued != 1 {
		t.Fatalf("first run queued = %d, want 1", first.Totals.Queued)
	}
	if due, waiting, total := opened.JobStats(time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC)); due != 0 || waiting != 1 || total != 1 {
		t.Fatalf("queue after the first run = %d due, %d waiting, %d total", due, waiting, total)
	}

	// A permanent failure is a fact and must not be queued: the 403 town proves it.
	permanent := baseAdapter()
	blocked := run(t, Options{Adapters: []Adapter{permanent}, Towns: []string{"london"}, DryRun: true, State: opened, Now: time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC)})
	if blocked.Totals.Queued != 0 {
		t.Fatalf("a 403 was queued (%d): a blocked page is not a bad minute", blocked.Totals.Queued)
	}

	// The platform recovers; six hours later the retry finds the show.
	adapter.failure = map[string]error{}
	later := time.Date(2026, 11, 17, 13, 0, 0, 0, time.UTC)
	second := run(t, Options{Adapters: []Adapter{adapter}, Towns: []string{"copenhagen"}, DryRun: true, State: opened, Now: later})
	if second.Totals.Retried != 1 {
		t.Fatalf("second run retried = %d, want 1", second.Totals.Retried)
	}
	if second.Totals.Rows != 2 {
		t.Fatalf("second run rows = %d, want 2 (the retry recovered the page)", second.Totals.Rows)
	}
	if _, _, total := opened.JobStats(later); total != 0 {
		t.Fatalf("the queue still holds %d jobs after a successful retry", total)
	}
}

// TestARetryThatKeepsFailingIsGivenUp keeps the budget honest: five attempts, then the target is a loss rather than a
// permanent tax on every run.
func TestARetryThatKeepsFailingIsGivenUp(t *testing.T) {
	opened, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	adapter := baseAdapter()
	adapter.towns["copenhagen"] = []string{"https://d.test/c1"}
	adapter.failure = map[string]error{"https://d.test/c1": errors.New("the host answered 503")}

	now := time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC)
	givenUp := 0
	for round := 0; round < state.MaxJobAttempts+1; round++ {
		report := run(t, Options{Adapters: []Adapter{adapter}, Towns: []string{"copenhagen"}, DryRun: true, State: opened, Now: now})
		givenUp += report.Totals.GivenUp
		now = now.Add(24 * time.Hour)
	}
	if givenUp != 1 {
		t.Fatalf("given up = %d, want exactly 1", givenUp)
	}
	if _, _, total := opened.JobStats(now); total != 0 {
		t.Fatalf("the queue still holds %d jobs after the attempts ran out", total)
	}
}

// The music policy is part of the sweep, not a client-side filter: a theatre's rows never reach the index,
// and a room nobody has classified is published and counted so the curation has a work list.
func TestRunAppliesTheMusicPolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "se.json")
	registry := `{"format":"lineup-venues/1","market":"se","venues":[
		{"name":"Debaser Strand","class":"music"},
		{"name":"Orionteatern","class":"other"}]}`
	if err := os.WriteFile(path, []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}
	rooms, err := venues.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	day := time.Date(2026, 12, 2, 20, 0, 0, 0, time.UTC)
	fake := &fakeAdapter{
		id:    "tickster",
		towns: map[string][]string{"stockholm": {"u1", "u2", "u3", "u4"}},
		shows: map[string]model.Show{
			"u1": showAt("u1", "Viagra Boys", "Debaser Strand", "stockholm-se", day, "tickster"),
			"u2": showAt("u2", "Fairy Queen", "Orionteatern", "stockholm-se", day, "tickster"),
			"u3": showAt("u3", "Julbord - eftermiddag", "Debaser Strand", "stockholm-se", day, "tickster"),
			"u4": showAt("u4", "Nytt Band", "Nya Klubben", "stockholm-se", day, "tickster"),
		},
	}
	report := run(t, Options{
		Adapters: []Adapter{fake},
		Towns:    []string{"stockholm"},
		DryRun:   true,
		Venues:   rooms,
	})
	if report.Totals.Rows != 2 {
		t.Fatalf("rows = %d, want the gig and the unclassified room (2)", report.Totals.Rows)
	}
	if report.Totals.NotMusic != 2 {
		t.Fatalf("not music = %d, want the theatre and the julbord (2)", report.Totals.NotMusic)
	}
	if report.Totals.Unclassified != 1 {
		t.Fatalf("unclassified = %d, want 1", report.Totals.Unclassified)
	}
	src := report.Towns[0].Sources[0]
	if src.Why["venue"] != 1 || src.Why["form:julbord"] != 1 {
		t.Fatalf("why = %v", src.Why)
	}
	if src.DroppedVenues["Orionteatern"] != 1 {
		t.Fatalf("dropped venues = %v", src.DroppedVenues)
	}
	if src.UnclassifiedVenues["Nya Klubben"] != 1 {
		t.Fatalf("unclassified venues = %v", src.UnclassifiedVenues)
	}
}

// expandingAdapter is a platform whose page carries more than one date: the shape a ticketing platform publishes for a
// tour stop with two nights, or a festival with several slots.
type expandingAdapter struct {
	*fakeAdapter
	pages map[string][]model.Show
}

func (e *expandingAdapter) Details(_ context.Context, c model.Candidate) ([]model.Show, error) {
	shows, ok := e.pages[c.URL]
	if !ok {
		return nil, errors.New("no event")
	}
	return shows, nil
}

func TestExpanderKeepsEveryDateOnOnePage(t *testing.T) {
	day := time.Date(2026, 12, 2, 20, 0, 0, 0, time.UTC)
	next := day.AddDate(0, 0, 1)
	adapter := &expandingAdapter{
		fakeAdapter: &fakeAdapter{
			id:    "nortic",
			towns: map[string][]string{"berlin": {"https://n.test/e1"}},
		},
		pages: map[string][]model.Show{
			"https://n.test/e1": {
				showAt("https://n.test/e1", "KISS", "Stadionmässan", "berlin-de", day, "nortic"),
				showAt("https://n.test/e1", "KISS", "Stadionmässan", "berlin-de", next, "nortic"),
			},
		},
	}
	report := run(t, Options{Adapters: []Adapter{adapter}, Towns: []string{"berlin"}, DryRun: true})
	if report.Totals.Candidates != 1 || report.Totals.Rows != 2 {
		t.Fatalf("totals = %+v, want 1 candidate and 2 rows", report.Totals)
	}
}

// A page the platform itself labels as theatre is a fact about the inventory, not a failure: the retry queue must not
// spend a second attempt on an answer that was already correct.
func TestNotMusicIsCountedAsAFact(t *testing.T) {
	adapter := &fakeAdapter{
		id:      "nortic",
		towns:   map[string][]string{"berlin": {"https://n.test/h1"}},
		failure: map[string]error{"https://n.test/h1": model.ErrNotMusic},
	}
	report := run(t, Options{Adapters: []Adapter{adapter}, Towns: []string{"berlin"}, DryRun: true})
	if report.Totals.Failures != 0 {
		t.Fatalf("failures = %d, want 0", report.Totals.Failures)
	}
	if report.Totals.NotMusic != 1 {
		t.Fatalf("not music = %d, want 1", report.Totals.NotMusic)
	}
	if report.Totals.Queued != 0 {
		t.Fatalf("queued = %d, want 0 (a fact is not retried)", report.Totals.Queued)
	}
}

// scopedAdapter covers one town; a run over another must not ask it, and must not report it as broken.
type scopedAdapter struct{ *fakeAdapter }

func (s *scopedAdapter) Towns() []string { return []string{"berlin"} }

func TestScopedSourceSkipsTownsItDoesNotCover(t *testing.T) {
	adapter := &scopedAdapter{baseAdapter()}
	report := run(t, Options{Adapters: []Adapter{adapter}, Towns: []string{"berlin", "london"}, DryRun: true})
	if len(report.Towns) != 2 {
		t.Fatalf("towns = %d, want 2", len(report.Towns))
	}
	if got := len(report.Towns[1].Sources); got != 0 {
		t.Fatalf("london has %d source entries, want 0 (the source does not cover it)", got)
	}
	// London's failing page is never asked, so the run has no failures and two rows.
	if report.Totals.Failures != 0 || report.Totals.Rows != 2 {
		t.Fatalf("totals = %+v, want 2 rows and no failures", report.Totals)
	}
}

// A sitemap's stale link is a fact: counted, never queued, never reported as a failure.
func TestGoneLinkIsCountedAsAFact(t *testing.T) {
	adapter := &fakeAdapter{
		id:      "nortic",
		towns:   map[string][]string{"berlin": {"https://n.test/gone"}},
		failure: map[string]error{"https://n.test/gone": model.ErrGone},
	}
	report := run(t, Options{Adapters: []Adapter{adapter}, Towns: []string{"berlin"}, DryRun: true})
	if report.Totals.Failures != 0 || report.Totals.Queued != 0 {
		t.Fatalf("totals = %+v, want no failures and no queue", report.Totals)
	}
	if report.Totals.Gone != 1 {
		t.Fatalf("gone = %d, want 1", report.Totals.Gone)
	}
}

// nameMatcher is a provider in a map: the names it knows, keyed by identity.
type nameMatcher struct{ names map[string]bool }

func (m *nameMatcher) Matches(_ context.Context, name string) (bool, error) {
	return m.names[model.Key(name)], nil
}

// TestResolverRunsBeforeTheMerge: the same bill spelled two ways must become one key, or the board shows the gig twice.
func TestResolverRunsBeforeTheMerge(t *testing.T) {
	day := time.Date(2026, 12, 2, 20, 0, 0, 0, time.UTC)
	adapter := &fakeAdapter{
		id:    "tickster",
		towns: map[string][]string{"stockholm": {"u1", "u2"}},
		shows: map[string]model.Show{
			"u1": showAt("u1", "Amanda Ginsburg - Det är jul nu igen", "Pustervik", "stockholm-se", day, "tickster"),
			"u2": showAt("u2", "Amanda Ginsburg", "Pustervik", "stockholm-se", day, "tickster"),
		},
	}
	resolver := &resolve.Resolver{
		Matcher: &nameMatcher{names: map[string]bool{"amanda-ginsburg": true}},
		State:   &resolve.State{Format: resolve.Format, Map: map[string]resolve.Decision{}},
		Now:     time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC),
	}
	report := run(t, Options{
		Adapters:        []Adapter{adapter},
		Towns:           []string{"stockholm"},
		DryRun:          true,
		Resolver:        resolver,
		ResolveLimit:    0,
		ArtistStatePath: filepath.Join(t.TempDir(), "artists.json"),
	})
	if report.Resolve == nil || report.Resolve.Applied != 1 {
		t.Fatalf("resolve = %+v, want one applied decision", report.Resolve)
	}
	if report.Merge.Merged != 1 {
		t.Fatalf("merge = %+v, want the two spellings folded into one", report.Merge)
	}
	if report.Totals.Rows != 2 {
		t.Fatalf("rows = %d, want the two source rows before the merge", report.Totals.Rows)
	}
}
