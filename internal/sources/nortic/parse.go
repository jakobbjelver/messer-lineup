package nortic

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/model"
	"github.com/jakobbjelver/messer-lineup/internal/sources/ldjson"
)

// location is the venue's time zone. Nortic publishes a naive local timestamp (`2026-11-21T18:00`, no offset), so the
// zone is a property of the market, not of the page — and every market this adapter reads is Sweden.
var location = mustLoad("Europe/Stockholm")

func mustLoad(name string) *time.Location {
	zone, err := time.LoadLocation(name)
	if err != nil {
		// A missing tz database is a broken build, not a runtime condition: refuse to guess an offset.
		panic("nortic: no time zone " + name + ": " + err.Error())
	}
	return zone
}

// The two categories that are gigs. Everything else the platform sells — theatre, comedy, dance, revue, sport, a
// brunch cruise — is not a concert, whatever room it is in. `[Festival]` is a gig in this product's model: one row
// whose artist is the festival's own name.
var musicCategories = map[string]bool{"konsert": true, "festival": true}

// categoryRe reads the platform's own prefix: the description always begins `[Konsert] …`.
var categoryRe = regexp.MustCompile(`^\s*\[([^\]]+)\]`)

// stageRe finds the set time inside the description: "[Konsert] Doors 6 PM On stage 8 PM 18+".
var stageRe = regexp.MustCompile(`(?i)on stage\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)

// townAliases are the spellings that must agree with another source's town id. `Göteborg` is the one that matters:
// the alias table is what keeps Nortic's rows in the same shard as Tickster's.
var townAliases = map[string]string{
	"malmö": "malmo", "malmo": "malmo", "malmoe": "malmo",
	"stockholm": "stockholm",
	"göteborg":  "gothenburg", "goteborg": "gothenburg", "gothenburg": "gothenburg",
}

// showFrom projects one schema.org Event into the miner's full-fidelity Show.
func showFrom(object map[string]any, pageURL string) (model.Show, error) {
	description := ldjson.String(object, "description")
	if !musicCategories[categoryOf(description)] {
		return model.Show{}, model.ErrNotMusic
	}

	place := ldjson.Object(object, "location")
	address := ldjson.Object(place, "address")
	locality := ldjson.String(address, "addressLocality")
	venue := cleanVenue(ldjson.String(place, "name"), locality)
	artist := cleanArtist(ldjson.String(object, "name"), venue)
	if artist == "" || venue == "" || locality == "" {
		return model.Show{}, fmt.Errorf("nortic: event is missing an artist, a venue or a town")
	}
	if model.Key(artist) == "" || model.Key(venue) == "" {
		return model.Show{}, fmt.Errorf("nortic: event has no identity keys")
	}

	start, err := parseStart(ldjson.String(object, "startDate"))
	if err != nil {
		return model.Show{}, err
	}
	// The page's start is often the doors time. When the description states a set time, that is the time a reader
	// puts in a calendar.
	if stage, ok := stageTime(description, start); ok {
		start = stage
	}

	townID, townName := townFor(locality)
	if townID == "" {
		return model.Show{}, fmt.Errorf("nortic: cannot derive a town id from %q", locality)
	}

	return model.Show{
		Artist:    artist,
		ArtistKey: model.Key(artist),
		Venue:     venue,
		VenueKey:  model.Key(venue),
		Town:      townID,
		TownName:  townName,
		Start:     start,
		Status:    statusOf(object),
		URL:       pageURL,
		Sources:   []string{ID},
		Geo:       geoOf(place),
	}, nil
}

// categoryOf returns the lower-cased prefix, or "" when the description has none.
func categoryOf(description string) string {
	match := categoryRe.FindStringSubmatch(description)
	if match == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(match[1]))
}

// parseStart reads the platform's naive local timestamp. A date without a time is refused rather than defaulted: an
// invented 19:00 is a claim the page did not make (and the lesson from Visit Stockholm is that it is a visible one).
func parseStart(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if parsed, err := time.ParseInLocation(layout, raw, location); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("nortic: start date %q is not a local date and time", raw)
}

// stageTime reads "On stage 8 PM" and returns the same day at that time, when it is later than the stated start.
// Without a meridiem the hour is read as 24-hour, which is how a Swedish page writes it when it writes one at all.
func stageTime(description string, start time.Time) (time.Time, bool) {
	match := stageRe.FindStringSubmatch(description)
	if match == nil {
		return time.Time{}, false
	}
	hour := atoi(match[1])
	minute := atoi(match[2])
	switch strings.ToLower(match[3]) {
	case "pm":
		if hour < 12 {
			hour += 12
		}
	case "am":
		if hour == 12 {
			hour = 0
		}
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return time.Time{}, false
	}
	staged := time.Date(start.Year(), start.Month(), start.Day(), hour, minute, 0, 0, start.Location())
	if !staged.After(start) {
		return time.Time{}, false
	}
	return staged, true
}

func atoi(value string) int {
	if value == "" {
		return 0
	}
	n := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// cleanArtist removes the shapes Nortic puts around a name: a bill separator (` // `), a promo tail after a pipe, a
// trailing asterisk, and the platform's habit of appending the room. The shared cleaner then does the date and
// pipe-venue work every source needs.
func cleanArtist(raw, venue string) string {
	name := strings.TrimSpace(raw)
	if at := strings.Index(name, " // "); at > 0 {
		name = name[:at]
	}
	if at := strings.Index(name, " | "); at > 0 {
		tail := strings.ToLower(name[at:])
		if strings.Contains(tail, "live at") || strings.Contains(tail, "moved to") ||
			strings.Contains(tail, "presents") || strings.Contains(tail, "inställd") {
			name = name[:at]
		}
	}
	name = strings.TrimRight(strings.TrimSpace(name), "*· ")
	return model.CleanArtistName(name, venue)
}

// cleanVenue drops the platform's ` - <locality>` tail, so `Plan B - Malmö` keys as `Plan B` and merges with the same
// room from another source.
func cleanVenue(raw, locality string) string {
	venue := strings.Join(strings.Fields(raw), " ")
	if locality = strings.Join(strings.Fields(locality), " "); locality != "" {
		for _, separator := range []string{" - ", " – ", " — "} {
			if strings.HasSuffix(venue, separator+locality) {
				venue = strings.TrimSpace(strings.TrimSuffix(venue, separator+locality))
				break
			}
		}
	}
	return strings.Join(strings.Fields(venue), " ")
}

// townFor maps the address's locality to the town id the index publishes. A locality the alias table does not know
// still gets an id (a derived slug), so the run counts it as another town instead of silently dropping the row.
func townFor(locality string) (string, string) {
	name := strings.TrimSpace(locality)
	key := strings.ToLower(name)
	if slug, ok := townAliases[key]; ok {
		return slug + "-se", name
	}
	if slug := model.Key(name); slug != "" {
		return slug + "-se", name
	}
	return "", name
}

// statusOf reads both of the platform's status signals: the schema.org event status and the ticket availability.
func statusOf(object map[string]any) model.Status {
	status := model.OnSale
	switch {
	case strings.Contains(ldjson.String(object, "eventStatus"), "Cancelled"):
		status = model.Cancelled
	case strings.Contains(ldjson.String(object, "eventStatus"), "Postponed"):
		status = model.Postponed
	}
	if strings.Contains(ldjson.String(ldjson.Object(object, "offers"), "availability"), "SoldOut") {
		status = model.SoldOut
	}
	return status
}

// geoOf reads the venue position when the page carries one. A missing position is a bonus, not an error: the row
// simply prints no distance.
func geoOf(place map[string]any) *model.Geo {
	geo := ldjson.Object(place, "geo")
	if geo == nil {
		return nil
	}
	lat, latOK := number(geo["latitude"])
	lon, lonOK := number(geo["longitude"])
	if !latOK || !lonOK || (lat == 0 && lon == 0) || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return nil
	}
	return &model.Geo{Lat: lat, Lon: lon}
}

func number(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case string:
		var parsed float64
		if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%f", &parsed); err == nil {
			return parsed, true
		}
	}
	return 0, false
}
