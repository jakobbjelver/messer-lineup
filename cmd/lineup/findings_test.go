package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/clock"
	"github.com/jakobbjelver/messer-lineup/internal/findings"
	"github.com/jakobbjelver/messer-lineup/internal/mine"
)

// report is a finished mine: one town that warned, one that did not, and one source that gave up on a target.
func report() mine.Report {
	return mine.Report{
		FinishedAt: time.Date(2026, 11, 22, 4, 50, 0, 0, time.UTC),
		Towns: []mine.TownReport{
			{
				Slug: "stockholm",
				Sources: []mine.SourceReport{
					{ID: "tickster", Candidates: 940, Rows: 12, Seconds: 601, Warning: "parse-yield: 12 rows from 940 candidates against a median of 320"},
					{ID: "nortic", Candidates: 2152, Rows: 240, Seconds: 1080, GivenUp: 2},
				},
			},
			{Slug: "malmo", Sources: []mine.SourceReport{{ID: "tickster", Candidates: 250, Rows: 70, Seconds: 150}}},
		},
	}
}

func TestAWarningBecomesOneFinding(t *testing.T) {
	store, err := findings.Open(filepath.Join(t.TempDir(), "findings.json"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if raised := recordMineFindings(store, report()); raised != 2 {
		t.Fatalf("raised %d, want 2 (one yield collapse, one give-up)", raised)
	}
	list := store.List(true)
	if len(list) != 2 {
		t.Fatalf("ledger holds %d open, want 2: %+v", len(list), list)
	}
	byID := map[string]findings.Finding{}
	for _, finding := range list {
		byID[finding.ID] = finding
	}
	yield, ok := byID[findings.ID(findings.KindYield, "tickster", "stockholm")]
	if !ok {
		t.Fatalf("no yield finding: %+v", byID)
	}
	if yield.Evidence["rows"] != float64(0) && yield.Evidence["rows"] != 12 {
		t.Fatalf("evidence does not carry the run's numbers: %+v", yield.Evidence)
	}
	if _, ok := byID[findings.ID(findings.KindGivenUp, "nortic", "stockholm")]; !ok {
		t.Fatalf("no give-up finding: %+v", byID)
	}
}

func TestACleanRunRaisesNothing(t *testing.T) {
	store, _ := findings.Open(filepath.Join(t.TempDir(), "findings.json"))
	clean := mine.Report{FinishedAt: clock.Now(), Towns: []mine.TownReport{{Slug: "malmo", Sources: []mine.SourceReport{{ID: "tickster", Candidates: 250, Rows: 70}}}}}
	if raised := recordMineFindings(store, clean); raised != 0 || store.Count() != 0 {
		t.Fatalf("a clean run raised %d findings", raised)
	}
}

func TestRepeatsAreNotNewEvents(t *testing.T) {
	store, _ := findings.Open(filepath.Join(t.TempDir(), "findings.json"))
	if raised := recordMineFindings(store, report()); raised != 2 {
		t.Fatalf("first run raised %d", raised)
	}
	if raised := recordMineFindings(store, report()); raised != 0 {
		t.Fatalf("a second identical run raised %d new findings, want 0", raised)
	}
	if store.Count() != 2 {
		t.Fatalf("count = %d after two runs, want 2", store.Count())
	}
}

func TestTheLedgerSitsBesideTheState(t *testing.T) {
	if got := findingsPath("", ".lineup-state/state.json"); got != filepath.Join(".lineup-state", "findings.json") {
		t.Fatalf("findingsPath = %q", got)
	}
	if got := findingsPath("/tmp/x.json", ".lineup-state/state.json"); got != "/tmp/x.json" {
		t.Fatalf("an explicit path was ignored: %q", got)
	}
}
