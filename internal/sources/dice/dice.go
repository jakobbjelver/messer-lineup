// Package dice reads DICE's public event pages.
//
// Two facts decide the shape of this adapter, both measured 2026-11-17 and recorded in docs/coverage.md:
// DICE answers this egress (its event pages return 200 while Eventim's and AXS's return 403), and its
// robots.txt allows `*` with `Content-Signal: search=yes,ai-train=no,use=reference` — which is what a
// board that indexes dates and links out to the seller is. Its sitemap carries no date and no town, so a
// candidate is one URL and every one costs a detail fetch.
package dice

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/jakobbjelver/messer-lineup/internal/fetch"
	"github.com/jakobbjelver/messer-lineup/internal/model"
)

// Source name, as it appears in a row's `src` and in the coverage line.
const Name = "dice"

// DefaultSitemap is the index DICE publishes in its own robots.txt.
const DefaultSitemap = "https://dice.fm/sitemaps/sitemap.xml"

// Source fetches DICE. It is a reader: it never writes, and it holds no state beyond its client.
type Source struct {
	client  *fetch.Client
	baseURL string
}

// New returns a DICE source over an existing client, so the rate limit, the robots cache and the browser
// profile are the miner's, not a second set of rules.
// ID names the source: it is what lands in a row's `src` and in the report.
func (s *Source) ID() string { return Name }

func New(client *fetch.Client) *Source { return &Source{client: client, baseURL: "https://dice.fm"} }

// WithBase points the source at another origin, which is how the tests serve fixtures without the network.
func (s *Source) WithBase(base string) *Source {
	s.baseURL = strings.TrimSuffix(base, "/")
	return s
}

// Sitemaps lists the event sitemap shards. Walking the index beats guessing `sitemap{n}.xml` until a 404:
// the index is the publisher's own statement of what exists.
func (s *Source) Sitemaps(ctx context.Context) ([]string, error) {
	resp, err := s.client.Get(ctx, DefaultSitemap)
	if err != nil {
		return nil, fmt.Errorf("dice: sitemap index: %w", err)
	}
	var index struct {
		Sitemaps []struct {
			Loc string `xml:"loc"`
		} `xml:"sitemap"`
	}
	if err := xml.Unmarshal(resp.Body, &index); err != nil {
		return nil, fmt.Errorf("dice: sitemap index: %w", err)
	}
	var out []string
	for _, sm := range index.Sitemaps {
		if loc := strings.TrimSpace(sm.Loc); loc != "" {
			out = append(out, loc)
		}
	}
	sort.Strings(out) // sitemap1…sitemap7, so a run is reproducible
	return out, nil
}

// Candidates reads one sitemap shard. Only /event/<id> paths are candidates: the same file lists about,
// jobs and venues, and a crawler that fetches those is spending the host's patience on nothing.
func (s *Source) Candidates(ctx context.Context, sitemapURL string) ([]model.Candidate, error) {
	resp, err := s.client.Get(ctx, sitemapURL)
	if err != nil {
		return nil, fmt.Errorf("dice: %s: %w", sitemapURL, err)
	}
	var set struct {
		URLs []struct {
			Loc     string `xml:"loc"`
			LastMod string `xml:"lastmod"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal(resp.Body, &set); err != nil {
		return nil, fmt.Errorf("dice: %s: %w", sitemapURL, err)
	}
	out := make([]model.Candidate, 0, len(set.URLs))
	for _, u := range set.URLs {
		loc := strings.TrimSpace(u.Loc)
		if isEventURL(loc) {
			out = append(out, model.Candidate{URL: loc, Source: Name})
		}
	}
	return out, nil
}

// eventHref matches the event links a city browse page server-renders. The page is a React app, but the
// first screen of events is in the HTML, which is what makes a town enumerable without running JavaScript.
var eventHref = regexp.MustCompile(`href="(/event/[0-9a-f]{16,})"`)

// TownCandidates lists the events a city's browse page shows.
//
// This is the platform's own city list — the design's "towns are derived from each platform's city sitemap"
// — and it is the cheap half of a sweep: one request per town instead of one per event in the whole index.
// It sees the page DICE renders, not the whole city, so it is a floor and never a total.
func (s *Source) TownCandidates(ctx context.Context, citySlug string) ([]model.Candidate, error) {
	pageURL := s.baseURL + "/browse/" + url.PathEscape(citySlug)
	resp, err := s.client.Get(ctx, pageURL)
	if err != nil {
		return nil, fmt.Errorf("dice: %s: %w", pageURL, err)
	}
	seen := map[string]bool{}
	out := make([]model.Candidate, 0, 32)
	for _, m := range eventHref.FindAllSubmatch(resp.Body, -1) {
		link := string(m[1])
		if seen[link] {
			continue
		}
		seen[link] = true
		out = append(out, model.Candidate{URL: s.baseURL + link, Source: Name})
	}
	return out, nil
}

func isEventURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	return len(parts) == 2 && parts[0] == "event" && parts[1] != ""
}

// Detail reads one event page and returns the show it describes. Only the JSON-LD block is read: the page
// is a React app whose visible text is not a contract, while `MusicEvent` is.
func (s *Source) Detail(ctx context.Context, candidate model.Candidate) (model.Show, error) {
	resp, err := s.client.Get(ctx, candidate.URL)
	if err != nil {
		return model.Show{}, fmt.Errorf("dice: %s: %w", candidate.URL, err)
	}
	event, err := findEvent(resp.Body)
	if err != nil {
		return model.Show{}, fmt.Errorf("dice: %s: %w", candidate.URL, err)
	}
	return toShow(event, candidate.URL)
}
