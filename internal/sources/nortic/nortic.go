// Package nortic reads Nortic, the Swedish ticketing platform behind a large share of the country's clubs and
// concert halls — including rooms the index had no rows for at all (Plan B, Malmö Live, Palladium).
//
// The shape that matters: the marketing site's town pages are client-rendered and ignore `?page`, but the ticketing
// host publishes its own sitemap (`sitemap-events.xml`) and every `/ticket/event/<id>` page carries a full schema.org
// `Event`:
//
//	{"@type":"Event","name":"…","startDate":"2026-11-21T18:00",
//	 "location":{"name":"Plan B - Malmö","address":{"addressLocality":"Malmö", …}},
//	 "offers":{"availability":"https://schema.org/InStock"},
//	 "description":"[Konsert]  Doors 6 PM On stage 8 PM"}
//
// Three consequences:
//
//   - **The candidate URL carries no town**, so every page costs a detail fetch — the same economics as DICE. The
//     adapter caches the parsed page for the length of one run, which is what makes three town passes cost one fetch
//     per URL rather than three.
//   - **The description begins with the platform's own category**, which is a cleaner music filter than anything
//     Tickster publishes: `[Konsert]` and `[Festival]` are kept, `[Teater]`, `[Humor]`, `[Dans]`, `[Revy]`, `[Show]`,
//     `[Sport]` and the rest are `model.ErrNotMusic`.
//   - **One page can hold several dates** (a tour stop with two nights, a festival with five slots, a dance night on
//     four dates). The adapter implements `mine.Expander`, so the loop keeps every date instead of one.
package nortic

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/jakobbjelver/messer-lineup/internal/fetch"
	"github.com/jakobbjelver/messer-lineup/internal/model"
	"github.com/jakobbjelver/messer-lineup/internal/sources/ldjson"
)

// ID is the source id published in every row's `src`.
const ID = "nortic"

// base is the ticketing host. The marketing site (`nortic.se`) links here; the sitemap is this host's own statement.
const base = "https://tickets.nortic.se"

// eventPath matches an event page by path. The host is not pinned: the sitemap is the host's own statement, and a
// test server (or a mirror) must be able to serve the same shape.
var eventPath = regexp.MustCompile(`/ticket/event/\d+$`)

// Source is Nortic.
type Source struct {
	client     *fetch.Client
	sitemapURL string

	mu    sync.Mutex
	urls  []string
	pages map[string]pageResult
}

// pageResult is one page's outcome for the length of a run. A *parse* outcome is cached — including "this page is not
// music" and "this page has no Event" — because it is deterministic for the run; a transport failure is not cached, so
// the next town's pass gets to retry it.
type pageResult struct {
	shows []model.Show
	err   error
}

// New returns a reader for the public site.
func New(client *fetch.Client) *Source {
	return &Source{client: client, sitemapURL: base + "/sitemap.xml", pages: map[string]pageResult{}}
}

// WithBase points the reader at another origin (a test server, or a mirror).
func (s *Source) WithBase(origin string) *Source {
	copied := *s
	copied.sitemapURL = strings.TrimRight(origin, "/") + "/sitemap.xml"
	copied.pages = map[string]pageResult{}
	return &copied
}

// ID names the source.
func (s *Source) ID() string { return ID }

// Towns lists the town slugs this build can mine. Nortic's inventory is Swedish; a show's town comes from its own
// address, not from the request, so the slug only decides which rows the run keeps.
func Towns() []string { return []string{"stockholm", "gothenburg", "malmo"} }

// Towns implements mine.TownScoped, so a run over four towns skips the Copenhagen pair instead of reporting an error
// for a source that never claimed it.
func (s *Source) Towns() []string { return Towns() }

// TownCandidates returns the sitemap for any town. It is deliberately the same list for every slug: the platform
// publishes no per-town index, and a page costs one fetch no matter which town asked for it (the parsed page is
// cached for the run). The loop keeps the rows whose own town matches the slug.
func (s *Source) TownCandidates(ctx context.Context, townSlug string) ([]model.Candidate, error) {
	slug := strings.ToLower(strings.TrimSpace(townSlug))
	known := false
	for _, candidate := range Towns() {
		if candidate == slug {
			known = true
			break
		}
	}
	if !known {
		return nil, fmt.Errorf("nortic: unknown town %q (known: %s)", townSlug, strings.Join(Towns(), ", "))
	}
	urls, err := s.eventURLs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]model.Candidate, 0, len(urls))
	for _, url := range urls {
		out = append(out, model.Candidate{URL: url, Source: ID})
	}
	return out, nil
}

// Detail reads one page and returns its first show. `mine` prefers `Details` (see the Expander interface); this is
// here so the adapter satisfies the plain interface too.
func (s *Source) Detail(ctx context.Context, candidate model.Candidate) (model.Show, error) {
	shows, err := s.Details(ctx, candidate)
	if err != nil {
		return model.Show{}, err
	}
	if len(shows) == 0 {
		return model.Show{}, fmt.Errorf("nortic: %s listed no event", candidate.URL)
	}
	return shows[0], nil
}

