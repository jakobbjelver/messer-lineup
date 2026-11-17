package index

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/model"
)

func build(t *testing.T, dir string, shows []model.Show, prune bool) (model.Manifest, Stats) {
	t.Helper()
	set := NewTownSet()
	for _, show := range shows {
		set.Add(show)
	}
	window := model.Window{
		From: time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2027, 3, 17, 0, 0, 0, 0, time.UTC),
		Days: DefaultWindowDays,
	}
	manifest, stats, err := Write(dir, shows, WriteOptions{
		BuiltAt: time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC),
		Window:  window,
		Sources: []model.Source{{ID: "dice", Index: "sitemap", OK: true, ParseYield: 1, Rows: len(shows)}},
		Towns:   set.Towns(time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC)).Towns,
		Prune:   prune,
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	return manifest, stats
}

func newShow(artist, venue, town string, start time.Time, src string) model.Show {
	return model.Show{
		Artist:    artist,
		ArtistKey: model.Key(artist),
		Venue:     venue,
		VenueKey:  model.Key(venue),
		Town:      town,
		Start:     start,
		Status:    model.OnSale,
		URL:       "https://example.test/" + model.Key(venue) + "/" + start.Format("2006-01-02"),
		Sources:   []string{src},
	}
}

func TestWriteThenVerify(t *testing.T) {
	dir := t.TempDir()
	shows := []model.Show{
		newShow("Anna von Hausswolff", "Vega", "copenhagen-dk", time.Date(2026, 12, 2, 20, 0, 0, 0, time.UTC), "tickster"),
		newShow("Efterklang", "Pumpehuset", "copenhagen-dk", time.Date(2026, 12, 5, 20, 0, 0, 0, time.UTC), "tickster"),
		newShow("Trentemøller", "Store Vega", "copenhagen-dk", time.Date(2027, 1, 9, 20, 0, 0, 0, time.UTC), "dice"),
	}
	manifest, stats := build(t, dir, shows, false)
	if stats.Shards != 1 || stats.Towns != 1 || stats.Published != 3 {
		t.Fatalf("stats = %+v", stats)
	}
	if len(manifest.Towns) != 1 || manifest.Towns[0].Count != 3 || manifest.Towns[0].Town != "copenhagen-dk" {
		t.Fatalf("manifest towns = %+v", manifest.Towns)
	}
	if manifest.Towns[0].Sources[0] != "dice" || len(manifest.Towns[0].Sources) != 2 {
		t.Fatalf("a town's sources must be every source that carried a row: %v", manifest.Towns[0].Sources)
	}

	// The writer's output is the verifier's input: this is the loop the product depends on.
	report, err := Verify(dir, time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !report.OK() {
		t.Fatalf("verify refused the writer's own index: %v", report.Problems)
	}

	rows, err := ReadShard(filepath.Join(dir, manifest.Towns[0].Shard))
	if err != nil {
		t.Fatalf("read shard: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d", len(rows))
	}
	// Sorted by k then d, so the file itself is stable.
	for i := 1; i < len(rows); i++ {
		if rows[i-1].K > rows[i].K {
			t.Fatalf("rows are not sorted by k: %q before %q", rows[i-1].K, rows[i].K)
		}
	}
}

func TestWriteIsDeterministic(t *testing.T) {
	shows := func() []model.Show {
		return []model.Show{
			newShow("Anna von Hausswolff", "Vega", "copenhagen-dk", time.Date(2026, 12, 2, 20, 0, 0, 0, time.UTC), "tickster"),
			newShow("Efterskæg", "Pumpehuset", "copenhagen-dk", time.Date(2026, 12, 5, 20, 0, 0, 0, time.UTC), "dice"),
		}
	}
	first, _ := build(t, t.TempDir(), shows(), false)
	second, _ := build(t, t.TempDir(), shows(), false)
	if first.Towns[0].SHA256 != second.Towns[0].SHA256 {
		t.Fatalf("the same shows produced two shards: %s vs %s", first.Towns[0].SHA256, second.Towns[0].SHA256)
	}
}

func TestWriteDropsWhatCannotBePublished(t *testing.T) {
	dir := t.TempDir()
	shows := []model.Show{
		newShow("Inside", "Vega", "copenhagen-dk", time.Date(2026, 12, 2, 20, 0, 0, 0, time.UTC), "tickster"),
		newShow("Too late", "Vega", "copenhagen-dk", time.Date(2027, 6, 1, 20, 0, 0, 0, time.UTC), "tickster"),
		newShow("Too early", "Vega", "copenhagen-dk", time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC), "tickster"),
		{Artist: "No keys", ArtistKey: "", Venue: "Vega", VenueKey: "", Town: "copenhagen-dk",
			Start: time.Date(2026, 12, 3, 20, 0, 0, 0, time.UTC), Status: model.OnSale, URL: "https://example.test/x"},
		{Artist: "No link", ArtistKey: model.Key("No link"), Venue: "Vega", VenueKey: model.Key("Vega"),
			Town: "copenhagen-dk", Start: time.Date(2026, 12, 4, 20, 0, 0, 0, time.UTC), Status: model.OnSale},
	}
	_, stats := build(t, dir, shows, false)
	if stats.Published != 1 || stats.Outside != 2 || stats.Refused != 2 {
		t.Fatalf("stats = %+v, want 1 published, 2 outside the window, 2 refused", stats)
	}
}

func TestPruneRemovesShardsTheManifestNoLongerNames(t *testing.T) {
	dir := t.TempDir()
	keep := newShow("Stays", "Vega", "copenhagen-dk", time.Date(2026, 12, 2, 20, 0, 0, 0, time.UTC), "tickster")
	first, _ := build(t, dir, []model.Show{keep, newShow("Leaves", "Vega", "copenhagen-dk", time.Date(2026, 12, 9, 20, 0, 0, 0, time.UTC), "tickster")}, false)
	if len(first.Towns) != 1 {
		t.Fatalf("expected one town")
	}
	stale := first.Towns[0].Shard
	if _, err := os.Stat(filepath.Join(dir, stale)); err != nil {
		t.Fatalf("first shard missing: %v", err)
	}
	second, _ := build(t, dir, []model.Show{keep}, true)
	if second.Towns[0].SHA256 == first.Towns[0].SHA256 {
		t.Fatal("the rebuild produced the same digest, so this test proves nothing")
	}
	if _, err := os.Stat(filepath.Join(dir, stale)); !os.IsNotExist(err) {
		t.Fatalf("prune left the stale shard behind: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, second.Towns[0].Shard)); err != nil {
		t.Fatalf("prune removed the shard it just wrote: %v", err)
	}
}

func TestWriteRefusesABuildWithNoClock(t *testing.T) {
	if _, _, err := Write(t.TempDir(), nil, WriteOptions{}); err == nil {
		t.Fatal("a build without its injected clock must fail, because BuiltAt is published")
	}
}
