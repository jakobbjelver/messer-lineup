// Package mine runs a sweep and writes down what happened.
//
// It exists so the numbers in docs/coverage.md are output rather than recollection: one command walks a set of
// towns, fetches a detail page per candidate, projects each show, and writes a report. A dry run stops short of
// writing shards; the report is the same either way.
package mine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/fetch"
	"github.com/jakobbjelver/messer-lineup/internal/index"
	"github.com/jakobbjelver/messer-lineup/internal/model"
	"github.com/jakobbjelver/messer-lineup/internal/sources/dice"
)

// Source names the platform a run reads. One today; a second platform plugs in behind the same three calls
// (TownCandidates, Detail, and a town id on the show it returns).
const (
	SourceDice = "dice"
)

// Options is one run.
type Options struct {
	Source     string        // dice
	Towns      []string      // platform city slugs, e.g. berlin, london, austin
	Countries  []string      // keep only towns whose id ends in one of these (de, gb, us)
	DryRun     bool          // measure and report; write no shards
	Interval   time.Duration // per-request gap; zero takes the client default
	MaxPerTown int           // 0 means every candidate the town page shows
	Now        time.Time     // the clock, injected
	BaseURL    string        // override the platform origin (tests, and a mirror)
	Log        io.Writer     // progress; may be nil
}

// Run sweeps and returns the report. It always returns a report when it returns at all: a run that died
// half-way is a fact worth writing down.
func Run(ctx context.Context, opts Options) (Report, error) {
	if len(opts.Towns) == 0 {
		return Report{}, fmt.Errorf("mine: no towns — this platform publishes no country index, so a run names its towns (`--town berlin`)")
	}
	source := strings.ToLower(strings.TrimSpace(opts.Source))
	if source == "" {
		source = SourceDice
	}
	if source != SourceDice {
		return Report{}, fmt.Errorf("mine: unknown source %q", opts.Source)
	}
	logf := func(format string, args ...any) {
		if opts.Log != nil {
			fmt.Fprintf(opts.Log, format+"\n", args...)
		}
	}

	client := fetch.New(fetch.Options{Interval: opts.Interval, Timeout: 45 * time.Second})
	src := dice.New(client)
	if opts.BaseURL != "" {
		src = src.WithBase(opts.BaseURL)
	}
	towns := index.NewTownSet()

	report := Report{Source: source, StartedAt: opts.Now.UTC(), DryRun: opts.DryRun}
	countries := countryFilter(opts.Countries)
	if countries != nil {
		report.Countries = sortedKeys(countries)
	}

	for _, slug := range opts.Towns {
		slug = strings.TrimSpace(slug)
		if slug == "" {
			continue
		}
		started := time.Now()
		town := TownReport{Slug: slug, Failures: map[string]int{}}

		candidates, err := src.TownCandidates(ctx, slug)
		if err != nil {
			town.Error = err.Error()
			report.Towns = append(report.Towns, town)
			logf("%-12s error: %v", slug, err)
			continue
		}
		if opts.MaxPerTown > 0 && len(candidates) > opts.MaxPerTown {
			candidates = candidates[:opts.MaxPerTown]
		}
		town.Candidates = len(candidates)

		for _, candidate := range candidates {
			show, derr := src.Detail(ctx, candidate)
			if derr != nil {
				town.Failures[classify(derr)]++
				continue
			}
			if _, perr := model.Project(show); perr != nil {
				town.Failures["not publishable: "+perr.Error()]++
				continue
			}
			if countries != nil && !countries[countryOf(show.Town)] {
				town.Filtered++
				continue
			}
			town.Rows++
			towns.Add(show)
		}
		town.Seconds = time.Since(started).Seconds()
		report.Towns = append(report.Towns, town)
		report.Totals.Candidates += town.Candidates
		report.Totals.Rows += town.Rows
		report.Totals.Failures += townFailures(town)
		report.Totals.Filtered += town.Filtered
		logf("%-12s %2d candidates → %2d rows, %2d filtered, %2d failures, %.0fs",
			slug, town.Candidates, town.Rows, town.Filtered, townFailures(town), town.Seconds)
	}

	report.TownCount = towns.Len()
	if !opts.DryRun {
		// The shard writer lands with the merge step; saying so is better than pretending a dry run wrote one.
		report.Note = "no shards written: the writer arrives with the merge step (P1)"
	}
	report.FinishedAt = time.Now().UTC()
	if report.FinishedAt.Before(report.StartedAt) {
		report.FinishedAt = report.StartedAt
	}
	return report, nil
}

func townFailures(t TownReport) int {
	n := 0
	for _, count := range t.Failures {
		n += count
	}
	return n
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

func countryOf(townID string) string {
	i := strings.LastIndex(townID, "-")
	if i < 0 || i == len(townID)-1 {
		return ""
	}
	return strings.ToLower(townID[i+1:])
}

// classify names why a page did not become a row, so a report distinguishes "the host refused us" from "the
// page had no event". A run that loses pages and cannot say why is not a measurement.
func classify(err error) string {
	switch {
	case err == nil:
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
	case strings.Contains(msg, "no schema.org event"):
		return "no event block"
	case strings.Contains(msg, "startDate"):
		return "no start date"
	case strings.Contains(msg, "town address"):
		return "no town"
	case strings.Contains(msg, "venue name"):
		return "no venue"
	case strings.Contains(msg, "context deadline"), strings.Contains(msg, "timeout"):
		return "timeout"
	}
	return "other: " + msg
}

// Report is one run, in the shape the coverage record is rendered from.
type Report struct {
	Source     string       `json:"source"`
	StartedAt  time.Time    `json:"started_at"`
	FinishedAt time.Time    `json:"finished_at"`
	DryRun     bool         `json:"dry_run"`
	Countries  []string     `json:"countries,omitempty"`
	Towns      []TownReport `json:"towns"`
	TownCount  int          `json:"towns_discovered"`
	Totals     Totals       `json:"totals"`
	Note       string       `json:"note,omitempty"`
}

// TownReport is one town's part of a run.
type TownReport struct {
	Slug       string         `json:"slug"`
	Candidates int            `json:"candidates"`
	Rows       int            `json:"rows"`
	Filtered   int            `json:"filtered"`
	Failures   map[string]int `json:"failures,omitempty"`
	Seconds    float64        `json:"seconds"`
	Error      string         `json:"error,omitempty"`
}

// Totals is the run in one line.
type Totals struct {
	Candidates int `json:"candidates"`
	Rows       int `json:"rows"`
	Filtered   int `json:"filtered"`
	Failures   int `json:"failures"`
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
