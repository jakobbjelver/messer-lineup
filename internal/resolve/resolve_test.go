package resolve

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/model"
)

type fakeMatcher struct {
	names map[string]bool
	fail  map[string]bool
	calls []string
}

func (f *fakeMatcher) Matches(_ context.Context, name string) (bool, error) {
	f.calls = append(f.calls, name)
	if f.fail[model.Key(name)] {
		return false, errors.New("provider blinked")
	}
	return f.names[model.Key(name)], nil
}

func TestCandidates(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{"Amanda Ginsburg - Det är jul nu igen", []string{"Amanda Ginsburg", "Det är jul nu igen"}},
		{"När det lider mot jul - Lisa Nilsson kl 19.00", []string{"När det lider mot jul", "Lisa Nilsson"}},
		{"FUNERAL | THE CRYPT", []string{"FUNERAL", "THE CRYPT"}},
		{"Aural Onsdag: Roberto N Peyre", []string{"Aural Onsdag", "Roberto N Peyre"}},
		{"Jonas Lundqvist*", []string{"Jonas Lundqvist"}},
		{"a - b", nil}, // fragments shorter than two runes are not names
	}
	for _, test := range cases {
		got := Candidates(test.raw)
		if len(got) != len(test.want) {
			t.Fatalf("Candidates(%q) = %v, want %v", test.raw, got, test.want)
		}
		for index := range got {
			if got[index] != test.want[index] {
				t.Fatalf("Candidates(%q) = %v, want %v", test.raw, got, test.want)
			}
		}
	}
}

func TestAmbiguous(t *testing.T) {
	yes := []string{"Amanda Ginsburg - Det är jul nu igen", "FUNERAL | THE CRYPT", "Lisa Nilsson kl 19.00", "Jonas Lundqvist*", "Aural Onsdag: Roberto N Peyre"}
	no := []string{"Opeth", "Bo Kaspers Orkester", "The 1975", "blink-182"}
	for _, name := range yes {
		if !Ambiguous(name) {
			t.Fatalf("Ambiguous(%q) = false, want true", name)
		}
	}
	for _, name := range no {
		if Ambiguous(name) {
			t.Fatalf("Ambiguous(%q) = true, want false", name)
		}
	}
}

func TestDecideRules(t *testing.T) {
	ctx := context.Background()
	// Exactly one fragment is an artist → the artist.
	matcher := &fakeMatcher{names: map[string]bool{"amanda-ginsburg": true}}
	resolver := &Resolver{Matcher: matcher, State: &State{Format: Format, Map: map[string]Decision{}}, Now: time.Now()}
	decision, err := resolver.decide(ctx, "Amanda Ginsburg - Det är jul nu igen")
	if err != nil || decision.To != "Amanda Ginsburg" || decision.Why != "split" {
		t.Fatalf("decision = %+v, err %v", decision, err)
	}

	// Both sides are real → a co-bill, not a choice.
	matcher = &fakeMatcher{names: map[string]bool{"funeral": true, "the-crypt": true}}
	resolver = &Resolver{Matcher: matcher, State: &State{Format: Format, Map: map[string]Decision{}}}
	decision, _ = resolver.decide(ctx, "FUNERAL | THE CRYPT")
	if decision.To != "" || decision.Why != "undecided" {
		t.Fatalf("co-bill decision = %+v, want undecided", decision)
	}

	// The whole string is an artist → keep it, never split.
	matcher = &fakeMatcher{names: map[string]bool{"kaizers-orchestra-live": true}}
	resolver = &Resolver{Matcher: matcher, State: &State{Format: Format, Map: map[string]Decision{}}}
	decision, _ = resolver.decide(ctx, "Kaizers Orchestra - Live")
	if decision.To != "Kaizers Orchestra - Live" || decision.Why != "kept" {
		t.Fatalf("kept decision = %+v", decision)
	}
}

