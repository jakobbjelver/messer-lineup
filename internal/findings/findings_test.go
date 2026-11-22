package findings

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func at(day int) time.Time { return time.Date(2026, 11, day, 4, 10, 0, 0, time.UTC) }

func TestARepeatRefreshesInsteadOfDuplicating(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "findings.json"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	first, created := store.Raise(Finding{
		Kind: KindYield, Source: "tickster", Town: "stockholm", OpenedAt: at(20),
		Evidence: map[string]any{"rows": 0, "median": 240.0},
	})
	if !created || first.ID != "yield-collapse|tickster|stockholm" {
		t.Fatalf("first = %+v created=%v", first, created)
	}

	second, created := store.Raise(Finding{
		Kind: KindYield, Source: "tickster", Town: "stockholm", OpenedAt: at(21),
		Evidence: map[string]any{"rows": 12, "median": 240.0},
	})
	if created {
		t.Fatalf("a repeat was created as new: %+v", second)
	}
	if second.OpenedAt != at(21) {
		t.Fatalf("opened at %v, want the newer run %v", second.OpenedAt, at(21))
	}
	if second.Evidence["rows"] != 12 {
		t.Fatalf("evidence was not refreshed: %+v", second.Evidence)
	}
	if got := len(store.List(false)); got != 1 {
		t.Fatalf("ledger holds %d findings, want 1", got)
	}
}

func TestCloseSurvivesAReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "findings.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	store.Raise(Finding{Kind: KindRefused, Town: "malmo", OpenedAt: at(20)})
	if store.Count() != 1 {
		t.Fatalf("count = %d, want 1", store.Count())
	}
	if err := store.Close(ID(KindRefused, "", "malmo"), "a human looked", at(21)); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := store.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	again, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if again.Count() != 0 {
		t.Fatalf("a closed finding is still open after a reopen: %+v", again.List(true))
	}
	if got := again.List(false); len(got) != 1 || got[0].Note != "a human looked" {
		t.Fatalf("the closed finding lost its note: %+v", got)
	}
}

func TestAClosedConditionThatComesBackReopens(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "findings.json"))
	store.Raise(Finding{Kind: KindGivenUp, Source: "nortic", OpenedAt: at(20)})
	if err := store.Close(ID(KindGivenUp, "nortic", ""), "", at(20)); err != nil {
		t.Fatalf("close: %v", err)
	}
	reopened, created := store.Raise(Finding{Kind: KindGivenUp, Source: "nortic", OpenedAt: at(22)})
	if created {
		t.Fatalf("a returning condition was appended as a second row")
	}
	if !reopened.Open() {
		t.Fatalf("a returning condition did not reopen: %+v", reopened)
	}
}

func TestABrokenLedgerIsAnErrorNotAnEmptyOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "findings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Open(path); err == nil {
		t.Fatalf("a corrupt ledger opened cleanly")
	}
}

func TestDefaultPathIsBesideTheState(t *testing.T) {
	if got := DefaultPath(".lineup-state/state.json"); got != filepath.Join(".lineup-state", "findings.json") {
		t.Fatalf("DefaultPath = %q", got)
	}
}
