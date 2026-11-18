package index

import (
	"sort"
	"strings"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/model"
)

// TownSet is how towns.json is produced: a town is **discovered**, never declared.
//
// The registry carries countries, not towns, and §5.7 replaced a hand-written town list with towns derived
// from what a platform actually publishes. So a town appears here the first time a mined show belongs to one,
// and its position is the mean of the venues seen in it — a centroid of the evidence, not a gazetteer lookup.
// That matters for the board's "near me" ordering, which only needs a point inside the town.
type TownSet struct {
	towns map[string]*townAccumulator
}

type townAccumulator struct {
	name    string
	country string
	lat     float64
	lon     float64
	points  int
}

// NewTownSet returns an empty set.
func NewTownSet() *TownSet { return &TownSet{towns: map[string]*townAccumulator{}} }

// Add records one show's town. A show with no town id is ignored rather than guessed at: an event nobody can
// place is not a reason to invent a place.
func (s *TownSet) Add(show model.Show) {
	id := strings.TrimSpace(show.Town)
	if id == "" {
		return
	}
	acc, ok := s.towns[id]
	if !ok {
		acc = &townAccumulator{country: countryOf(id), name: strings.TrimSpace(show.TownName)}
		s.towns[id] = acc
	}
	// The label is the **shortest** one seen, not the first. A platform names a venue's district as often as the
	// city — Tickster lists København, København Ø and København NV under one town id — and the first label wins
	// the sign, which is how a board ends up titled after a postcode. A shorter label is the city the districts
	// belong to, so it is the better name for the whole town.
	if name := strings.TrimSpace(show.TownName); name != "" && (acc.name == "" || len(name) < len(acc.name)) {
		acc.name = name
	}
	if show.Geo != nil {
		acc.lat += show.Geo.Lat
		acc.lon += show.Geo.Lon
		acc.points++
	}
}

// Len is how many towns the run found.
func (s *TownSet) Len() int { return len(s.towns) }

// Towns renders the picker file. Sorted by id so two runs over the same shows produce the same bytes.
//
// `tz` is left empty: a time zone has no trustworthy source in an event page (offsets move with daylight
// saving, and a country can hold several zones). The board renders a show's own offset, so nothing is wrong
// on screen — the picker simply has no zone name to print until a gazetteer is added.
func (s *TownSet) Towns(builtAt time.Time) model.Towns {
	out := model.Towns{Format: model.FormatTowns, BuiltAt: builtAt.UTC(), Towns: make([]model.Town, 0, len(s.towns))}
	for id, acc := range s.towns {
		town := model.Town{ID: id, Name: acc.name, Country: acc.country}
		if town.Name == "" {
			town.Name = id
		}
		if acc.points > 0 {
			town.Lat = acc.lat / float64(acc.points)
			town.Lon = acc.lon / float64(acc.points)
		}
		out.Towns = append(out.Towns, town)
	}
	sort.Slice(out.Towns, func(i, j int) bool { return out.Towns[i].ID < out.Towns[j].ID })
	return out
}

// countryOf reads the ISO code off a `<slug>-<cc>` town id. A malformed id yields "", which the verifier
// reports rather than the writer inventing a country.
func countryOf(townID string) string {
	i := strings.LastIndex(townID, "-")
	if i < 0 || i == len(townID)-1 {
		return ""
	}
	cc := townID[i+1:]
	if len(cc) != 2 {
		return ""
	}
	return strings.ToLower(cc)
}
