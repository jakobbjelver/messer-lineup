package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func open(t *testing.T) (*State, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state", "state.json")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return s, path
}

func TestBudgetCountsAndRefuses(t *testing.T) {
	s, path := open(t)
	day := time.Date(2026, 11, 17, 23, 30, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if err := s.Take("tickster", day, 3); err != nil {
			t.Fatalf("take %d: %v", i, err)
		}
	}
	if err := s.Take("tickster", day, 3); err == nil {
		t.Fatal("the fourth request was allowed past a cap of three")
	}
	if got := s.Taken("tickster", day); got != 3 {
		t.Fatalf("taken = %d, want 3 — a refused request must not be counted", got)
	}
	// The count survives a restart: that is the whole point of the file.
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := reopened.Taken("tickster", day); got != 3 {
		t.Fatalf("after reopen: %d, want 3", got)
	}
	// Another source has its own budget.
	if err := reopened.Take("dice", day, 3); err != nil {
		t.Fatalf("dice should not be affected by tickster's cap: %v", err)
	}
	// Tomorrow starts at zero.
	if got := reopened.Taken("tickster", day.Add(24*time.Hour)); got != 0 {
		t.Fatalf("tomorrow already has %d requests", got)
	}
}

func TestZeroLimitMeansNoCap(t *testing.T) {
	s, _ := open(t)
	day := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 50; i++ {
		if err := s.Take("dice", day, 0); err != nil {
			t.Fatalf("take: %v", err)
		}
	}
	if got := s.Taken("dice", day); got != 50 {
		t.Fatalf("taken = %d", got)
	}
}

func TestMedianAndItsConfidence(t *testing.T) {
	s, _ := open(t)
	day := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	s.Record("tickster", "copenhagen", Run{Day: day.Format("2006-01-02"), Rows: 40})
	if _, ok := s.Median("tickster", "copenhagen", HistoryDepth); ok {
		t.Fatal("one run is not a baseline")
	}
	for _, rows := range []int{30, 32, 38, 200} {
		s.Record("tickster", "copenhagen", Run{Day: day.Format("2006-01-02"), Rows: rows})
	}
	// Last seven, five runs: 40, 30, 32, 38, 200 → median 38 (the outlier does not move the middle).
	median, ok := s.Median("tickster", "copenhagen", HistoryDepth)
	if !ok || median != 38 {
		t.Fatalf("median = %v, ok = %v, want 38", median, ok)
	}
	// The window is capped at the depth, so an old run falls out and the outlier becomes the middle.
	for i := 0; i < 3; i++ {
		s.Record("tickster", "copenhagen", Run{Day: day.Format("2006-01-02"), Rows: 200})
	}
	median, _ = s.Median("tickster", "copenhagen", HistoryDepth)
	if median != 200 {
		t.Fatalf("median after aging = %v, want the newest seven", median)
	}
}

func TestHistoryIsBoundedAndPermanent(t *testing.T) {
	s, path := open(t)
	for i := 0; i < HistoryDepth+5; i++ {
		s.Record("dice", "berlin", Run{Rows: i})
	}
	history := s.History("dice", "berlin")
	if len(history) != HistoryDepth {
		t.Fatalf("history = %d entries, want %d", len(history), HistoryDepth)
	}
	if history[len(history)-1].Rows != HistoryDepth+4 {
		t.Fatalf("last entry = %+v, want the newest run", history[len(history)-1])
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if len(reopened.History("dice", "berlin")) != HistoryDepth {
		t.Fatal("history did not survive the restart")
	}
}

func TestAFutureFormatIsRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte(`{"version": 99}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("a file from a newer build was read")
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("a corrupt file was read as empty state — that would silently reset a day's budget")
	}
}
