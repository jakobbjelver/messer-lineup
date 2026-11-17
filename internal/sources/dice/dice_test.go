package dice

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jakobbjelver/messer-lineup/internal/fetch"
	"github.com/jakobbjelver/messer-lineup/internal/model"
)

// The fixtures are captures from real pages, trimmed to the fields this adapter reads:
// testdata/dice_event.json is one event's JSON-LD block from dice.fm, and dice_sitemap.xml keeps the URL
// shapes that file actually contains (events, a venue, a static page) so the filter is exercised.
func server(t *testing.T, routes map[string][]byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("")) })
	for path, body := range routes {
		body := body
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(body) })
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return body
}

// page wraps a JSON-LD document the way a real page does.
func page(t *testing.T) []byte {
	t.Helper()
	block := fixture(t, "dice_event.json")
	var compact strings.Builder
	compact.WriteString(`<html><head><script type="application/ld+json">`)
	compact.Write(block)
	compact.WriteString(`</script></head><body>visible text that must not be parsed</body></html>`)
	return []byte(compact.String())
}

func newSource() *Source { return New(fetch.New(fetch.Options{Interval: -1})) }

func TestCandidatesFromSitemap(t *testing.T) {
	srv := server(t, map[string][]byte{"/sitemap1.xml": fixture(t, "dice_sitemap.xml")})
	candidates, err := newSource().Candidates(context.Background(), srv.URL+"/sitemap1.xml")
	if err != nil {
		t.Fatalf("candidates: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("got %d candidates, want the 2 event URLs: %+v", len(candidates), candidates)
	}
	for _, c := range candidates {
		if c.Source != Name {
			t.Fatalf("candidate source = %q", c.Source)
		}
		if !strings.Contains(c.URL, "/event/") {
			t.Fatalf("candidate %q is not an event URL", c.URL)
		}
	}
}

func TestDetailFromFixture(t *testing.T) {
	srv := server(t, map[string][]byte{"/event/6af6e5fac4992d0001b4d821": page(t)})
	show, err := newSource().Detail(context.Background(), model.Candidate{URL: srv.URL + "/event/6af6e5fac4992d0001b4d821", Source: Name})
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if show.Artist != "Kingfishr" || show.ArtistKey != "kingfishr" {
		t.Fatalf("artist = %q / %q", show.Artist, show.ArtistKey)
	}
	if show.Venue != "Alexandra Palace" || show.VenueKey != "alexandra-palace" {
		t.Fatalf("venue = %q / %q", show.Venue, show.VenueKey)
	}
	if show.Town != "london-gb" {
		t.Fatalf("town = %q, want london-gb", show.Town)
	}
	if got := show.Start.UTC().Format("2006-01-02T15:04Z"); got != "2027-03-26T18:30Z" {
		t.Fatalf("start = %s", got)
	}
	if show.Status != model.OnSale {
		t.Fatalf("status = %q", show.Status)
	}
	if show.Geo == nil || show.Geo.Lat < 51 || show.Geo.Lat > 52 || show.Geo.Lon > 0 {
		t.Fatalf("geo = %+v", show.Geo)
	}
	if show.Price == nil || show.Price.Min != 55.55 || show.Price.Max != 55.55 || show.Price.Currency != "GBP" {
		t.Fatalf("price = %+v", show.Price)
	}
	if len(show.LineUp) != 1 || show.LineUp[0] != "Kingfishr" {
		t.Fatalf("line-up = %v", show.LineUp)
	}
	if !strings.Contains(show.URL, "dice.fm/event/") {
		t.Fatalf("url = %q, want the canonical event URL from the page", show.URL)
	}
	if len(show.Sources) != 1 || show.Sources[0] != Name {
		t.Fatalf("sources = %v", show.Sources)
	}
}

// TestTheRowItWouldPublish closes the loop: a DICE page becomes a row with nine keys and nothing else.
func TestTheRowItWouldPublish(t *testing.T) {
	srv := server(t, map[string][]byte{"/event/6af6e5fac4992d0001b4d821": page(t)})
	show, err := newSource().Detail(context.Background(), model.Candidate{URL: srv.URL + "/event/6af6e5fac4992d0001b4d821", Source: Name})
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	row, err := model.Project(show)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	out, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"k":"kingfishr|alexandra-palace|2027-03-26","a":"Kingfishr","v":"Alexandra Palace","c":"london-gb",` +
		`"d":"2027-03-26T18:30:00Z","st":"o","u":"https://dice.fm/event/3kq9xv-kingfishr-26th-mar-alexandra-palace-london-tickets",` +
		`"src":["dice"],"g":[51.5941783,-0.130773299999987]}`
	if string(out) != want {
		t.Fatalf("row drifted:\n got %s\nwant %s", out, want)
	}
}

func TestStatusMapping(t *testing.T) {
	cases := map[string]model.Status{
		"https://schema.org/EventScheduled": model.OnSale,
		"https://schema.org/EventCancelled": model.Cancelled,
		"https://schema.org/EventPostponed": model.Postponed,
		"":                                  model.OnSale,
	}
	for in, want := range cases {
		if got := status(in, ""); got != want {
			t.Errorf("status(%q) = %q, want %q", in, got, want)
		}
	}
	if got := status("https://schema.org/EventScheduled", "https://schema.org/SoldOut"); got != model.SoldOut {
		t.Errorf("sold out offer not mapped: %q", got)
	}
}

func TestFindEventShapes(t *testing.T) {
	oneEvent := `{"@context":"https://schema.org","@type":"MusicEvent","name":"A","startDate":"2027-01-01T20:00:00+01:00","location":{"name":"V","address":{"addressLocality":"Berlin","addressCountry":"DE"}}}`
	org := `{"@type":"Organization","name":"DICE"}`

	if _, err := findEvent([]byte(`<html><script type="application/ld+json">` + org + `</script><script type="application/ld+json">` + oneEvent + `</script></html>`)); err != nil {
		t.Fatalf("single object after a non-event block: %v", err)
	}
	if _, err := findEvent([]byte(`<script type="application/ld+json">[` + org + `,` + oneEvent + `]</script>`)); err != nil {
		t.Fatalf("event inside a list: %v", err)
	}
	if _, err := findEvent([]byte(`<script type="application/ld+json">` + org + `</script>`)); err == nil {
		t.Fatal("a page with only an Organization block was accepted")
	}
	if _, err := findEvent([]byte(`<html>no structured data</html>`)); err == nil {
		t.Fatal("a page with no JSON-LD was accepted")
	}
	escaped := `<script type="application/ld+json">{&quot;@type&quot;:&quot;MusicEvent&quot;,&quot;name&quot;:&quot;A&quot;,&quot;startDate&quot;:&quot;2027-01-01T20:00:00+01:00&quot;,&quot;location&quot;:{&quot;name&quot;:&quot;V&quot;,&quot;address&quot;:{&quot;addressLocality&quot;:&quot;Berlin&quot;,&quot;addressCountry&quot;:&quot;DE&quot;}}}</script>`
	if _, err := findEvent([]byte(escaped)); err != nil {
		t.Fatalf("HTML-escaped JSON-LD: %v", err)
	}
}

func TestTownIdIncludesTheCountry(t *testing.T) {
	body := []byte(`<script type="application/ld+json">{"@type":"MusicEvent","name":"A","startDate":"2027-01-01T20:00:00-06:00","location":{"name":"Mohawk","address":{"addressLocality":"Austin","addressCountry":"US"}}}</script>`)
	e, err := findEvent(body)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	show, err := toShow(e, "https://dice.fm/event/x")
	if err != nil {
		t.Fatalf("toShow: %v", err)
	}
	if show.Town != "austin-us" {
		t.Fatalf("town = %q, want austin-us", show.Town)
	}
}

func TestDetailRejectsAPageItCannotPlace(t *testing.T) {
	noTown := []byte(`<script type="application/ld+json">{"@type":"MusicEvent","name":"A","startDate":"2027-01-01T20:00:00Z","location":{"name":"V","address":{}}}</script>`)
	srv := server(t, map[string][]byte{"/event/x": noTown})
	if _, err := newSource().Detail(context.Background(), model.Candidate{URL: srv.URL + "/event/x"}); err == nil {
		t.Fatal("an event with no town was accepted")
	}
	noDate := []byte(`<script type="application/ld+json">{"@type":"MusicEvent","name":"A","location":{"name":"V","address":{"addressLocality":"Berlin","addressCountry":"DE"}}}</script>`)
	srv2 := server(t, map[string][]byte{"/event/y": noDate})
	if _, err := newSource().Detail(context.Background(), model.Candidate{URL: srv2.URL + "/event/y"}); err == nil {
		t.Fatal("an event with no start date was accepted")
	}
}