func TestResolveCachesAndRespectsTheLimit(t *testing.T) {
	matcher := &fakeMatcher{names: map[string]bool{
		"amanda-ginsburg": true,
		"lisa-nilsson":    true,
	}}
	resolver := &Resolver{Matcher: matcher, State: &State{Format: Format, Map: map[string]Decision{}}, Now: time.Now()}
	names := []string{
		"Amanda Ginsburg - Det är jul nu igen",
		"När det lider mot jul - Lisa Nilsson kl 19.00",
		"FUNERAL | THE CRYPT",
	}

	report, err := resolver.Resolve(context.Background(), names, 1)
	if err != nil {
		t.Fatal(err)
	}
	if report.Lookups != 1 || report.Skipped != 2 {
		t.Fatalf("report = %+v, want 1 lookup and 2 skipped", report)
	}

	report, err = resolver.Resolve(context.Background(), names, 0)
	if err != nil {
		t.Fatal(err)
	}
	if report.Lookups != 2 || report.Resolved != 1 {
		// The second name resolves; the co-bill is undecided.
		t.Fatalf("second pass = %+v", report)
	}

	// A third pass asks nothing: every name has a decision.
	before := len(matcher.calls)
	report, _ = resolver.Resolve(context.Background(), names, 0)
	if report.Lookups != 0 || len(matcher.calls) != before {
		t.Fatalf("third pass asked again: %+v, %d calls", report, len(matcher.calls)-before)
	}
}

func TestProviderFailureIsNotADecision(t *testing.T) {
	matcher := &fakeMatcher{names: map[string]bool{}, fail: map[string]bool{"broken-show": true}}
	resolver := &Resolver{Matcher: matcher, State: &State{Format: Format, Map: map[string]Decision{}}, Now: time.Now()}
	report, err := resolver.Resolve(context.Background(), []string{"Broken Show - Artist"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if report.Failures != 1 {
		t.Fatalf("report = %+v, want one failure", report)
	}
	if len(resolver.State.Map) != 0 {
		t.Fatalf("a failure was stored: %+v", resolver.State.Map)
	}
}

func TestNegativeDecisionExpiresAndPositiveDoesNot(t *testing.T) {
	now := time.Now()
	resolver := &Resolver{
		Matcher: &fakeMatcher{names: map[string]bool{}},
		State: &State{Format: Format, Map: map[string]Decision{
			model.Key("Old - Undecided"):   {To: "", Why: "undecided", At: now.Add(-31 * 24 * time.Hour)},
			model.Key("Fresh - Undecided"): {To: "", Why: "undecided", At: now.Add(-24 * time.Hour)},
			model.Key("Gone - Artist"):     {To: "Gone", Why: "split", At: now.Add(-365 * 24 * time.Hour)},
		}},
		Now: now,
	}
	report, _ := resolver.Resolve(context.Background(), []string{"Old - Undecided", "Fresh - Undecided", "Gone - Artist"}, 0)
	if report.Lookups != 1 {
		t.Fatalf("lookups = %d, want only the expired negative re-asked", report.Lookups)
	}
}

func TestApplyRewritesBeforeTheMerge(t *testing.T) {
	resolver := &Resolver{State: &State{Format: Format, Map: map[string]Decision{
		model.Key("Amanda Ginsburg - Det är jul nu igen"): {To: "Amanda Ginsburg", Why: "split"},
		model.Key("Debaser - Debaser"):                    {To: "Debaser", Why: "split"},
	}}}
	shows := []model.Show{
		{Artist: "Amanda Ginsburg - Det är jul nu igen", ArtistKey: model.Key("Amanda Ginsburg - Det är jul nu igen"), Venue: "Pustervik", VenueKey: "pustervik"},
		{Artist: "Debaser - Debaser", ArtistKey: model.Key("Debaser - Debaser"), Venue: "Debaser", VenueKey: "debaser"},
		{Artist: "Opeth", ArtistKey: "opeth", Venue: "Pustervik", VenueKey: "pustervik"},
	}
	changed := resolver.Apply(shows)
	if changed != 1 {
		t.Fatalf("changed = %d, want 1 (the venue-equal decision is refused)", changed)
	}
	if shows[0].Artist != "Amanda Ginsburg" || shows[0].ArtistKey != "amanda-ginsburg" {
		t.Fatalf("show 0 = %+v", shows[0])
	}
	if shows[1].Artist != "Debaser - Debaser" {
		t.Fatalf("the venue-equal decision was applied: %+v", shows[1])
	}
}

func TestStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artists.json")
	state := &State{Format: Format, Map: map[string]Decision{
		"amanda-ginsburg-det-ar-jul-nu-igen": {To: "Amanda Ginsburg", Why: "split", At: time.Now().UTC().Truncate(time.Second)},
	}}
	if err := state.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Map["amanda-ginsburg-det-ar-jul-nu-igen"].To != "Amanda Ginsburg" {
		t.Fatalf("loaded = %+v", loaded.Map)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("the atomic write left a temp file (err %v)", err)
	}
}
