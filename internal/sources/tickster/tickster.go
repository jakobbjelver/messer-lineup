// Package tickster reads Tickster, the Danish/Norwegian/Swedish venue platform that is Copenhagen's only
// reachable source.
//
// It publishes no JSON-LD: the structured data is *microdata* on the page (`itemprop="startDate"`,
// `itemprop="location"` with a `PostalAddress`), which is why this adapter parses those attributes instead of
// a JSON block. The town list is a paged city page (`/dk/da/events/in/<city>?skip=N`, 16 events per page), and
// the sitemap carries a date in every event URL — the one platform here where a candidate's date is known
// before the detail fetch.
package tickster

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/jakobbjelver/messer-lineup/internal/fetch"
	"github.com/jakobbjelver/messer-lineup/internal/model"
)

// ID is the source id published in every row's `src`.
const ID = "tickster"

// pageSize is how many events a city page renders; paging walks in these steps.
const pageSize = 16

// maxPages bounds a town walk. It is a runaway guard, not a promise: Stockholm passed 960 events on 2026-11-20
// (the walk was still finding fresh links at 60 pages), so the cap is 120 — 1 920 events, still a bound. Beyond
// that a town needs a real index rather than a deeper page walk, and truncating silently would be a lie told
// by a report — so the walk stops with an error instead.
const maxPages = 120

// cities maps the miner's town slug to the platform's own city path. This table is the one place a platform's
// spelling is translated into a town id, and it is deliberately explicit: the registry (`sources/aliases`) is
// where it belongs once P2 lands, and until then a wrong guess would silently mine the wrong city.
var cities = map[string]string{
	"copenhagen": "/dk/da/events/in/k%C3%B8benhavn",
	"stockholm":  "/se/sv/events/in/stockholm",
	"aarhus":     "/dk/da/events/in/arhus",
	"odense":     "/dk/da/events/in/odense",
	"aalborg":    "/dk/da/events/in/aalborg",
	"malmo":      "/se/sv/events/in/malm%C3%B6",
	"gothenburg": "/se/sv/events/in/g%C3%B6teborg",
	"oslo":       "/no/nb/events/in/oslo",
	"bergen":     "/no/nb/events/in/bergen",
}

// townID canonicalises what a page says into the town id the index publishes. Tickster spells a district
// ("København V", "København Ø") where DICE says a city, and the board shows the venue name anyway, so a
// district resolves to its city: one town id per metro area, or the same city splits into two shards and the
// gate counts half of it.
func townID(city string) (string, bool) {
	key := strings.ToLower(strings.TrimSpace(city))
	if key == "" {
		return "", false
	}
	if id, ok := towns[key]; ok {
		return id, true
	}
	// A district resolves to its city ("København NV" is Copenhagen): the longest known prefix wins, so the
	// same metro area never splits into two shards.
	best, bestLen := "", 0
	for name, id := range towns {
		if len(name) > bestLen && strings.HasPrefix(key, name) {
			best, bestLen = id, len(name)
		}
	}
	if best != "" {
		return best, true
	}
	// Anything else is a town the miner has no opinion about, and a derived id beats a refusal: towns are
	// discovered from what a platform publishes, so the map only holds the spellings that must agree with
	// another source.
	if slug := slugify(city); slug != "" {
		return slug, true
	}
	return "", false
}

