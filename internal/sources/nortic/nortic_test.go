package nortic

import (
	"context"
	"errors"
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

func newSource() *Source { return New(fetch.New(fetch.Options{Interval: -1})) }

func page(t *testing.T, id string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "pages", id+".html"))
	if err != nil {
		t.Fatalf("fixture %s: %v", id, err)
	}
	return body
}

// TestParseSingleEvent pins the whole projection on a real page: Plan B, a doors time of 18:00, a set time of 20:00.
func TestParseSingleEvent(t *testing.T) {
	shows, err := parseEvents(page(t, "83019"), "https://tickets.nortic.se/ticket/event/83019")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(shows) != 1 {
		t.Fatalf("got %d shows, want 1", len(shows))
	}
	show := shows[0]
	if show.Artist != "Hannes Aitman" || show.Venue != "Plan B" {
		t.Fatalf("artist/venue = %q / %q", show.Artist, show.Venue)
	}
	if show.Town != "malmo-se" || show.TownName != "Malmö" {
		t.Fatalf("town = %q / %q", show.Town, show.TownName)
	}
	// "[Konsert] Doors 6 PM On stage 8 PM" — 20:00, not the doors time.
	if got := show.Start.Format("2006-01-02 15:04"); got != "2026-11-21 20:00" {
		t.Fatalf("start = %s, want 2026-11-21 20:00", got)
	}
	if show.Status != model.OnSale {
		t.Fatalf("status = %q", show.Status)
	}
	if show.ArtistKey != "hannes-aitman" || show.VenueKey != "plan-b" {
		t.Fatalf("keys = %q / %q", show.ArtistKey, show.VenueKey)
	}
}

// TestParseMultiDatePage keeps every date on a page, and every town: one page can be two nights in two cities.
func TestParseMultiDatePage(t *testing.T) {
	shows, err := parseEvents(page(t, "74827"), "https://tickets.nortic.se/ticket/event/74827")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(shows) != 2 {
		t.Fatalf("got %d shows, want 2", len(shows))
	}
	if shows[0].Artist != "KISS" || shows[1].Artist != "KISS" {
		t.Fatalf("artists = %q / %q", shows[0].Artist, shows[1].Artist)
	}
	if shows[0].Town != "malmo-se" || shows[1].Town != "kosta-se" {
		t.Fatalf("towns = %q / %q", shows[0].Town, shows[1].Town)
	}
	if got := shows[0].Start.Format("2006-01-02"); got != "2027-01-27" {
		t.Fatalf("first day = %s", got)
	}
	if shows[0].Venue != "Stadionmässan" || shows[1].Venue != "Craft Arena" {
		t.Fatalf("venues = %q / %q", shows[0].Venue, shows[1].Venue)
	}
}

// TestFestivalSlotsCollapseToOneRowPerDay: five slots over two days are two rows, because the row key is the day.
func TestFestivalSlotsCollapseToOneRowPerDay(t *testing.T) {
	shows, err := parseEvents(page(t, "85860"), "https://tickets.nortic.se/ticket/event/85860")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(shows) != 2 {
		t.Fatalf("got %d shows, want one per day (2)", len(shows))
	}
	if !strings.Contains(shows[0].Artist, "&") {
		t.Fatalf("artist %q kept its HTML entity", shows[0].Artist)
	}
}

// TestNonMusicPagesAreFacts: the platform's own category is the filter, and it is a refusal the miner counts, not a
// failure it retries.
func TestNonMusicPagesAreFacts(t *testing.T) {
	for _, id := range []string{"80238", "84952", "85576"} { // [Humor], [Teater], [Dans]
		_, err := parseEvents(page(t, id), "https://tickets.nortic.se/ticket/event/"+id)
		if !errors.Is(err, model.ErrNotMusic) {
			t.Fatalf("%s: err = %v, want ErrNotMusic", id, err)
		}
	}
}

