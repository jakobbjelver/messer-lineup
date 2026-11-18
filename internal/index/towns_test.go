package index

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/model"
)

func show(town, townName string, lat, lon float64, withGeo bool) model.Show {
	s := model.Show{Town: town, TownName: townName, Artist: "A", Venue: "V"}
	if withGeo {
		s.Geo = &model.Geo{Lat: lat, Lon: lon}
	}
	return s
}

func TestTownSetDiscoversAndCentroids(t *testing.T) {
	set := NewTownSet()
	set.Add(show("london-gb", "London", 51.5, -0.12, true))
	set.Add(show("london-gb", "London", 51.7, -0.30, true))
	set.Add(show("berlin-de", "Berlin", 52.5, 13.4, true))
	set.Add(show("", "", 0, 0, false)) // no town: ignored, not guessed at

	if set.Len() != 2 {
		t.Fatalf("found %d towns, want 2", set.Len())
	}
	towns := set.Towns(time.Date(2026, 11, 17, 6, 12, 0, 0, time.UTC))
	if towns.Format != model.FormatTowns {
		t.Fatalf("format = %q", towns.Format)
	}
	if len(towns.Towns) != 2 || towns.Towns[0].ID != "berlin-de" || towns.Towns[1].ID != "london-gb" {
		t.Fatalf("towns are not sorted by id: %+v", towns.Towns)
	}
	london := towns.Towns[1]
	if london.Name != "London" || london.Country != "gb" {
		t.Fatalf("london = %+v", london)
	}
	if want := 51.6; london.Lat < want-0.001 || london.Lat > want+0.001 {
		t.Fatalf("london lat = %v, want the mean of the two venues (%v)", london.Lat, want)
	}
	if want := -0.21; london.Lon < want-0.001 || london.Lon > want+0.001 {
		t.Fatalf("london lon = %v, want %v", london.Lon, want)
	}
}

func TestTownSetWithoutGeoLeavesZero(t *testing.T) {
	set := NewTownSet()
	set.Add(show("austin-us", "Austin", 0, 0, false))
	towns := set.Towns(time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC))
	if towns.Towns[0].Lat != 0 || towns.Towns[0].Lon != 0 {
		t.Fatalf("a town with no positions invented one: %+v", towns.Towns[0])
	}
	if towns.Towns[0].ID != "austin-us" || towns.Towns[0].Country != "us" {
		t.Fatalf("town = %+v", towns.Towns[0])
	}
}

func TestTownsAreStableBytes(t *testing.T) {
	built := time.Date(2026, 11, 17, 6, 12, 0, 0, time.UTC)
	mk := func() []byte {
		set := NewTownSet()
		set.Add(show("london-gb", "London", 51.5, -0.12, true))
		set.Add(show("berlin-de", "Berlin", 52.5, 13.4, true))
		out, err := json.Marshal(set.Towns(built))
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return out
	}
	first, second := mk(), mk()
	if string(first) != string(second) {
		t.Fatalf("towns.json is not stable across runs:\n%s\n%s", first, second)
	}
	// It must also be readable by the verifier, which is the contract that matters.
	var back model.Towns
	if err := json.Unmarshal(first, &back); err != nil {
		t.Fatalf("round-trip: %v", err)
	}
}

func TestCountryOf(t *testing.T) {
	cases := map[string]string{
		"london-gb":   "gb",
		"new-york-us": "us",
		"berlin-de":   "de",
		"london":      "",
		"":            "",
		"london-":     "",
		"london-gbr":  "",
	}
	for id, want := range cases {
		if got := countryOf(id); got != want {
			t.Errorf("countryOf(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestACompactLabelNamesTheTown(t *testing.T) {
	// One town id, three labels: the platform names the district as often as the city. The shortest is the city
	// the other two belong to, and it is the one the board prints as the sign.
	set := NewTownSet()
	for _, name := range []string{"København Ø", "København NV", "København"} {
		set.Add(model.Show{Artist: "a", Venue: "v", Town: "copenhagen-dk", TownName: name, Start: time.Date(2026, 12, 2, 20, 0, 0, 0, time.UTC)})
	}
	towns := set.Towns(time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC))
	if len(towns.Towns) != 1 {
		t.Fatalf("expected one town, got %d", len(towns.Towns))
	}
	if got := towns.Towns[0].Name; got != "København" {
		t.Fatalf("town name = %q, want København", got)
	}
}
