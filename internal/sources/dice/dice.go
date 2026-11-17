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
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/fetch"
	"github.com/jakobbjelver/messer-lineup/internal/model"
)

// Source name, as it appears in a row's `src` and in the coverage line.
const Name = "dice"

// DefaultSitemap is the index DICE publishes in its own robots.txt.
const DefaultSitemap = "https://dice.fm/sitemaps/sitemap.xml"

// Source fetches DICE. It is a reader: it never writes, and it holds no state beyond its client.
type Source struct {
	client *fetch.Client
}

// New returns a DICE source over an existing client, so the rate limit, the robots cache and the browser
// profile are the miner's, not a second set of rules.
func New(client *fetch.Client) *Source { return &Source{client: client} }

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

// event is the subset of schema.org/MusicEvent this adapter reads.
type event struct {
	Type        string `json:"@type"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	StartDate   string `json:"startDate"`
	DoorTime    string `json:"doorTime"`
	EventStatus string `json:"eventStatus"`
	Description string `json:"description"`
	Image       any    `json:"image"`
	Location    struct {
		Name    string `json:"name"`
		URL     string `json:"url"`
		Address struct {
			Country  string `json:"addressCountry"`
			Locality string `json:"addressLocality"`
			Street   string `json:"streetAddress"`
		} `json:"address"`
		Geo struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"geo"`
	} `json:"location"`
	Offers struct {
		LowPrice      json.Number `json:"lowPrice"`
		HighPrice     json.Number `json:"highPrice"`
		PriceCurrency string      `json:"priceCurrency"`
		Availability  string      `json:"availability"`
	} `json:"offers"`
	Performer []struct {
		Name string `json:"name"`
	} `json:"performer"`
}

// toShow maps one JSON-LD event onto the domain, filling the identity keys here because this is the point
// where the names are known and no venue canonicalisation has happened yet.
func toShow(e event, pageURL string) (model.Show, error) {
	start, err := parseTime(e.StartDate)
	if err != nil {
		return model.Show{}, fmt.Errorf("no usable startDate (%q)", e.StartDate)
	}
	venue := strings.TrimSpace(e.Location.Name)
	town := strings.TrimSpace(e.Location.Address.Locality)
	country := strings.ToLower(strings.TrimSpace(e.Location.Address.Country))
	if venue == "" {
		return model.Show{}, fmt.Errorf("no venue name")
	}
	if town == "" || len(country) != 2 {
		return model.Show{}, fmt.Errorf("no town address (%q, %q)", town, country)
	}

	show := model.Show{
		Artist:  strings.TrimSpace(e.Name),
		Venue:   venue,
		Town:    model.Key(town) + "-" + country,
		Start:   start,
		Status:  status(e.EventStatus, e.Offers.Availability),
		URL:     firstNonEmpty(e.URL, pageURL),
		Sources: []string{Name},

		Description: e.Description,
		Image:       firstImage(e.Image),
		LineUp:      performerNames(e),
	}
	show.ArtistKey = model.Key(show.Artist)
	show.VenueKey = model.Key(venue)
	if e.Location.Geo.Latitude != 0 || e.Location.Geo.Longitude != 0 {
		show.Geo = &model.Geo{Lat: e.Location.Geo.Latitude, Lon: e.Location.Geo.Longitude}
	}
	if e.DoorTime != "" {
		if doors, err := parseTime(e.DoorTime); err == nil {
			show.Doors = doors
		}
	}
	// Offers are strings on DICE ("55.55"), so they are parsed as amounts rather than integers.
	if low, err := e.Offers.LowPrice.Float64(); err == nil && low > 0 {
		price := model.Price{Min: low, Max: low, Currency: e.Offers.PriceCurrency}
		if high, err := e.Offers.HighPrice.Float64(); err == nil && high > low {
			price.Max = high
		}
		show.Price = &price
	}
	return show, nil
}

// status maps schema.org's event status onto the board's one letter. Sold out is an offer fact, not an
// event fact, so it is read from the offer when the event itself is still scheduled.
func status(eventStatus, availability string) model.Status {
	switch {
	case strings.HasSuffix(eventStatus, "EventCancelled"):
		return model.Cancelled
	case strings.HasSuffix(eventStatus, "EventPostponed"):
		return model.Postponed
	case strings.HasSuffix(availability, "SoldOut"):
		return model.SoldOut
	default:
		return model.OnSale
	}
}

func performerNames(e event) []string {
	out := make([]string, 0, len(e.Performer))
	for _, p := range e.Performer {
		if name := strings.TrimSpace(p.Name); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func firstImage(v any) string {
	switch img := v.(type) {
	case string:
		return img
	case []any:
		for _, item := range img {
			if s, ok := item.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// parseTime accepts the offsets DICE publishes (it uses "+00:00", which RFC 3339 reads) and refuses
// anything it cannot place.
func parseTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty")
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z0700", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable %q", s)
}

// findEvent locates the schema.org event inside a page, whichever of the several shapes a publisher
// uses: a single object, a list, or a list of lists.
func findEvent(body []byte) (event, error) {
	blocks, err := jsonLDBlocks(body)
	if err != nil {
		return event{}, err
	}
	for _, block := range blocks {
		var single event
		if err := json.Unmarshal(block, &single); err == nil && isEventType(single.Type) {
			return single, nil
		}
		var list []event
		if err := json.Unmarshal(block, &list); err == nil {
			for _, e := range list {
				if isEventType(e.Type) {
					return e, nil
				}
			}
		}
	}
	return event{}, fmt.Errorf("no schema.org MusicEvent block on the page")
}

func isEventType(t string) bool {
	return t == "MusicEvent" || t == "Event" || t == "Festival" || t == "TheaterEvent"
}
