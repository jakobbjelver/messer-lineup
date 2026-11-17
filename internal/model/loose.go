package model

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// platformSuffixes are the upload-channel tails YouTube and friends append to an artist: "tAKiDA -
// Topic" is tAKiDA. One is dropped, because a real band can end in a word like "Music".
var platformSuffixes = []string{"-topic", "-vevo", "-official", "-band", "-music"}

// Loose is the relaxed form of a key. It never joins anything on its own: it nominates a candidate
// that an alias, a platform-native id or an identical display string has to confirm. That is how "The
// National" avoids absorbing a band called National while "The Cure" ↔ "Cure" stays possible.
//
// Steps:
//
//	 9  drop a leading "the-"   when the remainder is ≥2 chars, starts with a letter, and is not "the"
//	10  drop one trailing "-topic" / "-vevo" / "-official" / "-band" / "-music"
//	11  drop a trailing "-<1–2 digits>"   (a disambiguator, never part of a name: blink-182 survives)
func Loose(key string) string {
	k := key
	if rest, ok := strings.CutPrefix(k, "the-"); ok && len(rest) >= 2 && rest != "the" && startsWithLetter(rest) {
		k = rest
	}
	for _, suffix := range platformSuffixes {
		if rest, ok := strings.CutSuffix(k, suffix); ok && rest != "" {
			k = rest
			break
		}
	}
	return trimDisambiguator(k)
}

func startsWithLetter(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsLetter(r)
}

// trimDisambiguator removes a one- or two-digit numeric tail: "bon-iver-2" is Bon Iver, and
// "blink-182" is not a disambiguated blink.
func trimDisambiguator(k string) string {
	i := strings.LastIndex(k, "-")
	if i <= 0 || i == len(k)-1 {
		return k
	}
	digits := k[i+1:]
	if len(digits) > 2 {
		return k
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return k
		}
	}
	return k[:i]
}
