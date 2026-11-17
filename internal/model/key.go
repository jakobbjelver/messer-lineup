package model

import (
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Key is the identity key for an artist or a venue: the string that decides whether two rows are the
// same thing. `key` joins, `loose` only nominates, and an alias decides (identity.md in the design).
//
// The algorithm is fixed by the shared vector file, internal/model/testdata/keys.json, which both this
// implementation and the TypeScript twin are diffed against. Implementations are changed to match the
// vectors; a vector is never edited to match an implementation.
//
// Steps, in order:
//
//	1  NFKD, drop combining marks          Björk → bjork · Sigur Rós → sigur ros
//	2  transliterate what NFKD cannot decompose
//	                                      ø→o · æ→ae · œ→oe · ß→ss · þ→th · ð→d · ł→l · đ→d · ı→i
//	3  lowercase
//	4  "&" → " and "
//	5  drop apostrophes and quotation marks   don't → dont · "Weird Al" → weird al
//	6  cut at the first " feat." / " ft." / " featuring "   (the full credit stays in the displayed name)
//	7  every run of [^a-z0-9] → "-", then trim
//	8  empty → "x-" + sha1(name)[:10]      ¥$ → x-ebd798351b   (never an empty key)
func Key(name string) string {
	if k := normalize(name); k != "" {
		return k
	}
	// Nothing survived normalisation. Derive a stable key from the name as given, so an unpronounceable
	// pair of symbols still has an identity instead of an empty string joining everything to everything.
	sum := sha1.Sum([]byte(name))
	return "x-" + hex.EncodeToString(sum[:])[:10]
}

var (
	// Every run of anything that is not a lowercase ASCII letter or a digit becomes one separator.
	nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)
	// A credit marker ends the artist: "A feat. B" is A, because that is what a taste list contains.
	creditMarkers = []string{" feat.", " ft.", " featuring "}
	// Apostrophes and quotation marks are deleted, not replaced: don't → dont, not don-t.
	quotes = "'’‘`´\"“”„‚"
	// translit covers the letters NFKD leaves alone. Everything else accented in Latin script
	// decomposes into a base letter plus a combining mark, which stripMarks removes.
	translit = strings.NewReplacer(
		"ø", "o", "æ", "ae", "œ", "oe", "ß", "ss", "þ", "th", "ð", "d", "ł", "l", "đ", "d", "ı", "i",
	)
)

func normalize(name string) string {
	s := strings.ToLower(name)
	s = norm.NFKD.String(s)
	s = stripMarks(s)
	s = translit.Replace(s)
	s = strings.ReplaceAll(s, "&", " and ")
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(quotes, r) {
			return -1
		}
		return r
	}, s)
	if i := creditIndex(s); i >= 0 {
		s = s[:i]
	}
	s = nonAlnum.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// stripMarks drops combining marks left behind by NFKD (the diaeresis in Björk, the accents in Rós).
func stripMarks(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// creditIndex returns the position of the first credit marker, or -1.
func creditIndex(s string) int {
	at := -1
	for _, marker := range creditMarkers {
		if i := strings.Index(s, marker); i >= 0 && (at < 0 || i < at) {
			at = i
		}
	}
	return at
}
