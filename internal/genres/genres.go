// Package genres turns a provider's free-text tags into the sixteen words the board can offer, and publishes the map
// from a genre to the artists that play it.
//
// Last.fm is the source, not a taxonomy: an artist's top tags are whatever listeners typed — `progressive metal`,
// `seen live`, `swedish`, `favorites`, `death metal`, `Progressive Rock`. Two rules follow from that:
//
//   - **The vocabulary is small and closed.** A picker with 400 tags is a search box; sixteen buckets is a choice. Every
//     tag either lands in one of the sixteen or is counted as unmapped, and the count is in the report so the list can
//     be extended from evidence rather than taste.
//   - **The order comes from the tags, not from the bucket list.** An artist whose first tags are `pop` and `dance` is a
//     pop act who also makes dance music; the first three surviving tags decide, and the bucket order is only a
//     tie-break for duplicates.
package genres

import (
	"sort"
	"strings"
)

// Tag is one provider tag with its weight (Last.fm's 0–100 count).
type Tag struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Genre is one of the sixteen.
type Genre struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// All is the canonical vocabulary, in the order the picker shows it. The ids are the wire form and never change; the
// labels are what a reader sees, and a label can be reworded without moving a token.
var All = []Genre{
	{"rock", "Rock"},
	{"metal", "Metal"},
	{"punk", "Punk"},
	{"pop", "Pop"},
	{"electronic", "Electronic"},
	{"hip-hop", "Hip-hop"},
	{"folk", "Folk"},
	{"jazz", "Jazz"},
	{"soul", "Soul"},
	{"blues", "Blues"},
	{"country", "Country"},
	{"classical", "Classical"},
	{"reggae", "Reggae"},
	{"world", "World"},
	{"experimental", "Experimental"},
	{"soundtrack", "Soundtrack"},
}

// Max is the most genres one board may ask for. It is a picker cap and a token cap; six words is a taste, sixteen is
// a dump.
const Max = 6

var valid = func() map[string]bool {
	out := make(map[string]bool, len(All))
	for _, genre := range All {
		out[genre.ID] = true
	}
	return out
}()

// Known reports whether id is one of the sixteen.
func Known(id string) bool { return valid[id] }

// Label returns the display label for an id, or the id itself when it is unknown (a token from a newer build).
func Label(id string) string {
	for _, genre := range All {
		if genre.ID == id {
			return genre.Label
		}
	}
	return id
}

// stoplist is the tags that say nothing about a genre: where the artist is from, what a listener feels, and the
// decades the board does not filter on. `live` is here because `seen live` is the most common Last.fm tag that is not
// music.
var stoplist = map[string]bool{
	"swedish": true, "svenskt": true, "svensk": true, "sverige": true, "sweden": true,
	"scandinavian": true, "scandinavia": true, "nordic": true,
	"danish": true, "norwegian": true, "finnish": true, "icelandic": true,
	// Nationalities are the largest unmapped family after the province words: they say where a band is from, which the
	// board does not filter on, and they crowd out the tags that are real gaps in the vocabulary.
	"american": true, "british": true, "usa": true, "uk": true, "german": true, "french": true, "irish": true,
	"australian": true, "canadian": true, "european": true,
	"seen live": true, "set live": true, "live": true,
	"favorites": true, "favourites": true, "favorite": true, "favourite": true,
	"female vocalists": true, "male vocalists": true, "female vocalist": true, "male vocalist": true,
	// Listener noise: a tag that could be put on anything is not a genre.
	"all": true, "instrumental": true, "guitar": true, "chillout": true,
	"eurovision": true, "spotify": true,
	"60s": true, "70s": true, "80s": true, "90s": true, "00s": true, "10s": true, "20s": true,
	"awesome": true, "beautiful": true, "good": true, "best": true, "chill": true, "party": true,
}

