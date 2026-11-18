package server

// Which town a visitor should land on.
//
// The board is static, so it cannot know where a request came from; the edge can. Each entry in `t` is a published
// town and, when the index knows it, that town's own point — the mean of the venue positions its shows publish. With a
// point for the visitor and a point for each town, the answer is the nearest one, because a country is not a distance:
// an address in Malmö is 28 km from Copenhagen and 500 km from Stockholm, and both are `SE`.
//
// Without coordinates — the ordinary case in local development, and any town whose venues never stated a position — the
// country suffix decides, then the first published town. A nearest answer is about one visitor, so it is `private`:
// a shared cache must never hand one visitor's answer to another.

import (
	"math"
	"net/http"
	"strconv"
	"strings"
)

type townEntry struct {
	id       string
	lat, lon float64
	at       bool
}

type point struct {
	lat, lon float64
}

func (s *server) where(w http.ResponseWriter, r *http.Request) {
	entries := parseTownEntries(r.URL.Query()["t"])
	if len(entries) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "t is required: the published towns, as id@lat,lon")
		return
	}

	// Cloudflare puts the visitor's position in `request.cf`, not in a header, so the header pair is this server's
	// seam: it is what the Worker's `cf.latitude` becomes locally, and what makes the branch testable at all.
	country := strings.ToLower(strings.TrimSpace(r.Header.Get("CF-IPCountry")))
	lat, lon, haveHere := parseCoord(r.Header.Get("CF-IPLatitude"), r.Header.Get("CF-IPLongitude"))
	here := point{lat, lon}

	chosen, basis, km := entries[0], "fallback", 0.0
	if haveHere {
		best := math.Inf(1)
		for _, entry := range entries {
			if !entry.at {
				continue
			}
			distance := haversine(here, point{entry.lat, entry.lon})
			if distance < best {
				best, chosen, basis, km = distance, entry, "distance", distance
			}
		}
	}
	if basis == "fallback" && country != "" {
		for _, entry := range entries {
			if strings.HasSuffix(entry.id, "-"+country) {
				chosen, basis = entry, "country"
				break
			}
		}
	}

	if basis == "distance" {
		w.Header().Set("Cache-Control", "private, max-age=86400")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("Vary", "CF-IPCountry")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"town":    chosen.id,
		"country": nullIfEmpty(country),
		"basis":   basis,
		"km":      orNilKm(basis == "distance", km),
	})
}

// nullIfEmpty renders "" as an absent country rather than as an empty string.
func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func orNilKm(measured bool, metres float64) any {
	if !measured {
		return nil
	}
	return math.Round(metres/100) / 10
}

// parseTownEntries reads repeated `?t=id@lat,lon` parameters. (Repeated parameters rather than one separated list:
// Go's ParseQuery rejects an unencoded ';' outright, and a separator the server cannot parse is a trap.) An entry
// without coordinates is still an entry: it can still win on country, and the board can still open it.
func parseTownEntries(raw []string) []townEntry {
	out := make([]townEntry, 0, 4)
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		id, coords, _ := strings.Cut(item, "@")
		if id == "" || len(id) > 64 {
			continue
		}
		entry := townEntry{id: id}
		if lat, lon, ok := strings.Cut(coords, ","); ok {
			entry.lat, entry.lon, entry.at = parseCoord(lat, lon)
		}
		out = append(out, entry)
		if len(out) == 50 {
			break
		}
	}
	return out
}

// parseCoord accepts a pair only if both halves are real and in range. (0, 0) is "unknown", not the Gulf of Guinea:
// the index publishes it for any town whose venues have not stated a position.
func parseCoord(rawLat, rawLon string) (float64, float64, bool) {
	lat, errLat := strconv.ParseFloat(strings.TrimSpace(rawLat), 64)
	lon, errLon := strconv.ParseFloat(strings.TrimSpace(rawLon), 64)
	if errLat != nil || errLon != nil {
		return 0, 0, false
	}
	if math.IsNaN(lat) || math.IsNaN(lon) || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return 0, 0, false
	}
	if lat == 0 && lon == 0 {
		return 0, 0, false
	}
	return lat, lon, true
}

// haversine is the great-circle distance in metres.
func haversine(a, b point) float64 {
	const radius = 6_371_000
	const rad = math.Pi / 180
	dLat := (b.lat - a.lat) * rad
	dLon := (b.lon - a.lon) * rad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(a.lat*rad)*math.Cos(b.lat*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * radius * math.Asin(math.Sqrt(h))
}