// Details reads one page and returns every dated show on it.
//
// The cache is per run and keyed by URL, including the empty result: three town passes walk the same candidate list,
// and only the first should touch the network. A failure is not cached — the next town's pass gets to retry it.
func (s *Source) Details(ctx context.Context, candidate model.Candidate) ([]model.Show, error) {
	s.mu.Lock()
	if cached, ok := s.pages[candidate.URL]; ok {
		s.mu.Unlock()
		return cached.shows, cached.err
	}
	s.mu.Unlock()

	resp, err := s.client.Get(ctx, candidate.URL)
	if err != nil {
		// A transport failure is not a fact about the page: it is not cached, so a later pass can retry it.
		return nil, fmt.Errorf("nortic: %s: %w", candidate.URL, err)
	}
	shows, err := parseEvents(resp.Body, candidate.URL)
	s.mu.Lock()
	s.pages[candidate.URL] = pageResult{shows: shows, err: err}
	s.mu.Unlock()
	return shows, err
}

// eventURLs reads the sitemap index, then every child sitemap, and returns the event pages in a stable order.
func (s *Source) eventURLs(ctx context.Context) ([]string, error) {
	s.mu.Lock()
	if s.urls != nil {
		cached := s.urls
		s.mu.Unlock()
		return cached, nil
	}
	s.mu.Unlock()

	index, err := s.sitemap(ctx, s.sitemapURL)
	if err != nil {
		return nil, err
	}
	// A sitemap index names children; a plain urlset names pages. Both are answers, and a build that only handled
	// one would report "no candidates" on a platform that changed the wrapper.
	children := index.Sitemaps
	var urls []string
	if len(children) > 0 {
		for _, child := range children {
			if !strings.HasSuffix(child.Loc, ".xml") {
				continue
			}
			doc, err := s.sitemap(ctx, child.Loc)
			if err != nil {
				return nil, err
			}
			for _, entry := range doc.URLs {
				if eventPath.MatchString(entry.Loc) {
					urls = append(urls, entry.Loc)
				}
			}
		}
	} else {
		for _, entry := range index.URLs {
			if eventPath.MatchString(entry.Loc) {
				urls = append(urls, entry.Loc)
			}
		}
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("nortic: %s names no event pages", s.sitemapURL)
	}
	sort.Strings(urls)
	urls = dedupe(urls)

	s.mu.Lock()
	s.urls = urls
	s.mu.Unlock()
	return urls, nil
}

type sitemapDoc struct {
	Sitemaps []struct {
		Loc string `xml:"loc"`
	} `xml:"sitemap"`
	URLs []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
}

func (s *Source) sitemap(ctx context.Context, url string) (sitemapDoc, error) {
	resp, err := s.client.Get(ctx, url)
	if err != nil {
		return sitemapDoc{}, fmt.Errorf("nortic: %s: %w", url, err)
	}
	var doc sitemapDoc
	if err := xml.Unmarshal(resp.Body, &doc); err != nil {
		return sitemapDoc{}, fmt.Errorf("nortic: %s is not a sitemap: %w", url, err)
	}
	return doc, nil
}

func dedupe(values []string) []string {
	out := values[:0]
	last := ""
	for _, value := range values {
		if value == last {
			continue
		}
		out = append(out, value)
		last = value
	}
	return out
}

// parseEvents turns one event page into shows. The category is uniform per page, so a page whose events are not
// music is one `ErrNotMusic`, and the miner counts it as a fact rather than a failure.
func parseEvents(body []byte, pageURL string) ([]model.Show, error) {
	blocks, err := ldjson.Blocks(body)
	if err != nil {
		return nil, fmt.Errorf("nortic: %s: %w", pageURL, err)
	}
	var objects []map[string]any
	for _, block := range blocks {
		var document any
		if err := json.Unmarshal(block, &document); err != nil {
			// One unreadable block is not a dead page: another may hold the event.
			continue
		}
		ldjson.Walk(document, func(object map[string]any) {
			if ldjson.HasType(object, "Event") {
				objects = append(objects, object)
			}
		})
	}
	if len(objects) == 0 {
		return nil, fmt.Errorf("nortic: %s carries no Event", pageURL)
	}

	var shows []model.Show
	refused := 0
	seen := map[string]bool{}
	for _, object := range objects {
		show, err := showFrom(object, pageURL)
		if errors.Is(err, model.ErrNotMusic) {
			refused++
			continue
		}
		if err != nil {
			// A malformed event is skipped; if every event is malformed the page fails below, so a parser change is
			// still loud.
			refused++
			continue
		}
		// A festival's several slots on one day are one row: the row key is artist|venue|day, and three rows with the
		// same key would be three duplicates the merge has to undo.
		key := show.ArtistKey + "|" + show.VenueKey + "|" + show.Start.Format("2006-01-02")
		if seen[key] {
			continue
		}
		seen[key] = true
		shows = append(shows, show)
	}
	if len(shows) == 0 {
		if refused > 0 {
			return nil, model.ErrNotMusic
		}
		return nil, fmt.Errorf("nortic: %s listed no readable event", pageURL)
	}
	return shows, nil
}
