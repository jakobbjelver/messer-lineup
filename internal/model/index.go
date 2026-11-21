package model

import (
	"errors"
	"fmt"
	"time"
)

// FormatIndex is the manifest's format id. A client that does not recognise it must stop, because
// the shard map's meaning is tied to it.
const FormatIndex = "lineup-index/1"

// FormatTowns is the picker file's format id.
const FormatTowns = "lineup-towns/1"

// Row is the published projection of one show: nine keys, no content fields (ADR-001 §2).
//
// The field order is the wire order and it is part of the contract — a golden test pins one row's
// bytes, because the Go writer and the TypeScript reader are compared on output, not on intent.
// There is no field here that could hold a price, a currency, a description, an image, a line-up or
// a seat map: that is the whole point of the type.
type Row struct {
	K   string   `json:"k"`           // artistKey|venueKey|YYYY-MM-DD, recomputable from the row
	A   string   `json:"a"`           // artist display name
	V   string   `json:"v"`           // venue display name
	C   string   `json:"c"`           // town id, <slug>-<cc>
	D   string   `json:"d"`           // start, RFC 3339 with the venue's offset
	St  Status   `json:"st"`          // o on sale | c cancelled | x postponed | s sold out
	U   string   `json:"u"`           // the deep link to the seller
	Src []string `json:"src"`         // every source that carried the show after the merge
	G   *Geo     `json:"g,omitempty"` // venue position, absent when the source gave none
}

// RowKey is the merge key: artistKey|venueKey|YYYY-MM-DD. It is computed after venue
// canonicalisation, never before, and it is stable for a calendar UID and for "new since last visit".
func RowKey(artistKey, venueKey string, start time.Time) string {
	return artistKey + "|" + venueKey + "|" + start.Format(dateLayout)
}

// Project turns a full-fidelity show into the published row. It is the only path from Show to Row,
// and it reads only the fields the format allows — so a price cannot leak by construction rather than
// by a filter that could be forgotten.
func Project(s Show) (Row, error) {
	if s.ArtistKey == "" || s.VenueKey == "" {
		return Row{}, fmt.Errorf("project: show has no identity keys (artist %q, venue %q)", s.Artist, s.Venue)
	}
	if s.Start.IsZero() {
		return Row{}, errors.New("project: show has no start time")
	}
	if !s.Status.Valid() {
		return Row{}, fmt.Errorf("project: status %q is not one of o, c, x, s", s.Status)
	}
	if s.URL == "" {
		return Row{}, errors.New("project: every published row links out to the seller")
	}
	return Row{
		K:   RowKey(s.ArtistKey, s.VenueKey, s.Start),
		A:   s.Artist,
		V:   s.Venue,
		C:   s.Town,
		D:   s.Start.Format(time.RFC3339),
		St:  s.Status,
		U:   s.URL,
		Src: append([]string(nil), s.Sources...),
		G:   s.Geo,
	}, nil
}

// Town is one entry of the picker file: what a visitor needs to choose a place, and what the board
// needs to compute kilometres. It changes rarely, which is why it is not in the manifest.
type Town struct {
	ID      string  `json:"id"` // <slug>-<cc>; global slugs collide (London, ON)
	Name    string  `json:"name"`
	Country string  `json:"c"` // ISO 3166-1 alpha-2, lower case
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	TZ      string  `json:"tz"` // IANA name, e.g. Europe/Copenhagen
}

// Towns is towns.json: the picker's whole data set, published once per build.
type Towns struct {
	Format  string    `json:"format"`
	BuiltAt time.Time `json:"built_at"`
	Towns   []Town    `json:"towns"`
}

// Index describes one town's published shard — the object the manifest points at. The shard itself is
// flat JSONL, one Row per line, so this type carries metadata only: a document holding the rows would
// be the interned shape ADR-001 replaced.
type Index struct {
	Town    string    `json:"town"`   // town id
	Shard   string    `json:"shard"`  // towns/<sha256>.jsonl.gz — content addressed, immutable
	SHA256  string    `json:"sha256"` // the same digest, so a client can verify what it fetched
	Count   int       `json:"count"`  // published rows in the shard
	Sources []string  `json:"sources"`
	BuiltAt time.Time `json:"built_at"`
}

// Source is one adapter's contribution to a build: whether it ran, what it yielded, and when it last
// answered. The board's coverage line is rendered from these, so a source that is off says so.
type Source struct {
	ID         string    `json:"id"`
	Index      string    `json:"index"` // sitemap | ical | api
	OK         bool      `json:"ok"`
	ParseYield float64   `json:"parse_yield"` // parsed events ÷ candidates, 0..1
	LastOK     time.Time `json:"last_ok"`
	Rows       int       `json:"rows"`
}

// Manifest is the only file a client polls: build metadata, the window, per-source health and the
// shard map. Counts live here so the picker and the coverage line need no shard fetch.
type Manifest struct {
	Format  string    `json:"format"`
	BuiltAt time.Time `json:"built_at"`
	Window  Window    `json:"window"`
	Sources []Source  `json:"sources"`
	Towns   []Index   `json:"towns"`
	// Files names the derived artifacts this build publishes beside the shards, kind → path inside the index
	// (`{"portraits":"artists/portraits-<sha>.json"}`). The name is content-addressed, so an immutable cache header
	// is honest and a returning visitor sees a new artifact the night it changes — which a stable filename with
	// `max-age=31536000` never does.
	Files map[string]string `json:"files,omitempty"`
}
