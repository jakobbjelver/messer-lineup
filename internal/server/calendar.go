package server

// The calendar feed, in the dev server.
//
// `/calendar/<token>.ics` is the one route that renders rather than serves, and it exists twice — here and in
// `src/worker.ts` — because the contract (`scripts/contract.mjs`) is the promise that the two runtimes answer
// the same way. The rules are therefore written twice on purpose and held together by that test:
//
//   - the token is base64url of `{v,t,f?,o?,a?,s?}`, unsigned (see `src/calendar/spec.ts` for why a signature
//     would buy nothing here);
//   - **the window is optional, and its absence is the point.** A subscription is a URL a client polls for as
//     long as it lives, so a window baked in at subscribe time is a feed that dies: measured 2026-11-20, a valid
//     token whose window had passed answered 0 events while the index held 718. Absent, the feed follows the
//     published window, which the nightly publish moves. Present, an older token keeps its frozen window.
//   - **`t: "anywhere"` is a reserved scope, not a town**: it renders every published shard and labels each event
//     with the town it is actually in, which is why a feed row carries its own label and not the feed's.
//   - times are UTC, so no VTIMEZONE has to be kept in step with a venue's offset;
//   - `UID` is the row key, because a subscribed calendar re-fetches and a changing UID is a duplicate event;
//   - `DTEND` is start + 3h, the only duration the index can honestly imply;
//   - lines fold at 75 octets and text is escaped per RFC 5545 §3.3.11.

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jakobbjelver/messer-lineup/internal/genres"
	"github.com/jakobbjelver/messer-lineup/internal/index"
	"github.com/jakobbjelver/messer-lineup/internal/model"
)

// calendarDuration is the block a client draws: the index knows a start and nothing about an end.
const calendarDuration = 3 * time.Hour

// calendarAnywhere is the reserved scope id: every published town at once. It is never a published town (ids are
// `<slug>-<cc>`, so it cannot collide) and never a default — only an explicit pick can ask for it.
const calendarAnywhere = "anywhere"

// calendarDay is the wire form of a day in the token and in a row.
const calendarDay = "2006-01-02"

type calendarSpec struct {
	V int             `json:"v"`
	T string          `json:"t"`
	F string          `json:"f,omitempty"`
	O string          `json:"o,omitempty"`
	A []string        `json:"a,omitempty"`
	S *calendarSource `json:"s,omitempty"`
	// R asks the client for a day-before alarm. A v2 field, and `1` is the only value: a switch, not a setting.
	R int `json:"r,omitempty"`
	// G is a genre filter. A v2 field: the feed's artist set is A ∪ ⋃ genres[G].
	G []string `json:"g,omitempty"`
}

// calendarFields is the whole vocabulary of a token this build serves. See decodeCalendarSpec for why the set is
// enforced rather than trusted.
var calendarFields = map[string]bool{"v": true, "t": true, "f": true, "o": true, "a": true, "s": true, "r": true, "g": true}

// calendarSource says where a live artist list comes from. The snapshot in `a` stays the fallback: a provider that
// is down, a handle that was deleted or an account whose statistics are not computed yet must degrade to the list
// the subscriber saw — never to an empty calendar and never to the whole town.
type calendarSource struct {
	K string `json:"k"` // the only source today: listenbrainz
	U string `json:"u"` // the handle
}

// feedRow is a row with the town it belongs to. A one-town feed uses one label for every row; an anywhere feed
// needs each event's own town in `LOCATION`, because "Debaser, Anywhere" is not a place.
type feedRow struct {
	Row  model.Row
	Town string
}

