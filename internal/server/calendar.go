package server

// The calendar feed, in the dev server.
//
// `/calendar/<token>.ics` is the one route that renders rather than serves, and it exists twice — here and in
// `src/worker.ts` — because the contract (`scripts/contract.mjs`) is the promise that the two runtimes answer the
// same way. The rules are therefore written twice on purpose and held together by that test:
//
//   - the token is base64url of `{v,t,f,o,a?}`, unsigned (see `src/calendar/spec.ts` for why a signature would buy
//     nothing here);
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

	"github.com/jakobbjelver/messer-lineup/internal/index"
	"github.com/jakobbjelver/messer-lineup/internal/model"
)

// calendarDuration is the block a client draws: the index knows a start and nothing about an end.
const calendarDuration = 3 * time.Hour

type calendarSpec struct {
	V int      `json:"v"`
	T string   `json:"t"`
	F string   `json:"f"`
	O string   `json:"o"`
	A []string `json:"a,omitempty"`
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

	entry, found := s.townEntry(spec.T)
	if !found {
		writeError(w, http.StatusNotFound, "unknown_town", fmt.Sprintf("this index does not publish %q", spec.T))
		return
	}
	rows, err := index.ReadShard(filepath.Join(s.opts.IndexDir, filepath.FromSlash(entry.Shard)))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "shard_unreadable", err.Error())
		return
	}

	artists := map[string]bool{}
	for _, key := range spec.A {
		artists[key] = true
	}
	kept := make([]model.Row, 0, len(rows))
	for _, row := range rows {
		day := row.D
		if len(day) > 10 {
			day = day[:10]
		}
		if day < spec.F || day > spec.O {
			continue
		}
		if len(artists) > 0 && !artists[model.Key(row.A)] {
			continue
		}
		kept = append(kept, row)
	}
	sort.SliceStable(kept, func(i, j int) bool {
		if kept[i].D != kept[j].D {
			return kept[i].D < kept[j].D
		}
		return kept[i].A < kept[j].A
	})

	body := buildCalendar(kept, s.townLabel(spec.T), s.manifest.BuiltAt)
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `inline; filename="lineup-`+spec.T+`.ics"`)
	// Fifteen minutes: short enough to follow a nightly publish, long enough that a mail client polling twice an
	// hour does not reach the disk.
	w.Header().Set("Cache-Control", "public, max-age=900")
	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write([]byte(body))
}

func (s *server) townEntry(id string) (model.Index, bool) {
	for _, entry := range s.manifest.Towns {
		if entry.Town == id {
			return entry, true
		}
	}
	return model.Index{}, false
}

// townLabel is the shortest name the town was seen under, the same rule the index applies: platforms name districts
// as often as cities, and "København Ø" is a district of "København".
func (s *server) townLabel(id string) string {
	blob, err := os.ReadFile(filepath.Join(s.opts.IndexDir, "towns.json"))
	if err != nil {
		return id
	}
	var towns model.Towns
	if err := json.Unmarshal(blob, &towns); err != nil {
		return id
	}
	best := ""
	for _, town := range towns.Towns {
		if town.ID != id || town.Name == "" {
			continue
		}
		if best == "" || len(town.Name) < len(best) {
			best = town.Name
		}
	}
	if best == "" {
		return id
	}
	return best
}

// decodeCalendarSpec is strict on purpose: a token that is nearly valid is a link that will silently show the wrong
// board, so anything unexpected is refused rather than defaulted.
func decodeCalendarSpec(token string) (calendarSpec, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(token, "="))
	if err != nil {
		return calendarSpec{}, false
	}
	var spec calendarSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return calendarSpec{}, false
	}
	if spec.V != 1 || spec.T == "" || len(spec.T) > 64 {
		return calendarSpec{}, false
	}
	if !validDay(spec.F) || !validDay(spec.O) || spec.F > spec.O {
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
	return spec, true
}

func validDay(day string) bool {
	if len(day) != 10 || day[4] != '-' || day[7] != '-' {
		return false
	}
	parsed, err := time.Parse("2006-01-02", day)
	if err != nil {
		return false
	}
	return parsed.Format("2006-01-02") == day
}

// buildCalendar renders the feed. Text is escaped and folded here rather than by a library, because the two runtimes
// have to agree byte for byte and a dependency's folding rules would be a third opinion.
func buildCalendar(rows []model.Row, townLabel string, builtAt time.Time) string {
	stamp := builtAt.UTC().Format("20060102T150405Z")
	if builtAt.IsZero() {
		stamp = time.Now().UTC().Format("20060102T150405Z")
	}
	lines := []string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//Messer//Lineup//EN",
		"CALSCALE:GREGORIAN",
		"METHOD:PUBLISH",
		"X-WR-CALNAME:" + escapeText("Messer Lineup — "+townLabel),
		"X-WR-CALDESC:" + escapeText("Shows in "+townLabel+", from the Messer Lineup index."),
		"REFRESH-INTERVAL;VALUE=DURATION:PT6H",
		"X-PUBLISHED-TTL:PT6H",
	}
	for _, row := range rows {
		start, err := time.Parse(time.RFC3339, row.D)
		if err != nil {
			continue
		}
		status := "CONFIRMED"
		if string(row.St) == "c" {
			status = "CANCELLED"
		}
		lines = append(lines,
			"BEGIN:VEVENT",
			"UID:"+escapeText(row.K+"@lineup.messer.wtf"),
			"DTSTAMP:"+stamp,
			"DTSTART:"+start.UTC().Format("20060102T150405Z"),
			"DTEND:"+start.Add(calendarDuration).UTC().Format("20060102T150405Z"),
			"SUMMARY:"+escapeText(row.A+" @ "+row.V),
			"LOCATION:"+escapeText(row.V+", "+townLabel),
			"URL:"+row.U,
			"STATUS:"+status,
			"CATEGORIES:"+escapeText("Messer Lineup"),
			"TRANSP:OPAQUE",
			"END:VEVENT",
		)
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
