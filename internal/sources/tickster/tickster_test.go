package tickster

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/fetch"
	"github.com/jakobbjelver/messer-lineup/internal/model"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return raw
}

func newSource() *Source { return New(fetch.New(fetch.Options{Interval: -1})) }

// recorded serves the fixtures and remembers what was asked for.
func recorded(t *testing.T, routes map[string][]byte) (*httptest.Server, *[]string) {
	t.Helper()
	var paths []string
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("User-agent: *\nCrawl-delay: 2\n"))
	})
	for path, body := range routes {
		path, body := path, body
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			paths = append(paths, r.URL.RequestURI())
			_, _ = w.Write(body)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &paths
}

func TestDetailReadsTheMicrodata(t *testing.T) {
	srv, _ := recorded(t, map[string][]byte{
		"/dk/da/events/ttz0xn0rrklka9p/2026-11-27/a-proper-house-party": fixture(t, "event.html"),
	})
	src := newSource().WithBase(srv.URL)
	show, err := src.Detail(context.Background(), model0(srv.URL+"/dk/da/events/ttz0xn0rrklka9p/2026-11-27/a-proper-house-party"))
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if !strings.Contains(show.Artist, "A PROPER HOUSE PARTY") {
		t.Fatalf("artist = %q", show.Artist)
	}
	if show.Venue != "Pumpehuset" {
		t.Fatalf("venue = %q, want the Place's own name", show.Venue)
	}
	if show.Town != "copenhagen-dk" {
		t.Fatalf("town = %q, want the id DICE would also produce", show.Town)
	}
	if show.TownName != "København" {
		t.Fatalf("town name = %q", show.TownName)
	}
	if show.Status != "o" {
		t.Fatalf("status = %q", show.Status)
	}
	// 27 November is CET: the page states a naive local time and the offset must be the town's, not the
	// machine's.
	if got := show.Start.Format(time.RFC3339); got != "2026-11-27T22:00:00+01:00" {
		t.Fatalf("start = %s, want 2026-11-27T22:00:00+01:00", got)
	}
	if show.URL == "" || show.ArtistKey == "" || show.VenueKey == "" {
		t.Fatalf("identity incomplete: %+v", show)
	}
}

func TestADistrictResolvesToItsCity(t *testing.T) {
	// Tickster spells districts where DICE spells a city; one town id, or the same city splits in two shards.
	page := strings.Replace(string(fixture(t, "event.html")), "K&#248;benhavn", "K&#248;benhavn NV", 1)
	srv, _ := recorded(t, map[string][]byte{"/dk/da/events/x/2026-11-27/y": []byte(page)})
	src := newSource().WithBase(srv.URL)
	show, err := src.Detail(context.Background(), model0(srv.URL+"/dk/da/events/x/2026-11-27/y"))
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if show.Town != "copenhagen-dk" {
		t.Fatalf("town = %q, want the same id as the city", show.Town)
	}
}

func TestAnUnknownTownGetsADerivedId(t *testing.T) {
	// Towns are discovered, so a name the miner has no alias for becomes a deterministic id rather than a
	// refusal — and the alias table only has to hold the spellings that must agree with another source.
	page := strings.Replace(string(fixture(t, "event.html")), "K&#248;benhavn", "Herning", 1)
	srv, _ := recorded(t, map[string][]byte{"/dk/da/events/x/2026-11-27/y": []byte(page)})
	src := newSource().WithBase(srv.URL)
	show, err := src.Detail(context.Background(), model0(srv.URL+"/dk/da/events/x/2026-11-27/y"))
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if show.Town != "herning-dk" {
		t.Fatalf("town = %q, want herning-dk", show.Town)
	}
}

