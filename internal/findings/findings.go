// Package findings is a small ledger of things a run noticed and a person should look at.
//
// Why it exists: the nightly is unattended, and every condition that matters is already detected somewhere — the yield
// guard, the publish guard, the retry queue's give-up — but each of them only prints to a log at 04:10. A finding gives
// that condition a name, the numbers it was decided on, and a lifecycle (`open` → `closed`), so a later reader (or an
// agent) can ask "what is wrong with the mine?" without grepping a night's log.
//
// What it is not: a second detector. A finding is written **where the condition is already concluded**, never by a pass
// that re-derives it — otherwise the ledger disagrees with the run that produced it.
//
// The store is its own file (`.lineup-state/findings.json`), not a field of the miner's `state.json`: a corrupt ledger
// must not cost the rate budget or the retry queue, and a run that never touches findings must not rewrite this file.
package findings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/clock"
)

// Format is the file's format id, so a future shape change is visible rather than silent.
const Format = "lineup-findings/1"

// The kinds a run can raise. They are constants because the nightly, the CLI and a future warden all switch on them.
const (
	// KindYield is a town/source pair that produced no rows, or far fewer than its own median: a parser break.
	KindYield = "yield-collapse"
	// KindRefused is a publish the guards stopped (a collapsing index) or an index that failed to verify.
	KindRefused = "publish-refused"
	// KindGivenUp is a queued target that exhausted its retries.
	KindGivenUp = "queue-given-up"
)

// Finding is one thing to look at.
//
// The ID is stable — kind, source and town — so a condition that repeats for a week is one row with a fresher timestamp
// and newer evidence, not seven rows nobody reads. Closing is explicit: a run that stops seeing the condition does not
// close it, because "it stopped happening" is not the same as "somebody dealt with it".
type Finding struct {
	ID       string         `json:"id"`
	Kind     string         `json:"kind"`
	Source   string         `json:"source,omitempty"`
	Town     string         `json:"town,omitempty"`
	OpenedAt time.Time      `json:"opened_at"`
	ClosedAt *time.Time     `json:"closed_at,omitempty"`
	Evidence map[string]any `json:"evidence,omitempty"`
	Note     string         `json:"note,omitempty"`
}

// Open reports whether the finding is unresolved.
func (f Finding) Open() bool { return f.ClosedAt == nil }

// ID composes the stable identity: the condition, where it was seen, and nothing else.
func ID(kind, source, town string) string {
	parts := make([]string, 0, 3)
	for _, part := range []string{kind, source, town} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "|")
}

// File is the on-disk shape.
type File struct {
	Format   string    `json:"format"`
	Findings []Finding `json:"findings"`
}

// Store is the ledger, in memory until Save.
type Store struct {
	path string
	file File
}

// Open reads the ledger at path, or starts an empty one when the file does not exist.
func Open(path string) (*Store, error) {
	store := &Store{path: path, file: File{Format: Format}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &store.file); err != nil {
		return nil, fmt.Errorf("findings: %s: %w", path, err)
	}
	if store.file.Format != Format {
		return nil, fmt.Errorf("findings: %s: format %q, want %q", path, store.file.Format, Format)
	}
	return store, nil
}

// Path is where this store reads and writes.
func (s *Store) Path() string { return s.path }

// DefaultPath is the ledger beside a miner state file: `--state .lineup-state/state.json` means
// `.lineup-state/findings.json`, so one flag names the directory.
func DefaultPath(statePath string) string {
	if statePath == "" {
		return "findings.json"
	}
	return filepath.Join(filepath.Dir(statePath), "findings.json")
}

// Raise records a finding, or refreshes the open one with this ID. It returns the stored finding and whether it was new:
// a repeat is not an event, and a caller that says "raised 4 findings" every night is noise.
func (s *Store) Raise(f Finding) (Finding, bool) {
	if f.ID == "" {
		f.ID = ID(f.Kind, f.Source, f.Town)
	}
	if f.OpenedAt.IsZero() {
		f.OpenedAt = clock.Now()
	}
	for i := range s.file.Findings {
		if s.file.Findings[i].ID != f.ID {
			continue
		}
		// Refresh the evidence and the timestamp; leave the note and the lifecycle alone. An open finding stays open
		// (it is still happening); a closed one that comes back is reopened, because the condition returned.
		s.file.Findings[i].Evidence = f.Evidence
		s.file.Findings[i].OpenedAt = f.OpenedAt
		s.file.Findings[i].ClosedAt = nil
		return s.file.Findings[i], false
	}
	s.file.Findings = append(s.file.Findings, f)
	return f, true
}

// Close settles a finding. Closing one that is already closed only updates the note.
func (s *Store) Close(id, note string, now time.Time) error {
	for i := range s.file.Findings {
		if s.file.Findings[i].ID != id {
			continue
		}
		at := now.UTC()
		s.file.Findings[i].ClosedAt = &at
		if note != "" {
			s.file.Findings[i].Note = note
		}
		return nil
	}
	return fmt.Errorf("findings: no such finding %q", id)
}

// List returns the ledger, newest first. With openOnly, only what is still unresolved.
func (s *Store) List(openOnly bool) []Finding {
	out := make([]Finding, 0, len(s.file.Findings))
	for _, finding := range s.file.Findings {
		if openOnly && !finding.Open() {
			continue
		}
		out = append(out, finding)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].OpenedAt.Equal(out[j].OpenedAt) {
			return out[i].OpenedAt.After(out[j].OpenedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Count is how many findings are open.
func (s *Store) Count() int {
	n := 0
	for _, finding := range s.file.Findings {
		if finding.Open() {
			n++
		}
	}
	return n
}

// Save writes the ledger atomically: a watcher may read it at any moment, and a half-written file reads as a corrupt
// ledger rather than as the last good one.
func (s *Store) Save() error {
	if len(s.file.Findings) == 0 {
		// An empty ledger is still a file once something has been raised: a reader asking "did anything break?" wants
		// an answer, and an empty file is not one.
		if _, err := os.Stat(s.path); errors.Is(err, os.ErrNotExist) {
			return nil
		}
	}
	raw, err := json.MarshalIndent(s.file, "", " ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
