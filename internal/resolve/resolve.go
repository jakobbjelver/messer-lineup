// Package resolve decides which published artist strings are a name with a show title stuck to them.
//
// The sources publish a title, not a performer (Tickster has no `performer` field; Nortic's `name` is the poster's
// headline), so the miner's artist field carries shapes like:
//
//	Amanda Ginsburg - Det är jul nu igen      → the artist, then the show
//	När det lider mot jul - Lisa Nilsson kl 19.00 → the show, then the artist
//	FUNERAL | THE CRYPT                        → two artists (a co-bill)
//	Aural Onsdag: Roberto N Peyre              → a club night, then the artist
//
// Those keys cannot match a listener's library, and they cannot be genre-resolved either: `model.Key` of the whole
// string is an identity no provider has ever heard of. 26 % of the v1 rows carried one of these shapes (measured
// 2026-11-21).
//
// The rule is deliberately conservative, because a wrong artist is worse than an unclean one:
//
//  1. If the whole string is a real artist (exact name, folded), keep it. Never split a name that exists.
//  2. Otherwise split it into candidates and ask the provider about each.
//  3. Accept a candidate only when **exactly one** of them is a real artist. Two real names is a co-bill, and
//     choosing one of them would silently drop the other.
//  4. Anything else is undecided: stored as a decision (so the nightly does not re-ask), never guessed.
//
// A decision is cached in `.lineup-state/artists.json`, positive for ever and negative for thirty days. The resolver
// runs inside `lineup mine` before the merge, so a cleaned key is the key that dedupes and publishes.
package resolve

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/clock"
	"github.com/jakobbjelver/messer-lineup/internal/model"
)

// Format is the state file's format id.
const Format = "lineup-artists/1"

// NegativeTTL is how long a "no candidate is an artist" answer is trusted. A miss is not a fact for ever: a band may
// be added to the provider next month, so the nightly re-asks eventually without asking every night.
const NegativeTTL = 30 * 24 * time.Hour

// Decision is one raw artist string's fate.
type Decision struct {
	// To is the artist name to publish, or "" when the question stays open.
	To string `json:"to"`
	// Why names the rule: "kept" (the whole string is an artist), "split" (one fragment is), "undecided".
	Why string    `json:"why"`
	At  time.Time `json:"at"`
}

// State is the decisions, keyed by the raw name's identity key.
type State struct {
	Format string              `json:"format"`
	Map    map[string]Decision `json:"map"`
}

// LoadState reads a state file, or returns an empty state when it does not exist.
func LoadState(path string) (*State, error) {
	state := &State{Format: Format, Map: map[string]Decision{}}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, state); err != nil {
		return nil, fmt.Errorf("resolve: %s: %w", path, err)
	}
	if state.Format != Format {
		return nil, fmt.Errorf("resolve: %s declares format %q, want %q", path, state.Format, Format)
	}
	if state.Map == nil {
		state.Map = map[string]Decision{}
	}
	return state, nil
}

// Save writes the state atomically: a watcher may read it at any moment, and half a JSON file is a broken state.
func (s *State) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Matcher answers whether a string is a real artist's exact name. The default is Deezer (keyless, one call); a test
// supplies a fake.
type Matcher interface {
	Matches(ctx context.Context, name string) (bool, error)
}

// Resolver decides names and rewrites shows.
type Resolver struct {
	Matcher Matcher
	State   *State
	Now     time.Time
	// MissTTL overrides NegativeTTL (tests use a short one).
	MissTTL time.Duration
}

// Report is what one Resolve call did.
type Report struct {
	Lookups   int `json:"lookups"`
	Resolved  int `json:"resolved"`
	Undecided int `json:"undecided"`
	Applied   int `json:"applied"`
	Failures  int `json:"failures,omitempty"`
	Skipped   int `json:"skipped,omitempty"` // ambiguous names left for a later run (the limit)
}

// Ambiguous reports whether a name carries a shape worth asking about: a separator, a set time, or a trailing star.
// A clean one-word name is never questioned — a provider that happens not to know a real band must not turn into a
// "decision" about it.
func Ambiguous(name string) bool {
	lower := strings.ToLower(name)
	if strings.Contains(name, "|") || strings.Contains(name, ": ") ||
		strings.Contains(lower, " kl ") || strings.Contains(lower, " med ") || strings.Contains(lower, " with ") {
		return true
	}
	for _, separator := range []string{" - ", " – ", " — "} {
		if strings.Contains(name, separator) {
			return true
		}
	}
	return strings.HasSuffix(strings.TrimSpace(name), "*")
}

