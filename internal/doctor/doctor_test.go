package doctor

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 17, 19, 0, 0, 0, time.UTC)

func TestProbeOnAFreshClone(t *testing.T) {
	dir := t.TempDir()
	rep := Probe(filepath.Join(dir, ".lineup-index"), filepath.Join(dir, "sources"), now)
	if rep.Index.Built {
		t.Fatal("an install with no index reported one as built")
	}
	if len(rep.Problems) != 0 {
		t.Fatalf("nothing mined is a state, not a problem: %v", rep.Problems)
	}
	if rep.Version == "" || rep.Go == "" || !rep.Now.Equal(now) {
		t.Fatalf("report is missing its own facts: %+v", rep)
	}
}

func TestProbeReportsARefusedIndex(t *testing.T) {
	dir := t.TempDir()
	indexDir := filepath.Join(dir, ".lineup-index")
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, "manifest.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	rep := Probe(indexDir, filepath.Join(dir, "sources"), now)
	if len(rep.Problems) == 0 {
		t.Fatal("an index that refuses to publish was reported as healthy")
	}
}

func TestProbeListsTheRegistry(t *testing.T) {
	dir := t.TempDir()
	countries := filepath.Join(dir, "sources", "countries")
	if err := os.MkdirAll(countries, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, name := range []string{"se.toml", "de.toml", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(countries, name), []byte("id = \"x\"\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	rep := Probe(filepath.Join(dir, ".lineup-index"), filepath.Join(dir, "sources"), now)
	want := []string{"de", "se"}
	if len(rep.Sources.Countries) != len(want) {
		t.Fatalf("countries = %v, want %v", rep.Sources.Countries, want)
	}
	for i := range want {
		if rep.Sources.Countries[i] != want[i] {
			t.Fatalf("countries = %v, want a sorted %v", rep.Sources.Countries, want)
		}
	}
}
