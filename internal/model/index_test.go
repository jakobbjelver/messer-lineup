package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// fullShow is a show with every content field filled: the fixture the projection tests are worthless
// without, because an empty Show cannot leak anything.
func fullShow(t *testing.T) Show {
	t.Helper()
	start, err := time.Parse(time.RFC3339, "2026-11-19T20:00:00+01:00")
	if err != nil {
		t.Fatalf("parse start: %v", err)
	}
	doors := start.Add(-time.Hour)
	return Show{
		Artist: "Viagra Boys", ArtistKey: "viagra-boys",
		Venue: "Vega", VenueKey: "vega-cph",
		Town:  "copenhagen-dk",
		Start: start, Doors: doors, Status: OnSale,
		URL:     "https://www.billetlugen.dk/concert/x",
		Sources: []string{"billetlugen", "billetto"},
		Geo:     &Geo{Lat: 55.6707, Lon: 12.5503},

		Price:       &Price{Min: 295, Max: 450, Currency: "DKK"},
		Description: "Doors at 19.00, support act at 19.30.",
		Image:       "https://cdn.example/poster.jpg",
		LineUp:      []string{"Viagra Boys", "Support Act"},
		Seats:       []string{"GA", "Balcony A"},
	}
}

// The golden bytes. Field order and formatting are the contract: the Go writer and the TypeScript
// reader are compared on output, so a reordered struct is a broken format even when it is valid JSON.
const goldenRow = `{"k":"viagra-boys|vega-cph|2026-11-19","a":"Viagra Boys","v":"Vega","c":"copenhagen-dk",` +
	`"d":"2026-11-19T20:00:00+01:00","st":"o","u":"https://www.billetlugen.dk/concert/x",` +
	`"src":["billetlugen","billetto"],"g":[55.6707,12.5503]}`

func TestRowGoldenBytes(t *testing.T) {
	row, err := Project(fullShow(t))
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	got, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(got) != goldenRow {
		t.Fatalf("row bytes drifted from the contract:\n got %s\nwant %s", got, goldenRow)
	}
}

// TestProjectCannotCarryContent is the gate ADR-001 §2 asks for: a full-fidelity show goes through
// the projector and none of p, cur, desc, img, lineUp or seats can appear.
func TestProjectCannotCarryContent(t *testing.T) {
	show := fullShow(t)

	// First prove the fixture is loaded, or the absence checks below would pass on an empty struct.
	raw, err := json.Marshal(show)
	if err != nil {
		t.Fatalf("marshal show: %v", err)
	}
	for _, needle := range []string{"DKK", "poster.jpg", "Support Act", "Balcony A", "support act at"} {
		if !strings.Contains(string(raw), needle) {
			t.Fatalf("fixture is not full fidelity: %q missing from the marshalled Show", needle)
		}
	}

	row, err := Project(show)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	out, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal row: %v", err)
	}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal(out, &keys); err != nil {
		t.Fatalf("unmarshal row: %v", err)
	}
	want := []string{"k", "a", "v", "c", "d", "st", "u", "src", "g"}
	if len(keys) != len(want) {
		t.Fatalf("row has %d keys, want %d: %s", len(keys), len(want), out)
	}
	for _, k := range want {
		if _, ok := keys[k]; !ok {
			t.Fatalf("row is missing key %q: %s", k, out)
		}
	}
	// Forbidden on the wire (the ADR's names) and in Go's default spelling, so a field added later
	// without a tag is caught rather than smuggled.
	for _, banned := range []string{"p", "cur", "desc", "img", "lineUp", "seats", "price", "doors"} {
		if strings.Contains(string(out), `"`+banned+`"`) {
			t.Fatalf("row carries forbidden key %q: %s", banned, out)
		}
	}
	for _, needle := range []string{"DKK", "poster.jpg", "Support Act", "Balcony A", "295", "450"} {
		if strings.Contains(string(out), needle) {
			t.Fatalf("row carries content %q: %s", needle, out)
		}
	}
}

func TestRowWithoutGeoPrintsNoDistance(t *testing.T) {
	show := fullShow(t)
	show.Geo = nil
	row, err := Project(show)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	out, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(out), `"g"`) {
		t.Fatalf("a row with no coordinates must omit g, got %s", out)
	}
}

