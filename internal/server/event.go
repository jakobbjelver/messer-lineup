package server

// One show, as a calendar file.
//
// A subscribed feed has to read the index, because a week of a town cannot fit in a URL. A single event can: the row is
// nine short strings, so the event travels in the token and this route reads nothing at all — no manifest, no shard, no
// town lookup. The `VEVENT` it writes is the same one `buildCalendar` would write for that row, with the same `UID`, so a
// show taken from a row and the same show arriving through a subscription are one event to the client, not two.

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/model"

	"github.com/jakobbjelver/messer-lineup/internal/clock"
)

type eventSpec struct {
	V  int    `json:"v"`
	K  string `json:"k"`
	A  string `json:"a"`
	V2 string `json:"v2"`
	D  string `json:"d"`
	U  string `json:"u"`
	N  string `json:"n"`
}

func (s *server) event(w http.ResponseWriter, r *http.Request) {
	file := strings.TrimPrefix(r.URL.Path, "/event/")
	if !strings.HasSuffix(file, ".ics") {
		writeError(w, http.StatusNotFound, "not_found", "a single event is /event/<token>.ics")
		return
	}
	spec, ok := decodeEventSpec(strings.TrimSuffix(file, ".ics"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_event", "that link is not a show")
		return
	}
	row := model.Row{
		K:  spec.K,
		A:  spec.A,
		V:  spec.V2,
		D:  spec.D,
		U:  spec.U,
		St: model.Status("o"),
	}
	ics := buildEvent(row, spec.N, clock.Now())

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.TrimSuffix(file, ".ics")+`.ics"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(ics))
}

func decodeEventSpec(token string) (eventSpec, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(token, "="))
	if err != nil {
		return eventSpec{}, false
	}
	var spec eventSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return eventSpec{}, false
	}
	if spec.V != 1 {
		return eventSpec{}, false
	}
	for _, field := range []struct {
		value string
		max   int
	}{{spec.K, 128}, {spec.A, 160}, {spec.V2, 160}, {spec.N, 80}, {spec.U, 512}} {
		if field.value == "" || len(field.value) > field.max {
			return eventSpec{}, false
		}
	}
	if !strings.HasPrefix(spec.U, "http://") && !strings.HasPrefix(spec.U, "https://") {
		return eventSpec{}, false
	}
	if _, err := time.Parse(time.RFC3339, spec.D); err != nil {
		return eventSpec{}, false
	}
	return spec, true
}

// buildEvent writes the same VEVENT buildCalendar writes, without the feed's calendar-level metadata.
func buildEvent(row model.Row, townLabel string, now time.Time) string {
	lines := []string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//Messer//Lineup//EN",
		"CALSCALE:GREGORIAN",
		"METHOD:PUBLISH",
	}
	if start, err := time.Parse(time.RFC3339, row.D); err == nil {
		status := "CONFIRMED"
		if string(row.St) == "c" {
			status = "CANCELLED"
		}
		lines = append(lines,
			"BEGIN:VEVENT",
			"UID:"+escapeText(row.K+"@lineup.messer.wtf"),
			"DTSTAMP:"+now.UTC().Format("20060102T150405Z"),
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
