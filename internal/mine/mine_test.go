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
	if _, err := Run(context.Background(), Options{Adapters: []Adapter{baseAdapter()}, Now: time.Now()}); err == nil {
		t.Fatal("a run with no towns must say so, because no platform has a country index")
	}
	if _, err := Run(context.Background(), Options{Towns: []string{"berlin"}, Now: time.Now()}); err == nil {
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
		"| berlin | dice | 30 | 30 |",
		"| copenhagen | tickster | 30 | 28 | 1 | 1 |",
		"| **total** | | **60** | **58** |",
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