// Candidates splits a raw artist string into the fragments that could be the name.
//
// Both sides of a dash are candidates because sources disagree about the order: Tickster writes `artist - show`, while
// `När det lider mot jul - Lisa Nilsson` is `show - artist`. The whole string is not a candidate — it was already
// tested by the caller, and it would make "exactly one resolved" impossible to satisfy.
func Candidates(raw string) []string {
	name := strings.Join(strings.Fields(raw), " ")
	// A set time belongs to the event, not the name: `… - Lisa Nilsson kl 19.00`.
	if at := strings.Index(strings.ToLower(name), " kl "); at > 0 {
		name = name[:at]
	}
	var out []string
	seen := map[string]bool{}
	add := func(value string) {
		value = strings.TrimSpace(strings.Trim(strings.TrimSpace(value), "*·,; "))
		value = strings.Join(strings.Fields(value), " ")
		if len([]rune(value)) < 2 {
			return
		}
		key := model.Key(value)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, value)
	}
	split := false
	for _, separator := range []string{" - ", " – ", " — "} {
		if !strings.Contains(name, separator) {
			continue
		}
		split = true
		for _, part := range strings.Split(name, separator) {
			add(part)
		}
	}
	if strings.Contains(name, "|") {
		split = true
		for _, part := range strings.Split(name, "|") {
			add(part)
		}
	}
	if at := strings.Index(name, ": "); at > 0 {
		split = true
		add(name[:at])
		add(name[at+2:])
	}
	// `X - En julkonsert med West of Eden`: the second half names the act. Adding the tail after "med"/"with" as a
	// candidate is what turns a title that happens to be a known name (`Celtic Christmas`) into an undecided row instead
	// of a wrong artist — the title resolves too, so "exactly one" refuses.
	for _, conjunction := range []string{" med ", " with "} {
		if at := strings.Index(strings.ToLower(name), conjunction); at > 0 {
			split = true
			add(name[:at])
			add(name[at+len(conjunction):])
		}
	}
	// No separator: the only noise was a trailing star or a set time, so the cleaned whole string is the candidate.
	// When a separator was found, the whole string is exactly what must not be tested — it would make "exactly one
	// resolved" impossible.
	if !split {
		add(name)
	}
	return out
}

// Resolve asks about the ambiguous names it has not decided, up to limit (0 = all), and records each answer.
//
// A provider failure is not a decision: the name stays unrecorded and is retried next run. That is the same rule the
// portrait resolver learned — a 503 is not a property of the artist.
func (r *Resolver) Resolve(ctx context.Context, names []string, limit int) (Report, error) {
	var report Report
	if r.State == nil {
		r.State = &State{Format: Format, Map: map[string]Decision{}}
	}
	now := r.Now
	if now.IsZero() {
		now = clock.Now()
	}
	missTTL := r.MissTTL
	if missTTL == 0 {
		missTTL = NegativeTTL
	}

	asked := map[string]bool{}
	for _, raw := range names {
		if !Ambiguous(raw) {
			continue
		}
		id := model.Key(raw)
		if id == "" || asked[id] {
			continue
		}
		asked[id] = true

		if decision, ok := r.State.Map[id]; ok {
			// A positive decision is final; a negative one is revisited after the TTL.
			if decision.To != "" || now.Sub(decision.At) < missTTL {
				continue
			}
		}
		if limit > 0 && report.Lookups >= limit {
			report.Skipped++
			continue
		}

		decision, err := r.decide(ctx, raw)
		if err != nil {
			report.Failures++
			continue
		}
		report.Lookups++
		decision.At = now.UTC()
		r.State.Map[id] = decision
		if decision.To != "" && decision.To != raw {
			report.Resolved++
		} else {
			report.Undecided++
		}
	}
	return report, nil
}

// decide applies the rules to one name.
func (r *Resolver) decide(ctx context.Context, raw string) (Decision, error) {
	full, err := r.Matcher.Matches(ctx, raw)
	if err != nil {
		return Decision{}, err
	}
	if full {
		return Decision{To: raw, Why: "kept"}, nil
	}
	var resolved []string
	for _, candidate := range Candidates(raw) {
		if model.Key(candidate) == model.Key(raw) {
			continue
		}
		ok, err := r.Matcher.Matches(ctx, candidate)
		if err != nil {
			return Decision{}, err
		}
		if ok {
			resolved = append(resolved, candidate)
		}
	}
	if len(resolved) == 1 {
		return Decision{To: resolved[0], Why: "split"}, nil
	}
	return Decision{To: "", Why: "undecided"}, nil
}

// Apply rewrites the artist and its key on every show with a positive decision, and returns how many rows changed.
//
// It runs before the merge so that two spellings of the same bill become one key and dedupe. A name that resolves to
// the show's own venue is refused: `Debaser - Debaser` is a title the split got wrong, not an artist.
func (r *Resolver) Apply(shows []model.Show) int {
	if r.State == nil {
		return 0
	}
	changed := 0
	for index := range shows {
		decision, ok := r.State.Map[model.Key(shows[index].Artist)]
		if !ok || decision.To == "" || decision.To == shows[index].Artist {
			continue
		}
		key := model.Key(decision.To)
		if key == "" || key == shows[index].VenueKey {
			continue
		}
		shows[index].Artist = decision.To
		shows[index].ArtistKey = key
		changed++
	}
	return changed
}

// Resolved returns how many names carry a positive decision, for a report line.
func (s *State) Resolved() int {
	n := 0
	for _, decision := range s.Map {
		if decision.To != "" {
			n++
		}
	}
	return n
}