func TestGöteborgIsGothenburg(t *testing.T) {
	id, name := townFor("Göteborg")
	if id != "gothenburg-se" || name != "Göteborg" {
		t.Fatalf("townFor = %q / %q", id, name)
	}
	if id, _ := townFor("Malmö"); id != "malmo-se" {
		t.Fatalf("Malmö = %q", id)
	}
	if id, _ := townFor("Kosta"); id != "kosta-se" {
		t.Fatalf("Kosta = %q", id)
	}
}

// TestTownCandidatesAndDetailCache walks the sitemap once and fetches each page once, however many town passes ask.
func TestTownCandidatesAndDetailCache(t *testing.T) {
	var pageHits atomic.Int32
	body := page(t, "83019")
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/sitemap.xml":
			fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`+
				`<sitemap><loc>%s/sitemap-events.xml</loc></sitemap></sitemapindex>`, server.URL)
		case r.URL.Path == "/sitemap-events.xml":
			fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`+
				`<url><loc>%s/ticket/event/83019</loc></url><url><loc>%s/ticket/event/99999</loc></url></urlset>`,
				server.URL, server.URL)
		case r.URL.Path == "/ticket/event/83019":
			pageHits.Add(1)
			w.Write(body)
		case r.URL.Path == "/ticket/event/99999":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source := newSource().WithBase(server.URL)
	ctx := context.Background()

	first, err := source.TownCandidates(ctx, "malmo")
	if err != nil {
		t.Fatalf("candidates: %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("got %d candidates, want 2", len(first))
	}
	// A second town pass is the same list, and the sitemap is not re-read.
	second, err := source.TownCandidates(ctx, "stockholm")
	if err != nil || len(second) != 2 {
		t.Fatalf("second pass: %d candidates, err %v", len(second), err)
	}

	shows, err := source.Details(ctx, first[0])
	if err != nil || len(shows) != 1 {
		t.Fatalf("details: %d shows, err %v", len(shows), err)
	}
	// The same page, asked again (the next town's pass), must come from the run cache.
	if _, err := source.Details(ctx, first[0]); err != nil {
		t.Fatalf("cached details: %v", err)
	}
	if hits := pageHits.Load(); hits != 1 {
		t.Fatalf("the page was fetched %d times, want 1", hits)
	}
	if got := shows[0].Start.UTC().Format(time.RFC3339); got != "2026-11-21T19:00:00Z" {
		t.Fatalf("start in UTC = %s, want 19:00Z (20:00 CET)", got)
	}
}

func TestUnknownTownIsRefused(t *testing.T) {
	_, err := newSource().TownCandidates(context.Background(), "berlin")
	if err == nil || !strings.Contains(err.Error(), "unknown town") {
		t.Fatalf("err = %v", err)
	}
}

// TestNotMusicIsCachedAndCountedOnce: three town passes walk the same page. A page whose category says "not music" is
// an answer and must cost one fetch; the refusal is reported to the first pass only, so the run's `not_music` counts
// pages rather than pages × towns.
func TestNotMusicIsCachedAndCountedOnce(t *testing.T) {
	var hits atomic.Int32
	body := page(t, "80238") // [Humor]
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ticket/event/80238" {
			hits.Add(1)
			w.Write(body)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	source := newSource().WithBase(server.URL)
	candidate := model.Candidate{URL: server.URL + "/ticket/event/80238", Source: ID}
	if _, err := source.Details(context.Background(), candidate); !errors.Is(err, model.ErrNotMusic) {
		t.Fatalf("first pass: err = %v, want ErrNotMusic", err)
	}
	for pass := 1; pass < 3; pass++ {
		shows, err := source.Details(context.Background(), candidate)
		if err != nil || shows != nil {
			t.Fatalf("pass %d: (%v, %v), want a silent cached refusal", pass, shows, err)
		}
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("the page was fetched %d times, want 1", got)
	}
}

// TestGonePageIsAFact: a sitemap keeps a URL after the event is removed, and the page answers with the organizer's
// listing. That is not a parse failure and must never be queued for retry.
func TestGonePageIsAFact(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "pages", "gone.html"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = parseEvents(body, "https://tickets.nortic.se/ticket/event/78806")
	if !errors.Is(err, model.ErrGone) {
		t.Fatalf("err = %v, want ErrGone", err)
	}
}
