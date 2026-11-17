// Package match is the merge: the same gig arriving from two platforms becomes one show.
//
// The join key is the row key (artistKey|venueKey|date), so a merge is decided by the same identity the
// published index uses — not by a fuzzy title comparison. One fallback exists and it is deliberately narrow:
// when two platforms spell the venue differently but agree on the artist, the date and (to within ~200 m) the
// position, the shows are the same performance and both venue names are kept as aliases.
package match

import (
	"sort"
	"strings"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/model"
)

// GeoSlack is how far apart two venue coordinates may be and still describe one room, in degrees. 0.002°
// is roughly 200 m at northern latitudes — wide enough for a venue centroid read off a map, far too narrow
// for two different rooms in the same city.
const GeoSlack = 0.002

// Stats is what the merge did, in the terms G0 grades: a duplicate rate is merged ÷ raw.
type Stats struct {
	Raw           int     `json:"raw"`
	Out           int     `json:"out"`
	Merged        int     `json:"merged"`
	DuplicateRate float64 `json:"duplicate_rate"`
}

// Merge folds shows from several sources into one list. Input order does not decide the output: the result is
// sorted by merge key, so two runs over the same input produce the same list.
func Merge(shows []model.Show) ([]model.Show, Stats) {
	stats := Stats{Raw: len(shows)}
	if len(shows) == 0 {
		return nil, stats
	}

	// Deterministic order in, so "which source wins a tie" is a rule and not a scheduling accident: the
	// source id decides, then the venue name, then the URL.
	ordered := make([]model.Show, len(shows))
	copy(ordered, shows)
	sort.SliceStable(ordered, func(i, j int) bool {
		if a, b := firstSource(ordered[i]), firstSource(ordered[j]); a != b {
			return a < b
		}
		if ordered[i].Venue != ordered[j].Venue {
			return ordered[i].Venue < ordered[j].Venue
		}
		return ordered[i].URL < ordered[j].URL
	})

	// Bucket by artist and date: the exact-key match is the common case, and the geo fallback then only ever
	// compares a handful of shows. Without the bucket a world index is quadratic.
	buckets := map[string][]int{}
	out := make([]model.Show, 0, len(ordered))

	for _, show := range ordered {
		if show.ArtistKey == "" || show.Start.IsZero() {
			continue
		}
		bucket := artistDay(show)
		matchAt := -1
		for _, idx := range buckets[bucket] {
			if sameShow(out[idx], show) {
				matchAt = idx
				break
			}
		}
		if matchAt < 0 {
			out = append(out, show)
			buckets[bucket] = append(buckets[bucket], len(out)-1)
			continue
		}
		out[matchAt] = fold(out[matchAt], show)
		stats.Merged++
	}

	sort.SliceStable(out, func(i, j int) bool {
		ki, kj := rowKey(out[i]), rowKey(out[j])
		if ki != kj {
			return ki < kj
		}
		return out[i].Venue < out[j].Venue
	})
	stats.Out = len(out)
	if stats.Raw > 0 {
		stats.DuplicateRate = float64(stats.Merged) / float64(stats.Raw)
	}
	return out, stats
}

// sameShow is the whole merge decision, in one place.
func sameShow(a, b model.Show) bool {
	if a.ArtistKey != b.ArtistKey || a.Start.Format("2006-01-02") != b.Start.Format("2006-01-02") {
		return false
	}
	if a.VenueKey == b.VenueKey {
		return true
	}
	// Different spellings of one room: only the position can settle it, and both sides must have one.
	if a.Geo == nil || b.Geo == nil {
		return false
	}
	return abs(a.Geo.Lat-b.Geo.Lat) < GeoSlack && abs(a.Geo.Lon-b.Geo.Lon) < GeoSlack
}

// fold keeps the earliest record but takes everything that makes the row better: both sources, a status a fan
// must see, and a position if either side had one.
func fold(into, extra model.Show) model.Show {
	into.Sources = union(into.Sources, extra.Sources)
	if into.Geo == nil && extra.Geo != nil {
		into.Geo = extra.Geo
		into.Venue = extra.Venue
		into.VenueKey = extra.VenueKey
	}
	if statusRank(extra.Status) > statusRank(into.Status) {
		into.Status = extra.Status
	}
	if into.URL == "" {
		into.URL = extra.URL
	}
	if into.Venue != extra.Venue && extra.Venue != "" && !contains(into.VenueAliases, extra.Venue) {
		into.VenueAliases = append(into.VenueAliases, extra.Venue)
	}
	if into.Doors.IsZero() {
		into.Doors = extra.Doors
	}
	if into.Price == nil {
		into.Price = extra.Price
	}
	if into.Image == "" {
		into.Image = extra.Image
	}
	if into.Description == "" {
		into.Description = extra.Description
	}
	if len(into.LineUp) == 0 {
		into.LineUp = extra.LineUp
	}
	if into.Town == "" {
		into.Town, into.TownName = extra.Town, extra.TownName
	}
	return into
}

// statusRank orders the four states by what a fan must not miss: a cancellation outranks everything, and
// "on sale" is what an unannotated show means.
func statusRank(s model.Status) int {
	switch s {
	case model.Cancelled:
		return 3
	case model.Postponed:
		return 2
	case model.SoldOut:
		return 1
	}
	return 0
}

func artistDay(s model.Show) string {
	return s.ArtistKey + "|" + s.Start.Format("2006-01-02")
}

func rowKey(s model.Show) string {
	return s.ArtistKey + "|" + s.VenueKey + "|" + s.Start.Format("2006-01-02")
}

func firstSource(s model.Show) string {
	if len(s.Sources) == 0 {
		return ""
	}
	sorted := append([]string(nil), s.Sources...)
	sort.Strings(sorted)
	return sorted[0]
}

func union(a, b []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(a)+len(b))
	for _, group := range [][]string{a, b} {
		for _, v := range group {
			v = strings.TrimSpace(v)
			if v == "" || seen[v] {
				continue
			}
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// Window is here so a caller can trim before merging without re-implementing the comparison: the same
// date-only window the index uses.
func Window(from, to time.Time) func(model.Show) bool {
	f, t := from.Format("2006-01-02"), to.Format("2006-01-02")
	return func(s model.Show) bool {
		if s.Start.IsZero() {
			return false
		}
		d := s.Start.Format("2006-01-02")
		return d >= f && d <= t
	}
}
