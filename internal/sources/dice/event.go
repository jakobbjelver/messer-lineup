package dice

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/model"
)

// JSON-LD is a shape, not a schema: `@type` can be `MusicEvent` or `Event`, `offers` can be an object or a
// list, `image` a string or a list, `performer` one object or many, and prices arrive as strings as often as
// numbers. Ten percent of DICE's event pages carry a block that a strict struct refuses, so this reader walks
// the document by key and type-switches, and never fails because of a field it did not expect.
type document map[string]any

// findEvent locates the schema.org event inside a page, whichever of the shapes a publisher uses.
func findEvent(body []byte) (document, error) {
	blocks, err := jsonLDBlocks(body)
	if err != nil {
		return nil, err
	}
	for _, block := range blocks {
		for _, doc := range flatten(block) {
			if isEventType(asString(doc["@type"])) || containsEventType(doc["@type"]) {
				return doc, nil
			}
			if graph, ok := doc["@graph"]; ok {
				for _, node := range asDocuments(graph) {
					if isEventType(asString(node["@type"])) || containsEventType(node["@type"]) {
						return node, nil
					}
				}
			}
		}
	}
	return nil, fmt.Errorf("no schema.org event block on the page")
}

// flatten turns one JSON-LD block into a list of documents: an object, a list of objects, or its @graph.
func flatten(block []byte) []document {
	var one document
	if err := json.Unmarshal(block, &one); err == nil && one != nil {
		if graph, ok := one["@graph"]; ok && asString(one["@type"]) == "" {
			return asDocuments(graph)
		}
		return []document{one}
	}
	var list []document
	if err := json.Unmarshal(block, &list); err == nil {
		return list
	}
	return nil
}

func asDocuments(v any) []document {
	switch value := v.(type) {
	case []any:
		out := make([]document, 0, len(value))
		for _, item := range value {
			if doc, ok := item.(map[string]any); ok {
				out = append(out, doc)
			}
		}
		return out
	case map[string]any:
		return []document{value}
	}
	return nil
}

func isEventType(t string) bool {
	switch t {
	case "MusicEvent", "Event", "Festival", "TheaterEvent", "ComedyEvent", "DanceEvent":
		return true
	}
	return false
}

// containsEventType handles the `"@type": ["Event", "MusicEvent"]` form.
func containsEventType(v any) bool {
	if list, ok := v.([]any); ok {
		for _, item := range list {
			if isEventType(asString(item)) {
				return true
			}
		}
	}
	return false
}

// toShow maps one JSON-LD document onto the domain. The identity keys are filled here because this is where
// the names are known and no venue canonicalisation has happened yet.
func toShow(doc document, pageURL string) (model.Show, error) {
	start, err := parseTime(asString(doc["startDate"]))
	if err != nil {
		return model.Show{}, fmt.Errorf("no usable startDate (%q)", asString(doc["startDate"]))
	}
	location := asDocument(doc["location"])
	address := asDocument(location["address"])
	venue := strings.TrimSpace(asString(location["name"]))
	town := strings.TrimSpace(asString(address["addressLocality"]))
	country := strings.ToLower(strings.TrimSpace(asString(address["addressCountry"])))
	if venue == "" {
		return model.Show{}, fmt.Errorf("no venue name")
	}
	if town == "" || len(country) != 2 {
		return model.Show{}, fmt.Errorf("no town address (%q, %q)", town, country)
	}

	show := model.Show{
		Artist:  strings.TrimSpace(asString(doc["name"])),
		Venue:   venue,
		Town:    model.Key(town) + "-" + country,
		Start:   start,
		Status:  status(asString(doc["eventStatus"]), offerAvailability(doc)),
		URL:     firstNonEmpty(asString(doc["url"]), pageURL),
		Sources: []string{Name},

		Description: asString(doc["description"]),
		Image:       firstString(doc["image"]),
		LineUp:      performerNames(doc),
	}
	show.ArtistKey = model.Key(show.Artist)
	show.VenueKey = model.Key(venue)
	if doors, err := parseTime(asString(doc["doorTime"])); err == nil {
		show.Doors = doors
	}
	if geo := asDocument(location["geo"]); geo != nil {
		lat, latOK := asFloat(geo["latitude"])
		lon, lonOK := asFloat(geo["longitude"])
		if latOK && lonOK && (lat != 0 || lon != 0) {
			show.Geo = &model.Geo{Lat: lat, Lon: lon}
		}
	}
	if offer := firstOffer(doc); offer != nil {
		low, lowOK := asFloat(offer["lowPrice"])
		high, highOK := asFloat(offer["highPrice"])
		if lowOK && low > 0 {
			price := model.Price{Min: low, Max: low, Currency: asString(offer["priceCurrency"])}
			if highOK && high > low {
				price.Max = high
			}
			show.Price = &price
		}
	}
	return show, nil
}

// firstOffer returns the offer object, whichever of the two shapes it arrives in.
func firstOffer(doc document) document {
	switch value := doc["offers"].(type) {
	case map[string]any:
		return value
	case []any:
		for _, item := range value {
			if offer, ok := item.(map[string]any); ok {
				return offer
			}
		}
	}
	return nil
}

func offerAvailability(doc document) string {
	if offer := firstOffer(doc); offer != nil {
		return asString(offer["availability"])
	}
	return ""
}

func performerNames(doc document) []string {
	var out []string
	switch value := doc["performer"].(type) {
	case []any:
		for _, item := range value {
			if name := strings.TrimSpace(asString(asDocument(item)["name"])); name != "" {
				out = append(out, name)
			}
		}
	case map[string]any:
		if name := strings.TrimSpace(asString(value["name"])); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// status maps schema.org's event status onto the board's one letter. Sold out is an offer fact, not an event
// fact, so it is read from the offer when the event itself is still scheduled.
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

func asDocument(v any) document {
	if doc, ok := v.(map[string]any); ok {
		return doc
	}
	return nil
}

func asString(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case json.Number:
		return value.String()
	case float64:
		return strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%.2f", value), "0"), ".")
	}
	return ""
}

func asFloat(v any) (float64, bool) {
	switch value := v.(type) {
	case float64:
		return value, true
	case string:
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(value), "%g", &f); err == nil {
			return f, true
		}
	case json.Number:
		if f, err := value.Float64(); err == nil {
			return f, true
		}
	}
	return 0, false
}

func firstString(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case []any:
		for _, item := range value {
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

// parseTime accepts the offsets DICE publishes (it uses "+00:00", which RFC 3339 reads) and refuses anything
// it cannot place.
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
