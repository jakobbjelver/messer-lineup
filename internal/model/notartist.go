package model

import (
	"errors"
	"strings"
)

// ErrNotAnArtist is a detail that parsed but is not a show: the platform used its own ticket product as the
// event's name, so the "artist" is an admission label rather than a performer.
//
// This is not a category filter. ADR-003 forbids those at ingest, because keyword rules break on real music
// ("Vinkoncert med R E M I S D U O" is a concert, and a pancake workshop is not) — the mix is published and
// filtered for display. A ticket label is a different thing entirely: the field is not an artist *name*, and the
// board would print it as one. The list is deliberately tiny and exact; a title that is not on it is published.
var ErrNotAnArtist = errors.New("not an artist")

// notArtistLabels are whole titles, after Key, that name an admission product rather than a performer.
// `Entrébiljetter/Tickets` is Tickster's own label for unticketed museum admission — 44 rows at one Stockholm
// museum in the 2026-11-18 sweep.
var notArtistLabels = map[string]bool{
	"entrebiljetter-tickets": true,
	"entrebiljetter":         true,
	"entrebiljett":           true,
	"biljetter":              true,
	"biljett":                true,
	"tickets":                true,
	"ticket":                 true,
	"presentkort":            true,
	"sasongskort":            true,
}

// NotAnArtistTitle reports whether a title is an admission label. Matching is on the whole normalised title, so
// "Tickets to the Gun Show" stays a show and "Tickets" does not.
func NotAnArtistTitle(title string) bool {
	key := Key(title)
	if key == "" {
		return false
	}
	if notArtistLabels[key] {
		return true
	}
	// Tickster joins the two market spellings into one title: "Entrébiljetter/Tickets".
	return strings.Contains(key, "entrebiljetter")
}