// calendar serves `/calendar/<token>.ics`.
func (s *server) calendar(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/calendar/")
	if !strings.HasSuffix(name, ".ics") || name == ".ics" {
		writeError(w, http.StatusNotFound, "not_found", "a calendar feed is /calendar/<token>.ics")
		return
	}
	spec, ok := decodeCalendarSpec(strings.TrimSuffix(name, ".ics"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_calendar", "that link is not a board specification")
		return
	}

	entries := s.manifest.Towns
	if spec.T != calendarAnywhere {
		entry, found := s.townEntry(spec.T)
		if !found {
			writeError(w, http.StatusNotFound, "unknown_town", fmt.Sprintf("this index does not publish %q", spec.T))
			return
		}
		entries = []model.Index{entry}
	}

	// The window: the token's when it names one, the published one otherwise.
	from, to := s.manifest.Window.From.Format(calendarDay), s.manifest.Window.To.Format(calendarDay)
	if spec.F != "" {
		from, to = spec.F, spec.O
	}

	// The filter: an explicit list, a live source that falls back to it, and the genres unioned in. A genre-only token
	// is a board — and a genre that resolves to nothing is an empty feed, never the whole town.
	artists := spec.A
	filtered := len(spec.A) > 0 || len(spec.G) > 0
	if spec.S != nil && spec.S.K == "listenbrainz" {
		filtered = true
		if keys, answered := s.taste.keys(spec.S.U); answered {
			artists = keys
		}
	}
	wanted := make(map[string]bool, len(artists))
	for _, key := range artists {
		wanted[key] = true
	}
	if len(spec.G) > 0 {
		fromGenres, err := s.genreArtists(spec.G)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "genres_unreadable", err.Error())
			return
		}
		for key := range fromGenres {
			wanted[key] = true
		}
	}

	labels := s.townLabels()
	kept := make([]feedRow, 0, 256)
	for _, entry := range entries {
		rows, err := index.ReadShard(filepath.Join(s.opts.IndexDir, filepath.FromSlash(entry.Shard)))
		if err != nil {
			// One unreadable shard makes an anywhere feed a wrong answer rather than a smaller one.
			writeError(w, http.StatusInternalServerError, "shard_unreadable", err.Error())
			return
		}
		for _, row := range rows {
			day := row.D
			if len(day) > 10 {
				day = day[:10]
			}
			if day < from || day > to {
				continue
			}
			if filtered && !wanted[model.Key(row.A)] {
				continue
			}
			town := labels[row.C]
			if town == "" {
				town = labels[entry.Town]
			}
			kept = append(kept, feedRow{Row: row, Town: town})
		}
	}
	sort.SliceStable(kept, func(i, j int) bool {
		if kept[i].Row.D != kept[j].Row.D {
			return kept[i].Row.D < kept[j].Row.D
		}
		if kept[i].Row.A != kept[j].Row.A {
			return kept[i].Row.A < kept[j].Row.A
		}
		return kept[i].Row.K < kept[j].Row.K
	})

	name, description := s.feedTitle(spec.T, labels, filtered, len(wanted), spec.G)
	builtAt := s.manifest.BuiltAt
	if builtAt.IsZero() {
		builtAt = s.opts.Now()
	}
	body := buildCalendar(kept, name, description, builtAt, spec.R == 1)
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `inline; filename="lineup-`+feedSlug(spec.T)+`.ics"`)
	// Fifteen minutes: short enough to follow a nightly publish, long enough that a mail client polling twice an
	// hour does not reach the disk.
	w.Header().Set("Cache-Control", "public, max-age=900")
	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write([]byte(body))
}

// feedTitle is what a calendar client shows in its sidebar, and the sentence under it. Both name the contents,
// because "which artists is this calendar?" should be answerable in the client and not only on the board.
func (s *server) feedTitle(scope string, labels map[string]string, filtered bool, artists int, genreIDs []string) (string, string) {
	word := "artists"
	if artists == 1 {
		word = "artist"
	}
	if scope == calendarAnywhere {
		if filtered {
			return "Messer Lineup — Anywhere",
				fmt.Sprintf("Shows for %d %s, anywhere, from the Messer Lineup index.", artists, word)
		}
		return "Messer Lineup — Anywhere", "Every show, in every published town, from the Messer Lineup index."
	}
	label := labels[scope]
	if label == "" {
		label = scope
	}
	if len(genreIDs) > 0 {
		named := make([]string, len(genreIDs))
		for index, id := range genreIDs {
			named[index] = genres.Label(id)
		}
		list := strings.Join(named, ", ")
		if scope == calendarAnywhere {
			return "Messer Lineup — Anywhere",
				fmt.Sprintf("Shows in %s, anywhere, from the Messer Lineup index.", list)
		}
		return "Messer Lineup — " + label,
			fmt.Sprintf("Shows in %s in %s, from the Messer Lineup index.", list, label)
	}
	if filtered {
		return "Messer Lineup — " + label,
			fmt.Sprintf("Shows for %d %s in %s, from the Messer Lineup index.", artists, word, label)
	}
	return "Messer Lineup — " + label, "Shows in " + label + ", from the Messer Lineup index."
}

