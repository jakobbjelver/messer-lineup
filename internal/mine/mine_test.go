package mine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// eventPage is a page shaped like DICE's: one JSON-LD block, a name, a start, a venue with a town.
func eventPage(artist, town, country string, lat, lon float64) []byte {
	doc := map[string]any{
		"@context":  "https://schema.org",
		"@type":     "MusicEvent",
		"name":      artist,
		"startDate": "2026-12-01T20:00:00+00:00",
		"location": map[string]any{
			"@type": "Place",
			"name":  "Some Venue",
			"address": map[string]any{
				"@type":           "PostalAddress",
				"addressLocality": town,
				"addressCountry":  country,
			},
			"geo": map[string]any{"latitude": lat, "longitude": lon},
		},
	}
	payload, _ := json.Marshal(doc)
	return []byte(`<html><head><script type="application/ld+json">` + string(payload) + `</script></head></html>`)
}

func browsePage(links ...string) []byte {
	var b strings.Builder
	b.WriteString(`<html><body>`)
	for _, link := range links {
		b.WriteString(`<a href="` + link + `">x</a>`)
	}
	b.WriteString(`</body></html>`)
	return []byte(b.String())
}

// platform serves a small but correctly shaped platform: one browse page per town, and event pages whose
// ids are hex (the adapter's filter keeps only those).
func platform(t *testing.T, towns map[string][2]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("")) })

	// Event pages, registered once: <hex id 1> is a real event, <hex id 2> is gone.
	events := map[string][2]string{
		"aaaaaaaaaaaaaaaa1": {"Berlin", "DE"},
		"bbbbbbbbbbbbbbbb1": {"London", "GB"},
	}
	for id, pair := range events {
		id, pair := id, pair
		mux.HandleFunc("/event/"+id, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(eventPage("Artist "+id, pair[0], pair[1], 52.5, 13.4))
		})
	}
	mux.HandleFunc("/event/", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })

	for slug := range towns {
		slug := slug
		links := []string{"/event/aaaaaaaaaaaaaaaa1", "/event/aaaaaaaaaaaaaaaa2"}
		if slug == "london" {
			links = []string{"/event/bbbbbbbbbbbbbbbb1", "/event/bbbbbbbbbbbbbbbb2"}
		}
		mux.HandleFunc("/browse/"+slug, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(browsePage(links...))
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestRunMeasuresAndReports(t *testing.T) {
	srv := platform(t, map[string][2]string{
		"berlin": {"Berlin", "DE"},
		"london": {"London", "GB"},
	})
	ctx := context.Background()
	report, err := Run(ctx, Options{
		Source:   SourceDice,
		Towns:    []string{"berlin", "london"},
		DryRun:   true,
		Interval: -1,
		Now:      time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC),
		BaseURL:  srv.URL,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.Totals.Candidates != 4 || report.Totals.Rows != 2 {
		t.Fatalf("totals = %+v, want 4 candidates and 2 rows", report.Totals)
	}
	if report.Totals.Failures != 2 {
		t.Fatalf("failures = %d, want the two 404 pages", report.Totals.Failures)
	}
	if report.TownCount != 2 {
		t.Fatalf("towns discovered = %d, want 2", report.TownCount)
	}
	if !report.DryRun || report.StartedAt.IsZero() || report.FinishedAt.IsZero() {
		t.Fatalf("report header = %+v", report)
	}
	// Every town's failure map must name a reason, not just a count.
	for _, town := range report.Towns {
		if len(town.Failures) == 0 || town.Failures["gone (404/410)"] != 1 {
			t.Fatalf("town %s failures = %v, want one gone page", town.Slug, town.Failures)
		}
	}
}

func TestRunCountryFilter(t *testing.T) {
	srv := platform(t, map[string][2]string{
		"berlin": {"Berlin", "DE"},
		"london": {"London", "GB"},
	})
	report, err := Run(context.Background(), Options{
		Source:    SourceDice,
		Towns:     []string{"berlin", "london"},
		Countries: []string{"de"},
		DryRun:    true,
		Interval:  -1,
		BaseURL:   srv.URL,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.Totals.Rows != 1 || report.Totals.Filtered != 1 {
		t.Fatalf("with --country de: %+v, want 1 row and 1 filtered", report.Totals)
	}
	if report.TownCount != 1 {
		t.Fatalf("towns discovered = %d, want only berlin", report.TownCount)
	}
	if len(report.Countries) != 1 || report.Countries[0] != "de" {
		t.Fatalf("countries recorded = %v", report.Countries)
	}
}

func TestRunRefusesARunWithNoTowns(t *testing.T) {
	if _, err := Run(context.Background(), Options{DryRun: true}); err == nil {
		t.Fatal("a run with no towns must say so, because this platform has no country index")
	}
}

func TestReportJSONRoundTrip(t *testing.T) {
	srv := platform(t, map[string][2]string{"berlin": {"Berlin", "DE"}})
	report, err := Run(context.Background(), Options{Source: SourceDice, Towns: []string{"berlin"}, DryRun: true, Interval: -1, BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
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
}

func TestCoverageBlockAndRewrite(t *testing.T) {
	report := Report{
		Source: "dice", DryRun: true,
		StartedAt: time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC),
		Towns: []TownReport{
			{Slug: "berlin", Candidates: 30, Rows: 30, Seconds: 31},
			{Slug: "london", Candidates: 30, Rows: 28, Filtered: 1, Failures: map[string]int{"gone (404/410)": 1}, Seconds: 32},
			{Slug: "oslo", Error: "404"},
		},
		Totals:    Totals{Candidates: 60, Rows: 58, Filtered: 1, Failures: 1},
		TownCount: 2,
		Note:      "no shards written: the writer arrives with the merge step (P1)",
	}
	block := RenderCoverageBlock(report)
	for _, want := range []string{CoverageBegin, CoverageEnd, "| berlin | 30 | 30 |", "| **total** | **60** | **58** |", "gone (404/410) ×1", "Towns discovered: **2**", "`oslo` could not be read: 404"} {
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
	// A file without markers is an error, never an append.
	plain := filepath.Join(t.TempDir(), "plain.md")
	_ = os.WriteFile(plain, []byte("# nothing marked here\n"), 0o644)
	if err := UpdateCoverageFile(plain, report); err == nil {
		t.Fatal("a file with no markers was rewritten")
	}
}
