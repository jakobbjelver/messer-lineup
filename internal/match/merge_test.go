package match

import (
	"testing"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/model"
)

func at(day int) time.Time { return time.Date(2026, 12, day, 20, 0, 0, 0, time.UTC) }

func gig(artist, venue string, day int, sources ...string) model.Show {
	return model.Show{
		Artist:    artist,
		ArtistKey: model.Key(artist),
		Venue:     venue,
		VenueKey:  model.Key(venue),
		Town:      "copenhagen-dk",
		Start:     at(day),
		Status:    model.OnSale,
		URL:       "https://seller.test/" + model.Key(venue) + "/" + at(day).Format("2006-01-02"),
		Sources:   sources,
	}
}

func withGeo(s model.Show, lat, lon float64) model.Show {
	s.Geo = &model.Geo{Lat: lat, Lon: lon}
	return s
}

func TestTheSameGigFromTwoSourcesMerges(t *testing.T) {
	shows := []model.Show{
		gig("Anna von Hausswolff", "Vega", 2, "tickster"),
		gig("Anna von Hausswolff", "Vega", 2, "dice"),
	}
	out, stats := Merge(shows)
	if len(out) != 1 {
		t.Fatalf("merged to %d shows, want 1", len(out))
	}
	if stats.Merged != 1 || stats.Raw != 2 || stats.Out != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	if stats.DuplicateRate != 0.5 {
		t.Fatalf("duplicate rate = %v, want 0.5", stats.DuplicateRate)
	}
	srcs := out[0].Sources
	if len(srcs) != 2 || srcs[0] != "dice" || srcs[1] != "tickster" {
		t.Fatalf("sources = %v, want both, sorted", srcs)
	}
}

func TestTwoGigsAtOneRoomInOneNightDoNotMerge(t *testing.T) {
	// Two different artists, same venue, same night: the early and the late show.
	shows := []model.Show{
		gig("Artist One", "Vega", 2, "dice"),
		gig("Artist Two", "Vega", 2, "dice"),
	}
	out, stats := Merge(shows)
	if len(out) != 2 || stats.Merged != 0 {
		t.Fatalf("out = %d, stats = %+v — different artists are different shows", len(out), stats)
	}
}

func TestOneArtistTwoNightsStayTwoShows(t *testing.T) {
	shows := []model.Show{
		gig("Efterklang", "Pumpehuset", 2, "dice"),
		gig("Efterklang", "Pumpehuset", 3, "dice"),
	}
	if out, _ := Merge(shows); len(out) != 2 {
		t.Fatalf("out = %d, want a two-night residency to stay two rows", len(out))
	}
}

func TestAVenueSpellingDifferenceMergesOnPosition(t *testing.T) {
	// "Store Vega" and "Vega" are one room; both platforms place it at the same corner.
	shows := []model.Show{
		withGeo(gig("Trentemøller", "Store Vega", 9, "dice"), 55.6687, 12.5526),
		withGeo(gig("Trentemøller", "Vega", 9, "tickster"), 55.6689, 12.5528),
	}
	out, stats := Merge(shows)
	if len(out) != 1 || stats.Merged != 1 {
		t.Fatalf("out = %d, stats = %+v — 200 m apart is the same room", len(out), stats)
	}
	if len(out[0].VenueAliases) != 1 {
		t.Fatalf("aliases = %v, want the other spelling kept", out[0].VenueAliases)
	}
}

func TestAFarApartVenueDoesNotMergeOnPosition(t *testing.T) {
	// Same artist, same night, two rooms 3 km apart: a double booking in two towns, not a duplicate.
	shows := []model.Show{
		withGeo(gig("Trentemøller", "Vega", 9, "dice"), 55.6687, 12.5526),
		withGeo(gig("Trentemøller", "DR Koncerthuset", 9, "tickster"), 55.6576, 12.5884),
	}
	if out, _ := Merge(shows); len(out) != 2 {
		t.Fatalf("out = %d, want two rows", len(out))
	}
}

func TestOneSidedPositionNeverMerges(t *testing.T) {
	shows := []model.Show{
		withGeo(gig("Trentemøller", "Store Vega", 9, "dice"), 55.6687, 12.5526),
		gig("Trentemøller", "Vega", 9, "tickster"),
	}
	if out, _ := Merge(shows); len(out) != 2 {
		t.Fatalf("out = %d — without a position on both sides the names decide, and they differ", len(out))
	}
}

func TestACancellationWins(t *testing.T) {
	cancelled := gig("Efterklang", "Pumpehuset", 2, "dice")
	cancelled.Status = model.Cancelled
	shows := []model.Show{gig("Efterklang", "Pumpehuset", 2, "tickster"), cancelled}
	out, _ := Merge(shows)
	if len(out) != 1 || out[0].Status != model.Cancelled {
		t.Fatalf("status = %q, want the cancellation to survive the merge", out[0].Status)
	}
}

func TestMergeIsOrderIndependent(t *testing.T) {
	a := withGeo(gig("Trentemøller", "Store Vega", 9, "dice"), 55.6687, 12.5526)
	b := withGeo(gig("Trentemøller", "Vega", 9, "tickster"), 55.6689, 12.5528)
	c := gig("Efterklang", "Pumpehuset", 2, "dice")

	first, statsFirst := Merge([]model.Show{a, b, c})
	second, statsSecond := Merge([]model.Show{c, b, a})
	if len(first) != len(second) || statsFirst != statsSecond {
		t.Fatalf("order changed the result: %d/%+v vs %d/%+v", len(first), statsFirst, len(second), statsSecond)
	}
	for i := range first {
		ka, kb := model.RowKey(first[i].ArtistKey, first[i].VenueKey, first[i].Start), model.RowKey(second[i].ArtistKey, second[i].VenueKey, second[i].Start)
		if ka != kb {
			t.Fatalf("row %d differs: %q vs %q", i, ka, kb)
		}
	}
}

func TestMergeIgnoresAShowWithNoIdentity(t *testing.T) {
	if out, _ := Merge([]model.Show{{Artist: "Nobody"}}); len(out) != 0 {
		t.Fatalf("out = %d, want an unidentifiable show dropped", len(out))
	}
}