// feedSlug keeps a filename safe for a scope that is an id or a reserved word.
func feedSlug(scope string) string {
	var b strings.Builder
	for _, r := range scope {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 32)
		}
	}
	if b.Len() == 0 {
		return "board"
	}
	return b.String()
}

func (s *server) townEntry(id string) (model.Index, bool) {
	for _, entry := range s.manifest.Towns {
		if entry.Town == id {
			return entry, true
		}
	}
	return model.Index{}, false
}

// townLabels is every published town's display name in one read, keyed by id.
//
// The rule is the index's: the shortest name a town was seen under, because platforms name districts as often as
// cities and "København Ø" is a district of "København". Read once per feed rather than once per row.
func (s *server) townLabels() map[string]string {
	out := map[string]string{}
	for _, entry := range s.manifest.Towns {
		out[entry.Town] = entry.Town
	}
	blob, err := os.ReadFile(filepath.Join(s.opts.IndexDir, "towns.json"))
	if err != nil {
		return out
	}
	var towns model.Towns
	if err := json.Unmarshal(blob, &towns); err != nil {
		return out
	}
	for _, town := range towns.Towns {
		if town.Name == "" {
			continue
		}
		if best, found := out[town.ID]; !found || town.Name != town.ID && (best == town.ID || len(town.Name) < len(best)) {
			out[town.ID] = town.Name
		}
	}
	return out
}

// townLabel is one town's label, kept for the callers that ask about a single town.
func (s *server) townLabel(id string) string {
	return s.townLabels()[id]
}

// decodeCalendarSpec is strict on purpose: a token that is nearly valid is a link that will silently show the wrong
// board, so anything unexpected is refused rather than defaulted.
//
// The key set is checked before the struct is filled. `json.Unmarshal` ignores fields it does not know, and that is
// the one failure a subscriber can never see: a token asking for a filter this build has not implemented would read as
// "no filter" and the feed would answer the whole town. A field from a newer build is therefore a refusal (`400`),
// never a default — adding one means adding it here and to `calendarSpec` in the same change.
func decodeCalendarSpec(token string) (calendarSpec, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(token, "="))
	if err != nil {
		return calendarSpec{}, false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return calendarSpec{}, false
	}
	for key := range fields {
		if !calendarFields[key] {
			return calendarSpec{}, false
		}
	}
	var spec calendarSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return calendarSpec{}, false
	}
	if (spec.V != 1 && spec.V != 2) || spec.T == "" || len(spec.T) > 64 {
		return calendarSpec{}, false
	}
	// A window is both ends or neither: half a window is a link that lies about what it shows.
	if (spec.F == "") != (spec.O == "") {
		return calendarSpec{}, false
	}
	if spec.F != "" && (!validDay(spec.F) || !validDay(spec.O) || spec.F > spec.O) {
		return calendarSpec{}, false
	}
	if len(spec.A) > 200 {
		return calendarSpec{}, false
	}
	for _, key := range spec.A {
		if key == "" || len(key) > 96 {
			return calendarSpec{}, false
		}
	}
	if spec.S != nil {
		if spec.S.K != "listenbrainz" || spec.S.U == "" || len(spec.S.U) > 64 {
			return calendarSpec{}, false
		}
	}
	// A reminder is a v2 field and `1` is the only value. A v1 token carrying it was written by something that did not
	// understand the version it claimed, and guessing would put an alarm on somebody's phone unasked.
	if spec.R != 0 && (spec.V != 2 || spec.R != 1) {
		return calendarSpec{}, false
	}
	if len(spec.G) > 0 {
		// Six is the picker's cap, and the id grammar is what keeps a token from carrying a path or a quote.
		if spec.V != 2 || len(spec.G) > 6 {
			return calendarSpec{}, false
		}
		for _, id := range spec.G {
			if len(id) == 0 || len(id) > 24 || !validGenreID(id) {
				return calendarSpec{}, false
			}
		}
	}
	return spec, true
}

