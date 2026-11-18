package tickster

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/model"
)

// Tickster renders its event data as schema.org *microdata*, not JSON-LD: an `itemprop="startDate"` with the
// machine-readable value in a content attribute, and a `location` block holding the venue name and an
// `addressLocality`. That is a published, intentional contract on the page, unlike the visible Danish text —
// which is why this parser reads the attributes and only falls back to text for the cancellation label.
var (
	eventLinkPattern = regexp.MustCompile(`href="(/[a-z]{2}/[a-z]{2}/events/[0-9a-z]+/\d{4}-\d{2}-\d{2}/[^"]*)"`)
	titlePattern     = regexp.MustCompile(`<h1[^>]*itemprop="name"[^>]*>(.*?)</h1>`)
	startPattern     = regexp.MustCompile(`itemprop="startDate"[^>]*content="([^"]+)"`)
	cityPattern      = regexp.MustCompile(`itemprop="addressLocality"[^>]*>(.*?)<`)
	venuePattern     = regexp.MustCompile(`itemtype="http://schema.org/Place"[^>]*itemprop="location"[\s\S]{0,400}?<span itemprop="name"[^>]*>(.*?)</span>`)
	tagPattern       = regexp.MustCompile(`<[^>]+>`)
	spacePattern     = regexp.MustCompile(`\s+`)
)

// cancelledLabels are the words a page uses to say the show will not happen. The label is the only datum on
// the page that is not in a schema.org attribute, and a cancelled show is the one status a fan must see.
var cancelledLabels = []string{"Aflyst", "Inställt", "Avlyst"}

// eventLinks lists the event candidate URLs on a city page.
func eventLinks(body []byte) []string {
	matches := eventLinkPattern.FindAllSubmatch(body, -1)
	out := make([]string, 0, len(matches))
	seen := map[string]bool{}
	for _, m := range matches {
		link := string(m[1])
		if seen[link] {
			continue
		}
		seen[link] = true
		out = append(out, link)
	}
	return out
}

// parseEvent turns one event page into a show.
func parseEvent(body []byte, pageURL string) (model.Show, error) {
	page := string(body)

	title := firstGroup(titlePattern, page)
	if title == "" {
		return model.Show{}, fmt.Errorf("tickster: %s: no event title", pageURL)
	}
	// Tickster names some un-ticketed admission with its own ticket product ("Entrébiljetter/Tickets"). It parses
	// perfectly and is not a show, so the adapter refuses it by name instead of publishing an artist called
	// "Tickets" — see model.NotAnArtistTitle.
	if model.NotAnArtistTitle(text(title)) {
		return model.Show{}, model.ErrNotAnArtist
	}
	rawStart := firstGroup(startPattern, page)
	if rawStart == "" {
		return model.Show{}, fmt.Errorf("tickster: %s: no start date", pageURL)
	}

	// The town decides the zone; the page states a naive local time, so it is attached here rather than
	// guessed at from the machine's own zone.
	country, zone := localeOf(pageURL)
	if country == "" {
		return model.Show{}, fmt.Errorf("tickster: %s: not a market URL, so the show cannot be placed", pageURL)
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return model.Show{}, fmt.Errorf("tickster: loading %s: %w", zone, err)
	}
	start, err := parseLocal(rawStart, location)
	if err != nil {
		return model.Show{}, fmt.Errorf("tickster: %s: start date %q: %w", pageURL, rawStart, err)
	}

	city := text(firstGroup(cityPattern, page))
	if city == "" {
		return model.Show{}, fmt.Errorf("tickster: %s: no town address", pageURL)
	}
	venue := text(firstGroup(venuePattern, page))
	if venue == "" {
		return model.Show{}, fmt.Errorf("tickster: %s: no venue name", pageURL)
	}

	slug, ok := townID(city)
	if !ok {
		// A town the miner has no id for is not published under a guess: it would split one city's rows in two.
		return model.Show{}, fmt.Errorf("tickster: %s: no town id for %q", pageURL, city)
	}

	status := model.OnSale
	if cancelled(page) {
		status = model.Cancelled
	}

	return model.Show{
		Artist:    text(title),
		ArtistKey: model.Key(text(title)),
		Venue:     venue,
		VenueKey:  model.Key(venue),
		Town:      slug + "-" + country,
		TownName:  city,
		Start:     start,
		Status:    status,
		URL:       pageURL,
		Sources:   []string{ID},
	}, nil
}

// localeOf reads the market and its zone from the URL: /dk/da/… is Denmark, /se/sv/… is Sweden.
func localeOf(pageURL string) (country, zone string) {
	switch {
	case strings.Contains(pageURL, "//") && strings.Contains(pageURL, "/dk/"):
		return "dk", "Europe/Copenhagen"
	case strings.Contains(pageURL, "/se/"):
		return "se", "Europe/Stockholm"
	case strings.Contains(pageURL, "/no/"):
		return "no", "Europe/Oslo"
	}
	return "", ""
}

// parseLocal reads the naive local timestamp Tickster publishes ("2026-11-27T22:00:00") and attaches a zone.
// An offset, if one ever appears, is honoured rather than overwritten.
func parseLocal(raw string, location *time.Location) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z07:00"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, nil
		}
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, raw, location); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised timestamp")
}

func cancelled(page string) bool {
	head := page
	if len(head) > 8000 {
		head = head[:8000]
	}
	for _, label := range cancelledLabels {
		if strings.Contains(head, label) {
			return true
		}
	}
	return false
}

func firstGroup(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// text strips tags, unescapes entities and collapses whitespace — what a name looks like on screen.
func text(s string) string {
	s = tagPattern.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.TrimSpace(spacePattern.ReplaceAllString(s, " "))
}
