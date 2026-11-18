// Package state is the miner's memory between runs: how many requests a source has had today, and what a
// town's last few sweeps yielded.
//
// It is deliberately a single JSON file rather than a database. One process mines at a time, the whole file is
// a few kilobytes, and a human can read it when a run behaves oddly — SQLite arrives with the job queue, when
// claims actually need transactions. Losing this file costs a day of rate budget and a week of yield history,
// never data a published index depends on.
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Version is the on-disk format. A file from a future version is refused rather than half-read.
const Version = 1

// HistoryDepth is how many runs of one (source, town) the yield guard remembers.
const HistoryDepth = 7

// Run is one completed town sweep, kept so a collapse can be told from a quiet week.
type Run struct {
	At         time.Time `json:"at"`
	Day        string    `json:"day"`
	Candidates int       `json:"candidates"`
	Rows       int       `json:"rows"`
}

// Data is the file.
type Data struct {
	Version int                       `json:"version"`
	Budget  map[string]map[string]int `json:"budget"` // source → YYYY-MM-DD → requests taken
	Runs    map[string][]Run          `json:"runs"`   // "<source>|<town>" → most recent last
	Jobs    []Job                     `json:"jobs,omitempty"`
	GaveUp  map[string]time.Time      `json:"gave_up,omitempty"`
}

// State is the open file. Safe for concurrent use.
type State struct {
	mu   sync.Mutex
	path string
	data Data
}

// Open reads the state file, or starts a fresh one. A corrupt file is reported for what it is: the caller
// decides whether a run may proceed without its history.
func Open(path string) (*State, error) {
	s := &State{path: path, data: Data{Version: Version, Budget: map[string]map[string]int{}, Runs: map[string][]Run{}}}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(raw, &s.data); err != nil {
		return nil, fmt.Errorf("state: %s: %w", path, err)
	}
	if s.data.Version != Version {
		return nil, fmt.Errorf("state: %s is format %d, this build writes %d", path, s.data.Version, Version)
	}
	if s.data.Budget == nil {
		s.data.Budget = map[string]map[string]int{}
	}
	if s.data.Runs == nil {
		s.data.Runs = map[string][]Run{}
	}
	return s, nil
}

// Take records one request against a source's day and refuses the request when the day's budget is spent.
// A limit of zero means no cap — a measurement run, not a schedule.
func (s *State) Take(source string, day time.Time, limit int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := day.Format("2006-01-02")
	bySource, ok := s.data.Budget[source]
	if !ok {
		bySource = map[string]int{}
		s.data.Budget[source] = bySource
	}
	if limit > 0 && bySource[key] >= limit {
		return fmt.Errorf("state: %s has used its %d requests for %s (raise --budget or wait for tomorrow)", source, limit, key)
	}
	bySource[key]++
	return s.save()
}

// Taken is how many requests a source has made on a day.
func (s *State) Taken(source string, day time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Budget[source][day.Format("2006-01-02")]
}

// Record remembers one completed sweep.
func (s *State) Record(source, town string, run Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := source + "|" + town
	runs := append(s.data.Runs[key], run)
	if len(runs) > HistoryDepth {
		runs = runs[len(runs)-HistoryDepth:]
	}
	s.data.Runs[key] = runs
	return s.save()
}

// Median is the median rows of the last n sweeps of one town by one source, and whether there are enough runs
// for it to mean anything (two, at least: one run is not a baseline).
func (s *State) Median(source, town string, n int) (float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	runs := s.data.Runs[source+"|"+town]
	if n <= 0 || n > len(runs) {
		n = len(runs)
	}
	if n < 2 {
		return 0, false
	}
	window := runs[len(runs)-n:]
	values := make([]int, 0, len(window))
	for _, run := range window {
		values = append(values, run.Rows)
	}
	sort.Ints(values)
	mid := len(values) / 2
	if len(values)%2 == 1 {
		return float64(values[mid]), true
	}
	return float64(values[mid-1]+values[mid]) / 2, true
}

// History is a copy of one key's runs, oldest first — for a report or a doctor line.
func (s *State) History(source, town string) []Run {
	s.mu.Lock()
	defer s.mu.Unlock()
	runs := s.data.Runs[source+"|"+town]
	out := make([]Run, len(runs))
	copy(out, runs)
	return out
}

// Save writes the file, creating its directory. Every mutation already saves; this exists for a caller that
// wants the file to exist before a run begins.
func (s *State) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save()
}

func (s *State) save() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	blob, err := json.MarshalIndent(s.data, "", " ")
	if err != nil {
		return err
	}
	// Write-then-rename: a run killed mid-write must not leave a truncated history behind.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(blob, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