func TestProjectRejectsWhatCannotBePublished(t *testing.T) {
	cases := map[string]func(*Show){
		"no artist key": func(s *Show) { s.ArtistKey = "" },
		"no venue key":  func(s *Show) { s.VenueKey = "" },
		"no start":      func(s *Show) { s.Start = time.Time{} },
		"bad status":    func(s *Show) { s.Status = "onsale" },
		"no deep link":  func(s *Show) { s.URL = "" },
	}
	for name, breakIt := range cases {
		show := fullShow(t)
		breakIt(&show)
		if _, err := Project(show); err == nil {
			t.Fatalf("%s: projection succeeded, want an error", name)
		}
	}
}

func TestRowKeyIsRecomputable(t *testing.T) {
	show := fullShow(t)
	row, err := Project(show)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if row.K != "viagra-boys|vega-cph|2026-11-19" {
		t.Fatalf("key = %q", row.K)
	}
	// The date half is the venue's local date, not UTC: a 23:30 show in Copenhagen is the 19th even
	// though it is already the 20th in UTC+0 terms for some offsets.
	if RowKey(show.ArtistKey, show.VenueKey, show.Start) != row.K {
		t.Fatalf("RowKey disagrees with Project")
	}
}

func TestStatusValid(t *testing.T) {
	for _, s := range []Status{OnSale, Cancelled, Postponed, SoldOut} {
		if !s.Valid() {
			t.Fatalf("%q should be valid", s)
		}
	}
	for _, s := range []Status{"", "onsale", "O", "o "} {
		if s.Valid() {
			t.Fatalf("%q should be rejected", s)
		}
	}
}

func TestWindowMarshalsAsDates(t *testing.T) {
	from, _ := time.Parse(dateLayout, "2026-11-17")
	to, _ := time.Parse(dateLayout, "2027-03-17")
	got, err := json.Marshal(Window{From: from, To: to, Days: 120})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"from":"2026-11-17","to":"2027-03-17","days":120}`
	if string(got) != want {
		t.Fatalf("window = %s, want %s", got, want)
	}
	var back Window
	if err := json.Unmarshal(got, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Days != 120 || !back.From.Equal(from) || !back.To.Equal(to) {
		t.Fatalf("window round-trip lost data: %+v", back)
	}
}

func TestManifestAndTownsShape(t *testing.T) {
	built, _ := time.Parse(time.RFC3339, "2026-11-17T06:12:00Z")
	from, _ := time.Parse(dateLayout, "2026-11-17")
	to, _ := time.Parse(dateLayout, "2027-03-17")
	m := Manifest{
		Format:  FormatIndex,
		BuiltAt: built,
		Window:  Window{From: from, To: to, Days: 120},
		Sources: []Source{{ID: "eventim", Index: "sitemap", OK: true, ParseYield: 0.98, LastOK: built, Rows: 1000}},
		Towns:   []Index{{Town: "copenhagen-dk", Shard: "towns/9f2c.jsonl.gz", SHA256: "9f2c", Count: 412, Sources: []string{"billetlugen"}, BuiltAt: built}},
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{`"format":"lineup-index/1"`, `"built_at"`, `"window"`, `"sources"`, `"towns"`,
		`"parse_yield":0.98`, `"shard":"towns/9f2c.jsonl.gz"`, `"count":412`} {
		if !strings.Contains(string(out), key) {
			t.Fatalf("manifest is missing %s: %s", key, out)
		}
	}

	towns, err := json.Marshal(Towns{
		Format:  FormatTowns,
		BuiltAt: built,
		Towns:   []Town{{ID: "copenhagen-dk", Name: "København", Country: "dk", Lat: 55.6761, Lon: 12.5683, TZ: "Europe/Copenhagen"}},
	})
	if err != nil {
		t.Fatalf("marshal towns: %v", err)
	}
	want := `{"format":"lineup-towns/1","built_at":"2026-11-17T06:12:00Z","towns":[{"id":"copenhagen-dk","name":"København","c":"dk","lat":55.6761,"lon":12.5683,"tz":"Europe/Copenhagen"}]}`
	if string(towns) != want {
		t.Fatalf("towns.json bytes drifted:\n got %s\nwant %s", towns, want)
	}
}

func TestGeoRoundTrip(t *testing.T) {
	var g Geo
	if err := json.Unmarshal([]byte(`[55.6707,12.5503]`), &g); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if g.Lat != 55.6707 || g.Lon != 12.5503 {
		t.Fatalf("geo = %+v", g)
	}
	if err := json.Unmarshal([]byte(`[1]`), &g); err == nil {
		t.Fatalf("a one-element position must be rejected")
	}
}
