// Package venues decides whether a listing is a music event.
//
// The two signals are deliberately different in kind. The registry is curated data: a room either programmes
// music or it does not (a theatre, a cinema, a museum, a restaurant with a stage). That decision is stable for
// years and it is where the model's real error lives — 374 of Stockholm's 718 published rows were theatre,
// julbord, stand-up and museum workshops, and almost all of them were at rooms that never programme a gig.
//
// The form rule is the second half, for the rooms that do both: a church that hosts concerts also hosts
// services, a club that hosts bands also hosts a quiz. It matches *event forms* — "julbord", "provning",
// "workshop", "standup" — and never tries to recognise music. A positive rule was measured and discarded:
// "Elton John meets Freddie Mercury" carries no music word and is a gig, while "JULKONSERT: Hör julens jubel!"
// carries one and is also a gig. A rule that must see the word "konsert" loses the first and gains nothing on
// the second.
//
// What must NOT be a token: "teater". Tickster appends the room to the title ("Billy Opel - Södra Teatern"),
// and a band is called Nationalteaterns Rockorkester — a "teater" token dropped nine real gigs, measured
// 2026-11-20. The room is the registry's job. "supe" is not a token either: it matches "Super Night".
package venues

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jakobbjelver/messer-lineup/internal/model"
)

// FormatVenues is the registry file's format id.
const FormatVenues = "lineup-venues/1"

// Class is what a room's programme is.
type Class string

const (
	// Music is a room that programmes music: a club, a concert hall, a church whose listings are concerts.
	Music Class = "music"
	// Other is a room whose programme is not music: theatre, cinema, museum, restaurant, sport, conference.
	Other Class = "other"
)

// Registry is every classified room, keyed the way a row keys a venue.
type Registry struct {
	byKey map[string]Class
	name  map[string]string // key → the spelling the registry was written with
}

// Entry is one line of a registry file.
type Entry struct {
	Name  string `json:"name"`
	Class string `json:"class"`
}

type file struct {
	Format string  `json:"format"`
	Market string  `json:"market"`
	Venues []Entry `json:"venues"`
}

// LoadDir reads every *.json in dir into one registry. Files are per market (`se`, `dk`); a venue key belongs
// to exactly one market, so two files naming the same key are a mistake in the data, not a merge.
func LoadDir(dir string) (*Registry, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, fmt.Errorf("venues: %w", err)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("venues: no registry files in %s", dir)
	}
	sort.Strings(paths)
	r := &Registry{byKey: map[string]Class{}, name: map[string]string{}}
	for _, path := range paths {
		if err := r.load(path); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// LoadFile reads one registry file. It exists for tests and for a market that ships as a single file.
func LoadFile(path string) (*Registry, error) {
	r := &Registry{byKey: map[string]Class{}, name: map[string]string{}}
	if err := r.load(path); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Registry) load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("venues: %w", err)
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("venues: %s: %w", path, err)
	}
	if f.Format != FormatVenues {
		return fmt.Errorf("venues: %s: format %q, want %q", path, f.Format, FormatVenues)
	}
	for _, e := range f.Venues {
		name := strings.Join(strings.Fields(e.Name), " ")
		key := model.Key(name)
		if key == "" {
			return fmt.Errorf("venues: %s: empty venue name", path)
		}
		class := Class(e.Class)
		if class != Music && class != Other {
			return fmt.Errorf("venues: %s: %q has class %q, want %q or %q", path, name, e.Class, Music, Other)
		}
		if have, ok := r.byKey[key]; ok {
			return fmt.Errorf("venues: %s: %q repeats %q (key %s, already %s)", path, name, r.name[key], key, have)
		}
		r.byKey[key] = class
		r.name[key] = name
	}
	return nil
}

// Len is how many rooms the registry knows.
func (r *Registry) Len() int {
	if r == nil {
		return 0
	}
	return len(r.byKey)
}

// Counts is (music, other).
func (r *Registry) Counts() (int, int) {
	music, other := 0, 0
	if r == nil {
		return 0, 0
	}
	for _, c := range r.byKey {
		if c == Other {
			other++
			continue
		}
		music++
	}
	return music, other
}

// Class is the room's class, and whether the registry knows the room at all.
func (r *Registry) Class(venueKey string) (Class, bool) {
	if r == nil {
		return "", false
	}
	c, ok := r.byKey[venueKey]
	return c, ok
}

// Verdict is one listing's fate. Reason is "" for a kept row, and otherwise names why it went:
// "venue" (a room that does not programme music), "form:<token>" (an event form that is never a gig), or
// "unclassified" for a kept row at a room the registry has not seen.
type Verdict struct {
	Keep   bool
	Reason string
}

// Decide applies the policy. A room the registry has not classified is kept: a new market must be measurable
// before its rooms are curated, and the report counts what was kept unclassified so the curation has a work
// list. Dropping unknown rooms would make a new town publish an empty index and look like a broken parser.
func (r *Registry) Decide(venueKey, title string) Verdict {
	class, known := r.Class(venueKey)
	if known && class == Other {
		return Verdict{Keep: false, Reason: "venue"}
	}
	if token := NonMusicForm(title); token != "" {
		return Verdict{Keep: false, Reason: "form:" + token}
	}
	if !known {
		return Verdict{Keep: true, Reason: "unclassified"}
	}
	return Verdict{Keep: true}
}

// NonMusicForm returns the event-form token that makes a title not a gig, or "".
//
// Tokens are matched against the folded title (model.Key's normal form), so Swedish and Danish compounds are
// covered without word-boundary guesswork: "julbord" catches "Julbord - eftermiddag (afternoon) 14:00",
// "humor" catches "Humorquizet", "baka" catches "Familjesöndag: baka pepparkakor".
func NonMusicForm(title string) string {
	key := model.Key(title)
	if key == "" {
		return ""
	}
	for _, token := range formTokens {
		if strings.Contains(key, token) {
			return token
		}
	}
	return ""
}

// formTokens are event forms that are never a gig. Every entry was measured against the live index on
// 2026-11-20: each one names an event that is not music, and no entry drops a title that is a gig. See the
// package comment for the two that must never be added.
//
// "musical", "dinner" and "allsång" are the three the Swedish form list could not see, measured on the
// three-town index on 2026-11-20: "Legally Blonde - The musical" (English), "Christmas Dinner Show på Valand"
// and "DANCING QUEEN – A CHRISTMAS DINNER SHOW" (an English dinner show), and "Allsång i Hjorthagen" (a
// community sing-along, not a gig). The venue names are untouched: "Södra Teaterns Stora Scen" and
// "Teatern i Hjorthagens Kulturhus" are concert rooms, which is why "teater" is still not a token.
var formTokens = []string{
	"julbord", "buffe", "brunch", "middag", "dinner", "provning", "tasting", "workshop", "kurs", "kollo",
	"forelasning", "seminarium", "konferens", "visning", "guidning", "vandring",
	"standup", "stand-up", "komedi", "humor", "quiz", "bingo", "karaoke",
	"forestallning", "musikal", "musical", "allsang", "vernissage", "utstallning", "loppis", "marknad",
	"gudstjanst", "andakt", "pyssel", "lunchtrav", "travbana", "fotboll", "hockey", "basket",
	"filmklubb", "film-och", "yoga", "traning", "frukost", "dating", "familjesondag",
	"ljustandning", "baka", "kidz", "nyarsskal", "mingel", "wrestling", "vendors",
}
