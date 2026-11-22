package genres

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/index"
	"github.com/jakobbjelver/messer-lineup/internal/model"
)

type fakeProvider struct {
	tags  map[string][]Tag
	calls []string
}

func (f *fakeProvider) TopTags(_ context.Context, artist string) ([]Tag, error) {
	f.calls = append(f.calls, artist)
	return f.tags[artist], nil
}

func testIndex(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	day := time.Date(2026, 12, 2, 20, 0, 0, 0, time.UTC)
	shows := []model.Show{
		{Artist: "Opeth", ArtistKey: "opeth", Venue: "Vega", VenueKey: "vega", Town: "copenhagen-dk", TownName: "København",
			Start: day, Status: model.OnSale, URL: "https://t.test/1", Sources: []string{"tickster"}},
		{Artist: "Robyn", ArtistKey: "robyn", Venue: "Vega", VenueKey: "vega", Town: "copenhagen-dk", TownName: "København",
			Start: day, Status: model.OnSale, URL: "https://t.test/2", Sources: []string{"tickster"}},
		{Artist: "Nobody", ArtistKey: "nobody", Venue: "Vega", VenueKey: "vega", Town: "copenhagen-dk", TownName: "København",
			Start: day, Status: model.OnSale, URL: "https://t.test/3", Sources: []string{"tickster"}},
	}
	set := index.NewTownSet()
	for _, show := range shows {
		set.Add(show)
	}
	built := time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC)
	if _, _, err := index.Write(dir, shows, index.WriteOptions{
		BuiltAt: built,
		Window:  model.Window{From: built, To: built.AddDate(0, 0, 120), Days: 120},
		Sources: []model.Source{{ID: "tickster", Index: "sitemap", OK: true, Rows: len(shows)}},
		Towns:   set.Towns(built).Towns,
	}); err != nil {
		t.Fatalf("write index: %v", err)
	}
	return dir
}

func TestBuildWritesBothDirections(t *testing.T) {
	dir := testIndex(t)
	provider := &fakeProvider{tags: map[string][]Tag{
		"Opeth": {{"Progressive metal", 100}, {"Progressive rock", 6}},
		"Robyn": {{"pop", 100}, {"electronic", 93}},
	}}
	state := emptyState()
	name, report, err := Build(context.Background(), Options{
		IndexDir: dir,
		State:    state,
		Provider: provider,
		Now:      time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Artists != 3 || report.WithGenre != 2 || report.Coverage < 0.66 || report.Fetched != 3 {
		t.Fatalf("report = %+v", report)
	}

	manifest, err := index.ReadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Files["genres"] != name {
		t.Fatalf("manifest.files.genres = %q, want %q", manifest.Files["genres"], name)
	}
	raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	var document Artifact
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Artist["opeth"]) != 2 || document.Artist["opeth"][0] != "metal" {
		t.Fatalf("artist[opeth] = %v", document.Artist["opeth"])
	}
	if len(document.Genres["metal"]) != 1 || document.Genres["metal"][0] != "opeth" {
		t.Fatalf("genres[metal] = %v", document.Genres["metal"])
	}
	if len(document.Genres["rock"]) != 1 || document.Genres["rock"][0] != "opeth" {
		t.Fatalf("genres[rock] = %v", document.Genres["rock"])
	}
	if _, ok := document.Artist["nobody"]; ok {
		t.Fatal("an artist with no tags must not appear in the map")
	}
}

func TestBuildRespectsTheLimitAndTheCache(t *testing.T) {
	dir := testIndex(t)
	provider := &fakeProvider{tags: map[string][]Tag{
		"Opeth": {{"metal", 100}},
		"Robyn": {{"pop", 100}},
	}}
	state := emptyState()
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

	if _, report, err := Build(context.Background(), Options{IndexDir: dir, State: state, Provider: provider, Limit: 1, Now: now}); err != nil {
		t.Fatal(err)
	} else if report.Fetched != 1 || report.Pending != 2 {
		t.Fatalf("first run = %+v, want one fetch and two pending", report)
	}

	if _, report, err := Build(context.Background(), Options{IndexDir: dir, State: state, Provider: provider, Now: now}); err != nil {
		t.Fatal(err)
	} else if report.Fetched != 2 || report.Pending != 0 {
		t.Fatalf("second run = %+v, want the remaining two", report)
	}

	if _, report, err := Build(context.Background(), Options{IndexDir: dir, State: state, Provider: provider, Now: now}); err != nil {
		t.Fatal(err)
	} else if report.Fetched != 0 {
		t.Fatalf("third run fetched %d, want nothing (the cache is the answer)", report.Fetched)
	}
	if len(provider.calls) != 3 {
		t.Fatalf("provider was asked %d times, want 3", len(provider.calls))
	}
}

func TestANegativeExpires(t *testing.T) {
	dir := testIndex(t)
	provider := &fakeProvider{tags: map[string][]Tag{"Opeth": {{"metal", 100}}}}
	state := emptyState()
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	// Robyn was asked a month ago and had nothing.
	state.Map["robyn"] = Entry{Name: "Robyn", At: now.Add(-31 * 24 * time.Hour)}

	if _, report, err := Build(context.Background(), Options{IndexDir: dir, State: state, Provider: provider, Now: now}); err != nil {
		t.Fatal(err)
	} else if report.Fetched != 3 {
		t.Fatalf("fetched = %d, want all three (the negative expired)", report.Fetched)
	}
}