func validDay(day string) bool {
	if len(day) != 10 || day[4] != '-' || day[7] != '-' {
		return false
	}
	parsed, err := time.Parse(calendarDay, day)
	if err != nil {
		return false
	}
	return parsed.Format(calendarDay) == day
}

// buildCalendar renders the feed. Text is escaped and folded here rather than by a library, because the two runtimes
// have to agree byte for byte and a dependency's folding rules would be a third opinion.
func buildCalendar(rows []feedRow, name, description string, builtAt time.Time, reminder bool) string {
	// The caller supplies the clock: the manifest's build time, never this process's. A feed whose DTSTAMP moved on
	// every fetch would make every subscription look edited to a client that compares them.
	stamp := builtAt.UTC().Format("20060102T150405Z")
	lines := []string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//Messer//Lineup//EN",
		"CALSCALE:GREGORIAN",
		"METHOD:PUBLISH",
		"X-WR-CALNAME:" + escapeText(name),
		"X-WR-CALDESC:" + escapeText(description),
		"REFRESH-INTERVAL;VALUE=DURATION:PT6H",
		"X-PUBLISHED-TTL:PT6H",
	}
	for _, item := range rows {
		row := item.Row
		start, err := time.Parse(time.RFC3339, row.D)
		if err != nil {
			continue
		}
		status := "CONFIRMED"
		if string(row.St) == "c" {
			status = "CANCELLED"
		}
		location := row.V
		if item.Town != "" {
			location = row.V + ", " + item.Town
		}
		summary := row.A + " @ " + row.V
		lines = append(lines,
			"BEGIN:VEVENT",
			"UID:"+escapeText(row.K+"@lineup.messer.wtf"),
			"DTSTAMP:"+stamp,
			"DTSTART:"+start.UTC().Format("20060102T150405Z"),
			"DTEND:"+start.Add(calendarDuration).UTC().Format("20060102T150405Z"),
			"SUMMARY:"+escapeText(summary),
			"LOCATION:"+escapeText(location),
			"URL:"+row.U,
			"STATUS:"+status,
			"CATEGORIES:"+escapeText("Messer Lineup"),
			"TRANSP:OPAQUE",
		)
		if reminder {
			// A display alarm the day before. Some clients ignore VALARM on a subscribed calendar, which is why it is a
			// request in the token and never a promise on the page.
			lines = append(lines,
				"BEGIN:VALARM",
				"ACTION:DISPLAY",
				"DESCRIPTION:"+escapeText(summary),
				"TRIGGER:-P1D",
				"END:VALARM",
			)
		}
		lines = append(lines, "END:VEVENT")
	}
	lines = append(lines, "END:VCALENDAR")

	var b strings.Builder
	for _, line := range lines {
		b.WriteString(foldLine(line))
		b.WriteString("\r\n")
	}
	return b.String()
}

// escapeText is RFC 5545 §3.3.11: backslash first, then the separators, and a newline becomes a literal `\n`.
func escapeText(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, ";", `\;`)
	value = strings.ReplaceAll(value, ",", `\,`)
	value = strings.ReplaceAll(value, "\r\n", `\n`)
	return strings.ReplaceAll(value, "\n", `\n`)
}

// foldLine is RFC 5545 §3.1: physical lines are at most 75 octets and a continuation starts with one space. It folds
// on rune boundaries so a multi-byte character is never split.
func foldLine(line string) string {
	if len(line) <= 75 {
		return line
	}
	var b strings.Builder
	size := 0
	for _, r := range line {
		width := utf8.RuneLen(r)
		if size+width > 74 {
			b.WriteString("\r\n ")
			size = 0
		}
		b.WriteRune(r)
		size += width
	}
	return b.String()
}