// aliases maps a substring of a normalised tag to a bucket. It is checked in this order, so the more specific word
// wins: `pop rock` is rock, `synth pop` is pop, `folk rock` is folk. Substrings rather than a synonym list because the
// long tail is combinatorial (`atmospheric black metal`, `melodic death metal`, `swedish death metal`) and every one
// of those is metal.
var aliases = []struct {
	needle string
	id     string
}{
	// Metal first: it is the most reliably tagged corner of Last.fm and `metal` alone must never fall through to rock.
	{"metal", "metal"}, {"metalcore", "metal"}, {"grindcore", "metal"}, {"sludge", "metal"}, {"doom", "metal"}, {"stoner", "metal"},
	{"blackgaze", "metal"}, {"djent", "metal"},

	{"punk", "punk"}, {"hardcore", "punk"}, {"emo", "punk"}, {"shoegaze", "punk"}, {"grunge", "punk"},

	{"hip hop", "hip-hop"}, {"hip-hop", "hip-hop"}, {"hiphop", "hip-hop"}, {"rap", "hip-hop"}, {"grime", "hip-hop"},

	{"techno", "electronic"}, {"house", "electronic"}, {"ambient", "electronic"}, {"trance", "electronic"},
	{"dance", "electronic"}, {"electro", "electronic"}, {"idm", "electronic"}, {"drum and bass", "electronic"},
	{"dnb", "electronic"}, {"dubstep", "electronic"}, {"synth", "electronic"}, {"ebm", "electronic"},

	{"r&b", "soul"}, {"rnb", "soul"}, {"soul", "soul"}, {"funk", "soul"}, {"motown", "soul"}, {"neo soul", "soul"},

	{"jazz", "jazz"}, {"bebop", "jazz"}, {"swing", "jazz"},

	{"blues", "blues"},

	{"classical", "classical"}, {"baroque", "classical"}, {"opera", "classical"}, {"choral", "classical"},
	{"orchestral", "classical"}, {"symphony", "classical"}, {"requiem", "classical"},

	{"country", "country"}, {"americana", "country"}, {"bluegrass", "country"},

	{"reggae", "reggae"}, {"ska", "reggae"}, {"dub", "reggae"},

	{"afrobeat", "world"}, {"latin", "world"}, {"flamenco", "world"}, {"bossa", "world"},
	{"salsa", "world"}, {"klezmer", "world"}, {"arabic", "world"}, {"bollywood", "world"}, {"fado", "world"},

	{"singer-songwriter", "folk"}, {"singer songwriter", "folk"}, {"folk", "folk"}, {"acoustic", "folk"},
	{"visa", "folk"}, {"ballad", "folk"},

	{"indie rock", "rock"}, {"alt rock", "rock"}, {"alternative", "rock"}, {"rock", "rock"}, {"indie", "rock"},
	{"psychedelia", "rock"}, {"progg", "rock"},
	{"post-rock", "rock"}, {"psychedelic", "rock"}, {"garage", "rock"}, {"progressive rock", "rock"},

	{"pop", "pop"}, {"chamber pop", "pop"}, {"k-pop", "pop"}, {"europop", "pop"},

	{"experimental", "experimental"}, {"noise", "experimental"}, {"avant", "experimental"}, {"industrial", "experimental"},

	{"soundtrack", "soundtrack"}, {"score", "soundtrack"}, {"film music", "soundtrack"}, {"musical", "soundtrack"},
}

// Normalise is the folding every tag and every artist name goes through: lowercase, underscores and runs of spaces to
// one space. Public because the caller wants the same key when it reports an unmapped tag twice.
func Normalise(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", " ")
	return strings.Join(strings.Fields(value), " ")
}

// Bucket maps an artist's tags to at most three genres, in the order the tags were given.
//
// The input is expected in weight order (Last.fm returns it that way); this function does not sort, because a provider
// that returns its own order is the provider's statement about the artist. Duplicates are dropped, so `metal` and
// `death metal` produce one bucket.
func Bucket(tags []Tag) []string {
	out := make([]string, 0, 3)
	seen := map[string]bool{}
	for _, tag := range tags {
		name := Normalise(tag.Name)
		if name == "" || stoplist[name] {
			continue
		}
		id := bucketFor(name)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if len(out) == 3 {
			break
		}
	}
	return out
}

// bucketFor is the one mapping function. An exact hit wins over a substring so `pop` does not become `rock` through
// `progressive`.
func bucketFor(name string) string {
	if id, ok := exact[name]; ok {
		return id
	}
	for _, alias := range aliases {
		if strings.Contains(name, alias.needle) {
			return alias.id
		}
	}
	return ""
}

// exact holds the tags whose spelling would be misread by a substring rule (or that have no substring at all).
var exact = map[string]string{
	"pop": "pop", "rock": "rock", "metal": "metal", "punk": "punk", "jazz": "jazz", "blues": "blues",
	"folk": "folk", "country": "country", "classical": "classical", "reggae": "reggae", "soul": "soul",
	"funk": "soul", "electro": "electronic", "electronic": "electronic", "ambient": "electronic",
	"experimental": "experimental", "soundtrack": "soundtrack", "singer-songwriter": "folk",
	"hip hop": "hip-hop", "hip-hop": "hip-hop", "rap": "hip-hop", "r&b": "soul", "rnb": "soul",
	"indie": "rock", "alternative": "rock", "grunge": "punk", "shoegaze": "punk",
	"world": "world", "latin": "world", "reggaeton": "world",
	"musical": "soundtrack", "opera": "classical", "choral": "classical",
}

// Unmapped returns the tags that fall outside the vocabulary, most frequent first, for the report. It is a measurement
// of the list, not a filter: an unmapped tag costs nothing but a missing genre.
func Unmapped(tags []Tag) []Tag {
	counts := map[string]int{}
	for _, tag := range tags {
		name := Normalise(tag.Name)
		if name == "" || stoplist[name] || bucketFor(name) != "" {
			continue
		}
		counts[name]++
	}
	out := make([]Tag, 0, len(counts))
	for name, count := range counts {
		out = append(out, Tag{Name: name, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}