func TestSlugifySpellsOutTheNordicLetters(t *testing.T) {
	cases := map[string]string{
		"København":  "koebenhavn",
		"Århus C":    "aarhus-c",
		"Ærøskøbing": "aeroeskoebing",
		"Odense SV":  "odense-sv",
		"  ":         "",
	}
	for name, want := range cases {
		if got := slugify(name); got != want {
			t.Fatalf("slugify(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestATownThatCannotBeResolvedIsAnError(t *testing.T) {
	page := strings.Replace(string(fixture(t, "event.html")), `<span itemprop="addressLocality">K&#248;benhavn</span>`, `<span itemprop="addressLocality"></span>`, 1)
	srv, _ := recorded(t, map[string][]byte{"/dk/da/events/x/2026-11-27/y": []byte(page)})
	src := newSource().WithBase(srv.URL)
	if _, err := src.Detail(context.Background(), model0(srv.URL+"/dk/da/events/x/2026-11-27/y")); err == nil {
		t.Fatal("a page with no town produced a show")
	}
}

func TestAPageWithNoStartDateIsAnError(t *testing.T) {
	page := strings.Replace(string(fixture(t, "event.html")), `itemprop="startDate" content="2026-11-27T22:00:00"`, `itemprop="startDate"`, 1)
	srv, _ := recorded(t, map[string][]byte{"/dk/da/events/x/2026-11-27/y": []byte(page)})
	src := newSource().WithBase(srv.URL)
	if _, err := src.Detail(context.Background(), model0(srv.URL+"/dk/da/events/x/2026-11-27/y")); err == nil {
		t.Fatal("a page with no start date produced a show")
	}
}

func TestACancellationLabelIsHonoured(t *testing.T) {
	page := strings.Replace(string(fixture(t, "event.html")), "<article", "<article>\n<span class=\"c-label\">Aflyst</span>", 1)
	srv, _ := recorded(t, map[string][]byte{"/dk/da/events/x/2026-11-27/y": []byte(page)})
	src := newSource().WithBase(srv.URL)
	show, err := src.Detail(context.Background(), model0(srv.URL+"/dk/da/events/x/2026-11-27/y"))
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if show.Status != "c" {
		t.Fatalf("status = %q, want c", show.Status)
	}
}

func TestTownCandidatesPagesUntilShort(t *testing.T) {
	// A city page renders sixteen events and hides the rest behind ?skip=; the walk stops on the short page.
	link := func(n int) string {
		return fmt.Sprintf(`<a href="/dk/da/events/abcdefghijklmn%d/2026-12-%02d/slug">x</a>`, n, (n%28)+1)
	}
	first := strings.Builder{}
	for i := 0; i < pageSize; i++ {
		first.WriteString(link(i))
	}
	second := strings.Builder{}
	for i := pageSize; i < pageSize+3; i++ {
		second.WriteString(link(i))
	}
	srv, paths := recorded(t, map[string][]byte{"/dk/da/events/in/k%C3%B8benhavn": []byte(first.String())})
	// The same handler answers the offset pages, so the fixture is per-request.
	var calls int32
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			_, _ = w.Write([]byte("User-agent: *\n"))
		case "/dk/da/events/in/københavn":
			if r.URL.Query().Get("skip") == "" {
				atomic.AddInt32(&calls, 1)
				_, _ = w.Write([]byte(first.String()))
				return
			}
			_, _ = w.Write([]byte(second.String()))
		default:
			http.NotFound(w, r)
		}
	})

	src := newSource().WithBase(srv.URL)
	candidates, err := src.TownCandidates(context.Background(), "copenhagen")
	if err != nil {
		t.Fatalf("candidates: %v", err)
	}
	if len(candidates) != pageSize+3 {
		t.Fatalf("candidates = %d, want %d", len(candidates), pageSize+3)
	}
	if candidates[0].Source != ID {
		t.Fatalf("source = %q", candidates[0].Source)
	}
	if len(*paths) != 0 && !strings.Contains(strings.Join(*paths, " "), "skip=16") {
		t.Fatalf("the second page was not requested by offset: %v", *paths)
	}
}

func TestAnUnknownTownSaysWhatIsKnown(t *testing.T) {
	src := newSource()
	_, err := src.TownCandidates(context.Background(), "atlantis")
	if err == nil || !strings.Contains(err.Error(), "copenhagen") {
		t.Fatalf("err = %v, want a refusal that lists the towns it can mine", err)
	}
}

func TestTheCityPageFixtureYieldsCandidates(t *testing.T) {
	// The real page, kept in testdata: if Tickster changes its markup this test is the alarm.
	if got := len(eventLinks(fixture(t, "city.html"))); got < 10 {
		t.Fatalf("the city fixture yielded %d links; the markup has changed", got)
	}
}

func TestLocaleComesFromTheURL(t *testing.T) {
	cases := map[string][2]string{
		"https://www.tickster.com/dk/da/events/x/2026-11-27/y": {"dk", "Europe/Copenhagen"},
		"https://www.tickster.com/se/sv/events/x/2026-11-27/y": {"se", "Europe/Stockholm"},
		"https://www.tickster.com/no/nb/events/x/2026-11-27/y": {"no", "Europe/Oslo"},
		"https://example.test/event/1":                         {"", ""},
	}
	for url, want := range cases {
		country, zone := localeOf(url)
		if country != want[0] || zone != want[1] {
			t.Fatalf("%s: got %q/%q, want %q/%q", url, country, zone, want[0], want[1])
		}
	}
}

func TestParseLocalKeepsAnOffsetThatExists(t *testing.T) {
	// A naive value takes the town's zone; one that states an offset is honoured rather than overwritten.
	cph, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil {
		t.Fatalf("zone: %v", err)
	}
	if got, err := parseLocal("2026-07-01T20:00:00", cph); err != nil || got.Format(time.RFC3339) != "2026-07-01T20:00:00+02:00" {
		t.Fatalf("summer naive value: %v %v", got, err)
	}
	if got, err := parseLocal("2026-07-01T18:00:00Z", cph); err != nil || got.Format(time.RFC3339) != "2026-07-01T18:00:00Z" {
		t.Fatalf("explicit offset: %v %v", got, err)
	}
}

// model0 is the candidate one URL becomes; the name is a reminder that only the URL matters here.
func model0(u string) model.Candidate { return model.Candidate{URL: u, Source: ID} }
