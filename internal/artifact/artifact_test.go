package artifact

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// fixture writes the smallest manifest an artifact can attach to.
func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	manifest := `{"format":"lineup-index/1","built_at":"2026-11-21T00:00:00Z",` +
		`"window":{"from":"2026-11-21","to":"2027-03-21","days":120},"sources":[],"towns":[]}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestWriteIsContentAddressedAndPrunesTheOldVersion(t *testing.T) {
	dir := fixture(t)
	// The file the pre-content-addressing build published. It must go: a stable name with an immutable header is a
	// file that never reaches a returning visitor again.
	if err := os.MkdirAll(filepath.Join(dir, Dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, Dir, "portraits.json"), []byte(`{"old":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	first, err := Write(dir, "portraits", []byte(`{"a":{"u":"one"}}`))
	if err != nil {
		t.Fatalf("first write: %v", err)
	}
	second, err := Write(dir, "portraits", []byte(`{"a":{"u":"two"}}`))
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	if first == second {
		t.Fatalf("different bytes produced the same name %q", first)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(first))); !os.IsNotExist(err) {
		t.Fatalf("the old version %s was not pruned (err %v)", first, err)
	}
	if _, err := os.Stat(filepath.Join(dir, Dir, "portraits.json")); !os.IsNotExist(err) {
		t.Fatalf("the legacy stable name was not pruned (err %v)", err)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(second))); err != nil {
		t.Fatalf("the new version %s is missing: %v", second, err)
	}

	var manifest struct {
		Files map[string]string `json:"files"`
	}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Files["portraits"] != second {
		t.Fatalf("manifest names %q, want %q", manifest.Files["portraits"], second)
	}
}

func TestWriteKeepsOtherKindsAndIsIdempotent(t *testing.T) {
	dir := fixture(t)

	if _, err := Write(dir, "portraits", []byte(`{"p":1}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(dir, "genres", []byte(`{"g":1}`)); err != nil {
		t.Fatal(err)
	}
	same, err := Write(dir, "portraits", []byte(`{"p":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if same == "" {
		t.Fatal("an idempotent write returned no name")
	}

	var manifest struct {
		Files map[string]string `json:"files"`
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 2 {
		t.Fatalf("files map holds %d entries, want 2: %v", len(manifest.Files), manifest.Files)
	}

	entries, err := os.ReadDir(filepath.Join(dir, Dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("artists/ holds %d files, want one per kind: %v", len(entries), entries)
	}
}
