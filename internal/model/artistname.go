package model

import (
	"regexp"
	"strconv"
	"strings"
)

// A ticketer's event title is written for the ticketer's page, not for a board. Two things in it are not the
// artist, and both were measured on the live index (95 of 730 rows, 13 %):
//
//	"1/28 NOAH GUNDERSEN | DEBASER NOVA"   →  "NOAH GUNDERSEN"
//	"13/2 SHREK RAVE | DEBASER KLUBBEN"    →  "SHREK RAVE"
//
// The first is a `M/D` or `D/M` date (both orderings occur, and they are indistinguishable, so both are
// stripped); the second is the venue repeated after a pipe. Neither is safe to fix in the client: the artist
// name is the identity key, so a polluted name can never match a reader's list and can never resolve a portrait.
var leadingDate = regexp.MustCompile(`^\d{1,2}\s*/\s*\d{1,2}\s+`)

// A second shape, measured the same day: `261121 CÉILIDH`, `261205B LIVETS ORKESTER` — a compact `YYMMDD` with an
// optional letter for a second show that day. It is only stripped when the six digits really are a date, so an
// artist whose name begins with digits is left alone.
var leadingCompact = regexp.MustCompile(`^(\d{2})(\d{2})(\d{2})[A-Za-z]?\s+`)

func stripCompactDate(name string) string {
	match := leadingCompact.FindStringSubmatch(name)
	if match == nil {
		return name
	}
	year, _ := strconv.Atoi(match[1])
	month, _ := strconv.Atoi(match[2])
	day, _ := strconv.Atoi(match[3])
	if year < 20 || year > 45 || month < 1 || month > 12 || day < 1 || day > 31 {
		return name
	}
	return leadingCompact.ReplaceAllString(name, "")
}

// CleanArtistName returns the artist as the title meant it, given the venue the show is at.
//
// The rule for the pipe is structural rather than lexical: the right-hand fragment is dropped only when it looks
// like *this venue* — the identity keys are prefixes of one another, or they share their first token. That keeps
// a co-headline ("The Bug | Flowdan") intact, because neither fragment resembles the room.
func CleanArtistName(raw, venue string) string {
	name := strings.Join(strings.Fields(raw), " ")
	name = leadingDate.ReplaceAllString(name, "")
	name = stripCompactDate(name)

	if left, right, ok := strings.Cut(name, "|"); ok && looksLikeVenue(right, venue) {
		name = left
	}

	name = strings.TrimSpace(name)
	name = strings.TrimRight(name, " -–—|·,")
	return strings.Join(strings.Fields(name), " ")
}

func looksLikeVenue(fragment, venue string) bool {
	fragment = strings.TrimSpace(fragment)
	if fragment == "" || strings.TrimSpace(venue) == "" {
		return false
	}
	fragmentKey, venueKey := Key(fragment), Key(venue)
	if fragmentKey == "" || venueKey == "" {
		return false
	}
	if strings.HasPrefix(fragmentKey, venueKey) || strings.HasPrefix(venueKey, fragmentKey) {
		return true
	}
	// A platform names the room as often as the building ("DEBASER KLUBBEN" at "Debaser Nova"), so the first
	// token is the brand that decides.
	return firstToken(fragmentKey) == firstToken(venueKey)
}

func firstToken(key string) string {
	if before, _, ok := strings.Cut(key, "-"); ok {
		return before
	}
	return key
}
