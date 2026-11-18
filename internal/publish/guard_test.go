package publish

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jakobbjelver/messer-lineup/internal/model"
)

func manifest(towns map[string]int, sources map[string]bool, towns_ []string) *model.Manifest {
	m := &model.Manifest{Format: "lineup-index/1", Towns: []model.Index{}}
	for _, id := range towns_ {
		m.Towns = append(m.Towns, model.Index{Town: id, Count: towns[id], Sources: []string{"tickster"}})
	}
	for id, ok := range sources {
		m.Sources = append(m.Sources, model.Source{ID: id, OK: ok})
	}
	return m
}

func TestNoPreviousManifestPublishes(t *testing.T) {
	if problems := Check(nil, manifest(map[string]int{"copenhagen-dk": 37}, map[string]bool{"tickster": true}, []string{"copenhagen-dk"})); len(problems) != 0 {
		t.Fatalf("first publish was refused: %v", problems)
	}
}

func TestAnOrdinaryDayPublishes(t *testing.T) {
	previous := manifest(map[string]int{"copenhagen-dk": 40, "stockholm-se": 700}, map[string]bool{"tickster": true}, []string{"copenhagen-dk", "stockholm-se"})
	current := manifest(map[string]int{"copenhagen-dk": 37, "stockholm-se": 640}, map[string]bool{"tickster": true}, []string{"copenhagen-dk", "stockholm-se"})
	if problems := Check(previous, current); len(problems) != 0 {
		t.Fatalf("a 9 %% drop was refused: %v", problems)
	}
}

func TestAQuarterOfTheIndexVanishingIsRefused(t *testing.T) {
	previous := manifest(map[string]int{"copenhagen-dk": 100, "stockholm-se": 700}, map[string]bool{"tickster": true}, []string{"copenhagen-dk", "stockholm-se"})
	current := manifest(map[string]int{"copenhagen-dk": 100, "stockholm-se": 400}, map[string]bool{"tickster": true}, []string{"copenhagen-dk", "stockholm-se"})
	problems := Check(previous, current)
	if len(problems) == 0 {
		t.Fatal("a 37 % total drop was published")
	}
	if !strings.Contains(problems[0], "rows") {
		t.Fatalf("the problem does not say what happened: %v", problems)
	}
}

func TestATownCollapsingWhileItsSourceSaysOKIsRefused(t *testing.T) {
	// A big stable town keeps the total inside the limit, so this isolates the per-town rule: one town loses 90 %
	// of its rows and the source behind it still says ok — the signature of a parser that stopped matching.
	previous := manifest(map[string]int{"berlin-de": 900, "stockholm-se": 100}, map[string]bool{"tickster": true}, []string{"berlin-de", "stockholm-se"})
	current := manifest(map[string]int{"berlin-de": 900, "stockholm-se": 10}, map[string]bool{"tickster": true}, []string{"berlin-de", "stockholm-se"})
	problems := Check(previous, current)
	if len(problems) == 0 {
		t.Fatal("a town that lost 86 % of its rows while its source reported ok was published")
	}
	if !strings.Contains(problems[0], "stockholm-se") {
		t.Fatalf("the problem does not name the town: %v", problems)
	}
}

func TestATownCollapseWithAFailedSourceIsExplained(t *testing.T) {
	// The source reports the failure; refusing here would hide it. The drop is published and the coverage line
	// carries the reason.
	previous := manifest(map[string]int{"berlin-de": 900, "stockholm-se": 100}, map[string]bool{"tickster": true}, []string{"berlin-de", "stockholm-se"})
	current := manifest(map[string]int{"berlin-de": 900, "stockholm-se": 10}, map[string]bool{"tickster": false}, []string{"berlin-de", "stockholm-se"})
	if problems := Check(previous, current); len(problems) != 0 {
		t.Fatalf("a failure the report already states was refused: %v", problems)
	}
}

func TestATownDisappearingEntirelyIsRefused(t *testing.T) {
	previous := manifest(map[string]int{"stockholm-se": 700}, map[string]bool{"tickster": true}, []string{"stockholm-se"})
	current := manifest(map[string]int{"copenhagen-dk": 37}, map[string]bool{"tickster": true}, []string{"copenhagen-dk"})
	problems := Check(previous, current)
	if len(problems) == 0 {
		t.Fatal("a town vanished from the manifest and nothing said so")
	}
	if !strings.Contains(strings.Join(problems, " "), "disappear") {
		t.Fatalf("the problem does not say the town disappeared: %v", problems)
	}
}

func TestASmallTownIsNotGuarded(t *testing.T) {
	// Three shows becoming one is an ordinary week, not a parser break. A guard that fires on it teaches people
	// to pass --force out of habit, which is worse than no guard.
	previous := manifest(map[string]int{"berlin-de": 900, "malmo-se": 12}, map[string]bool{"tickster": true}, []string{"berlin-de", "malmo-se"})
	current := manifest(map[string]int{"berlin-de": 900, "malmo-se": 1}, map[string]bool{"tickster": true}, []string{"berlin-de", "malmo-se"})
	if problems := Check(previous, current); len(problems) != 0 {
		t.Fatalf("a small town was guarded: %v", problems)
	}
}

func TestARefusedPublishUploadsNothing(t *testing.T) {
	dir := index(t)
	b := &bucket{objects: map[string]record{}}
	// What is published today: a much larger index.
	b.previous = []byte(`{"format":"lineup-index/1","towns":[{"town":"copenhagen-dk","count":900,"sources":["tickster"]}],"sources":[{"id":"tickster","ok":true}]}`)
	srv := httptest.NewServer(b.handler(t))
	defer srv.Close()
	opts := options(dir, srv)

	_, err := Run(context.Background(), opts)
	if err == nil {
		t.Fatal("a collapsing index was published")
	}
	if _, ok := err.(Refused); !ok {
		t.Fatalf("err = %v, want Refused", err)
	}
	if len(b.puts) != 0 {
		t.Fatalf("a refused publish uploaded %d object(s)", len(b.puts))
	}
}

func TestForcePublishesAnyway(t *testing.T) {
	dir := index(t)
	b := &bucket{objects: map[string]record{}}
	b.previous = []byte(`{"format":"lineup-index/1","towns":[{"town":"copenhagen-dk","count":900,"sources":["tickster"]}],"sources":[{"id":"tickster","ok":true}]}`)
	srv := httptest.NewServer(b.handler(t))
	defer srv.Close()
	opts := options(dir, srv)
	opts.NoGuard = true
	if _, err := Run(t.Context(), opts); err != nil {
		t.Fatalf("--force still refused: %v", err)
	}
	if len(b.puts) != 4 {
		t.Fatalf("uploads = %d, want 4", len(b.puts))
	}
}