// slugify is the deterministic id a town name becomes: lower case, Danish/Norwegian/Swedish letters spelled
// out, everything else folded to a hyphen. It is the same rule for every platform, which is what keeps one
// city on one id when no alias exists.
func slugify(name string) string {
	replacer := strings.NewReplacer(
		"ø", "oe", "å", "aa", "æ", "ae", "ä", "ae", "ö", "oe", "ü", "ue",
		"é", "e", "è", "e", "ê", "e", "á", "a", "ó", "o", "í", "i", "ú", "u", "ñ", "n", "ç", "c",
	)
	lowered := replacer.Replace(strings.ToLower(strings.TrimSpace(name)))
	var b strings.Builder
	lastHyphen := false
	for _, r := range lowered {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		case !lastHyphen && b.Len() > 0:
			b.WriteByte('-')
			lastHyphen = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// towns canonicalises what a page says into the town id the index publishes. DICE spells Copenhagen
// `copenhagen` in its own URLs, so the same city must land on the same id here or the merge never sees the
// duplicate.
var towns = map[string]string{
	"københavn":     "copenhagen",
	"kobenhavn":     "copenhagen",
	"copenhagen":    "copenhagen",
	"frederiksberg": "copenhagen", // its own municipality, one metro area on the board
	"stockholm":     "stockholm",
	"göteborg":      "gothenburg",
	"goteborg":      "gothenburg",
	"gothenburg":    "gothenburg",
	"arhus":         "aarhus",
	"århus":         "aarhus",
	"odense":        "odense",
	"aalborg":       "aalborg",
	"oslo":          "oslo",
	"bergen":        "bergen",
}

// Source is Tickster.
type Source struct {
	client  *fetch.Client
	baseURL string
}

// New returns a reader for the public site.
func New(client *fetch.Client) *Source {
	return &Source{client: client, baseURL: "https://www.tickster.com"}
}

// WithBase points the reader at another origin (a test server, or a mirror).
func (s *Source) WithBase(base string) *Source {
	copied := *s
	copied.baseURL = strings.TrimRight(base, "/")
	return &copied
}

// ID names the source.
func (s *Source) ID() string { return ID }

// Towns lists the town slugs this build can mine.
func Towns() []string {
	out := make([]string, 0, len(cities))
	for slug := range cities {
		out = append(out, slug)
	}
	sort.Strings(out)
	return out
}

// TownCandidates walks a city page until it runs out of events. It returns what the platform lists, not what
// it can parse: the yield is Detail's business, and a run reports both.
func (s *Source) TownCandidates(ctx context.Context, townSlug string) ([]model.Candidate, error) {
	path, ok := cities[strings.ToLower(strings.TrimSpace(townSlug))]
	if !ok {
		return nil, fmt.Errorf("tickster: unknown town %q (known: %s)", townSlug, strings.Join(Towns(), ", "))
	}
	seen := map[string]bool{}
	var out []model.Candidate
	for page := 0; page < maxPages; page++ {
		pageURL := s.baseURL + path
		if page > 0 {
			pageURL = fmt.Sprintf("%s?skip=%d", s.baseURL+path, page*pageSize)
		}
		resp, err := s.client.Get(ctx, pageURL)
		if err != nil {
			if page > 0 && isMissing(err) {
				break
			}
			return nil, fmt.Errorf("tickster: %s: %w", pageURL, err)
		}
		links := eventLinks(resp.Body)
		fresh := 0
		for _, link := range links {
			absolute, err := url.Parse(link)
			if err != nil {
				continue
			}
			full := s.baseURL + absolute.Path
			if seen[full] {
				continue
			}
			seen[full] = true
			fresh++
			out = append(out, model.Candidate{URL: full, Source: ID})
		}
		// A short page is the last page; a page of repeats means the pager ignored the offset.
		if len(links) < pageSize || fresh == 0 {
			return out, nil
		}
	}
	return nil, fmt.Errorf("tickster: %s has more than %d events; a town page walk is not an index", townSlug, maxPages*pageSize)
}

// Detail reads one event page.
func (s *Source) Detail(ctx context.Context, candidate model.Candidate) (model.Show, error) {
	resp, err := s.client.Get(ctx, candidate.URL)
	if err != nil {
		return model.Show{}, fmt.Errorf("tickster: %s: %w", candidate.URL, err)
	}
	return parseEvent(resp.Body, candidate.URL)
}

func isMissing(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "404") || strings.Contains(msg, "410")
}
