package model

import "testing"

// The cases are the live index's own rows (measured 2026-11-19), not invented ones.
func TestCleanArtistName(t *testing.T) {
	cases := []struct {
		raw, venue, want string
	}{
		{"1/28 NOAH GUNDERSEN | DEBASER NOVA", "DEBASER NOVA", "NOAH GUNDERSEN"},
		{"13/2 SHREK RAVE | DEBASER KLUBBEN", "DEBASER NOVA", "SHREK RAVE"},
		{"11/12 DEBASER SELECTS | DEBASER STRAND & NOVA", "Debaser Strand", "DEBASER SELECTS"},
		{"15/1 NATIONALTEATERNS ROCKORKESTER | DEBASER", "Debaser Strand", "NATIONALTEATERNS ROCKORKESTER"},
		{"12/3 MATTIAS ALKBERG & CONNY NIMMERSJÖ | DEBASER", "Debaser Strand", "MATTIAS ALKBERG & CONNY NIMMERSJÖ"},
		// Already clean: no date, no pipe.
		{"Archspire + Crown Magnetar + The Zenith Passage", "Kollektivet Livet", "Archspire + Crown Magnetar + The Zenith Passage"},
		{"Näääk & Nimo", "Debaser Strand", "Näääk & Nimo"},
		// Two separators: only the trailing room goes, the co-headline stays.
		{"FUNERAL | THE CRYPT | DEBASER NOVA", "Debaser Nova", "FUNERAL | THE CRYPT"},
		// A co-headline whose second half is not the room survives whole.
		{"The Bug | Flowdan", "Vega", "The Bug | Flowdan"},
		// An artist that begins with digits is not a date unless it carries a slash.
		{"65daysofstatic", "Pumpehuset", "65daysofstatic"},
		// The compact date shape, measured on the same shard: `YYMMDD` plus an optional letter for a second show.
		{"261121 CÉILIDH", "Cap30", "CÉILIDH"},
		{"261205B LIVETS ORKESTER", "Pumpehuset", "LIVETS ORKESTER"},
		// Six digits that are not a date are part of the name.
		{"991341 SOMETHING", "Vega", "991341 SOMETHING"},
		// Whitespace is normalised, and a trailing separator goes.
		{"  Björk  |  ", "Vega", "Björk"},
		{"", "Vega", ""},
	}
	for _, tc := range cases {
		got := CleanArtistName(tc.raw, tc.venue)
		if got != tc.want {
			t.Errorf("CleanArtistName(%q, %q) = %q, want %q", tc.raw, tc.venue, got, tc.want)
		}
	}
}

// The cleaned name is what the identity key is computed from — a polluted key can never match a reader's list.
func TestCleanArtistNameFeedsTheKey(t *testing.T) {
	before := Key("1/28 NOAH GUNDERSEN | DEBASER NOVA")
	after := Key(CleanArtistName("1/28 NOAH GUNDERSEN | DEBASER NOVA", "DEBASER NOVA"))
	if before == after {
		t.Fatalf("the key did not change: %q", before)
	}
	if after != Key("Noah Gundersen") {
		t.Fatalf("cleaned key = %q, want the key of the plain name %q", after, Key("Noah Gundersen"))
	}
}
