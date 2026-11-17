// Package model holds the two shapes the product is built on: the full-fidelity show the miner
// keeps, and the nine-key row that is published.
//
// The boundary between them is structural, not procedural. `Show` carries price, description,
// images, the line-up and seat data; `Row` has no field that could hold any of them, and `Project`
// is the only path from one to the other. There is no filter to forget, so `lineup verify` is a
// schema check rather than a field scan. Contract: docs/decisions/2026-11-17-001 §2 (the ADR calls
// it `index.Row`; it lives here as `model.Row` — one package, two files).
package model

import (
	"encoding/json"
	"fmt"
	"time"
)

// Status is the one-letter show state the board renders. Cheap on the wire, and a cancelled show is
// the one status a fan must see.
type Status string

const (
	OnSale    Status = "o"
	Cancelled Status = "c"
	Postponed Status = "x"
	SoldOut   Status = "s"
)

// Valid reports whether the status is one of the four the index format defines.
func (s Status) Valid() bool {
	switch s {
	case OnSale, Cancelled, Postponed, SoldOut:
		return true
	}
	return false
}

// Geo is a venue position. It marshals as [lat, lon] so a row costs sixteen bytes rather than a
// nested object: the board shows kilometres, so a row without coordinates prints no distance
// instead of a wrong one.
type Geo struct {
	Lat float64
	Lon float64
}

func (g Geo) MarshalJSON() ([]byte, error) {
	return json.Marshal([2]float64{g.Lat, g.Lon})
}

func (g *Geo) UnmarshalJSON(data []byte) error {
	// A Go array accepts a *shorter* JSON array and zero-fills the rest, so `[55.67]` would silently
	// become (55.67, 0) — a position in the Gulf of Guinea and a wrong distance. The length is checked
	// explicitly for that reason.
	var pair []float64
	if err := json.Unmarshal(data, &pair); err != nil {
		return err
	}
	if len(pair) != 2 {
		return fmt.Errorf("model: position must be [lat, lon], got %d value(s)", len(pair))
	}
	g.Lat, g.Lon = pair[0], pair[1]
	return nil
}

// Price is what a ticket costs, in the seller's currency.
//
// It never reaches a published row: a price is the seller's content, and the index only ever points at the
// seller. The amount is a float because that is how sellers state it ("55.55 GBP"); it is never used for
// arithmetic and never compared, so there is no rounding decision to get wrong here.
type Price struct {
	Min      float64
	Max      float64
	Currency string
}

// Window is the published date range. It is date-only: a rolling 120-day window is a range of days,
// not of instants, and every selection path takes the clock as a parameter.
type Window struct {
	From time.Time
	To   time.Time
	Days int
}

const dateLayout = "2006-01-02"

// MarshalJSON renders the window as the manifest carries it: {"from":"2026-11-17","to":"2027-03-17",
// "days":120}.
func (w Window) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		From string `json:"from"`
		To   string `json:"to"`
		Days int    `json:"days"`
	}{w.From.Format(dateLayout), w.To.Format(dateLayout), w.Days})
}

func (w *Window) UnmarshalJSON(data []byte) error {
	var raw struct {
		From string `json:"from"`
		To   string `json:"to"`
		Days int    `json:"days"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	from, err := time.Parse(dateLayout, raw.From)
	if err != nil {
		return err
	}
	to, err := time.Parse(dateLayout, raw.To)
	if err != nil {
		return err
	}
	w.From, w.To, w.Days = from, to, raw.Days
	return nil
}

// Show is one performance as the miner knows it. Everything here is kept on the mining machine and
// nothing but the projection is published.
type Show struct {
	// Identity. The keys are filled by the identity layer (key.go, loose.go); the published row's
	// `k` is built from them plus the date.
	Artist    string
	ArtistKey string
	Venue     string
	VenueKey  string
	Town      string // town id, <slug>-<cc>
	TownName  string // the town as the source spells it; the id is derived from it

	// VenueAliases are the other spellings of the same room that the merge folded in. Kept on the mining
	// machine only: a published row carries one venue name.
	VenueAliases []string

	// What is published, in the row's terms.
	Start   time.Time // carries the venue's offset
	Status  Status
	URL     string // the deep link to the seller
	Sources []string
	Geo     *Geo

	// Full fidelity only. ADR-001 §2 forbids every one of these in a published row; the projection
	// test asserts it.
	Doors       time.Time
	Price       *Price
	Description string
	Image       string
	LineUp      []string
	Seats       []string
}
