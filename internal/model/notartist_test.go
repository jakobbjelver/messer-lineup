package model

import "testing"

func TestATicketLabelIsNotAnArtist(t *testing.T) {
	// The exact title that put 44 museum-admission rows on the Stockholm board (sweep 2026-11-18).
	for _, title := range []string{
		"Entrébiljetter/Tickets",
		"Entrébiljetter",
		"Tickets",
		"Biljetter",
		"Presentkort",
	} {
		if !NotAnArtistTitle(title) {
			t.Fatalf("%q should be an admission label", title)
		}
	}
	// Real titles that merely contain the word are shows, not labels: matching is on the whole title.
	for _, title := range []string{
		"Nazareth - Right, Left, Centre Tour 2026",
		"Tomorrow shall be – Fri entré, ingen bilj behövs",
		"Tickets to the Gun Show",
		"A Winged Victory for the Sullen",
		"",
	} {
		if NotAnArtistTitle(title) {
			t.Fatalf("%q is a show title, not an admission label", title)
		}
	}
}
