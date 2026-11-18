package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/model"
)

// calendarToken builds the token the client builds: base64url of a small JSON object.
func calendarToken(t *testing.T, spec calendarSpec) string {
	t.Helper()
	blob, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(blob)
}

func TestCalendarFeedRendersTheBoard(t *testing.T) {
	handler := newServer(t, buildIndex(t))
	token := calendarToken(t, calendarSpec{V: 1, T: "copenhagen-dk", F: "2026-11-01", O: "2027-01-31"})
	rec := get(t, handler, "/calendar/"+token+".ics")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/calendar") {
		t.Fatalf("content-type = %q", got)
	}
	body := rec.Body.String()
	if !strings.HasSuffix(body, "END:VCALENDAR\r\n") {
		t.Fatalf("feed does not end with a CRLF: %q", tail(body, 40))
	}
	if got := strings.Count(body, "BEGIN:VEVENT"); got != 2 {
		t.Fatalf("events = %d, want 2 (the two Copenhagen shows):\n%s", got, body)
	}
	for _, want := range []string{
		"BEGIN:VCALENDAR",
		"X-WR-CALNAME:Messer Lineup — København",
		"DTSTART:20261202T200000Z", // the venue offset is folded into UTC
		"DTEND:20261202T230000Z",
		"SUMMARY:Anna von Hausswolff @ Vega",
		"LOCATION:Vega\\, København",
		"STATUS:CONFIRMED",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("feed is missing %q:\n%s", want, body)
		}
	}
	// Every physical line obeys RFC 5545 §3.1, including the folded ones.
	for _, line := range strings.Split(strings.TrimSuffix(body, "\r\n"), "\r\n") {
		if len(line) > 75 {
			t.Fatalf("line of %d octets over the 75 limit: %q", len(line), line)
		}
	}
}

func TestCalendarArtistFilterAndUIDs(t *testing.T) {
	handler := newServer(t, buildIndex(t))
	spec := calendarSpec{V: 1, T: "copenhagen-dk", F: "2026-11-01", O: "2027-01-31", A: []string{model.Key("Efterklang")}}
	rec := get(t, handler, "/calendar/"+calendarToken(t, spec)+".ics")
	body := rec.Body.String()
	if got := strings.Count(body, "BEGIN:VEVENT"); got != 1 {
		t.Fatalf("events = %d, want 1 after the artist filter:\n%s", got, body)
	}
	if !strings.Contains(body, "SUMMARY:Efterklang @ Pumpehuset") {
		t.Fatalf("wrong artist kept:\n%s", body)
	}
	// The UID is the row key plus the host: stable across sweeps, which is what stops a re-subscribe duplicating.
	if !strings.Contains(body, "UID:"+model.RowKey(model.Key("Efterklang"), model.Key("Pumpehuset"), time.Date(2026, 12, 2, 20, 0, 0, 0, time.UTC))) {
		t.Fatalf("UID is not the row key:\n%s", body)
	}
}

func TestCalendarIsValidWithoutAnyEvents(t *testing.T) {
	handler := newServer(t, buildIndex(t))
	token := calendarToken(t, calendarSpec{V: 1, T: "copenhagen-dk", F: "2027-02-01", O: "2027-02-10"})
	rec := get(t, handler, "/calendar/"+token+".ics")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for an empty window", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "BEGIN:VEVENT") {
		t.Fatalf("an empty window produced events:\n%s", body)
	}
	if !strings.Contains(body, "BEGIN:VCALENDAR") || !strings.HasSuffix(body, "END:VCALENDAR\r\n") {
		t.Fatalf("an empty window must still be a calendar:\n%s", body)
	}
}

func TestCalendarRefusesWhatItCannotHonour(t *testing.T) {
	handler := newServer(t, buildIndex(t))
	cases := []struct {
		path string
		code int
		want string
	}{
		{"/calendar/not-base64!!.ics", http.StatusBadRequest, "invalid_calendar"},
		{"/calendar/" + calendarToken(t, calendarSpec{V: 1, T: "atlantis-dk", F: "2026-11-01", O: "2027-01-31"}) + ".ics", http.StatusNotFound, "unknown_town"},
		{"/calendar/" + calendarToken(t, calendarSpec{V: 1, T: "copenhagen-dk", F: "2027-01-31", O: "2026-11-01"}) + ".ics", http.StatusBadRequest, "invalid_calendar"},
		{"/calendar/" + calendarToken(t, calendarSpec{V: 9, T: "copenhagen-dk", F: "2026-11-01", O: "2027-01-31"}) + ".ics", http.StatusBadRequest, "invalid_calendar"},
		{"/calendar/taste-abc.ics", http.StatusBadRequest, "invalid_calendar"},
	}
	for _, tc := range cases {
		rec := get(t, handler, tc.path)
		if rec.Code != tc.code {
			t.Fatalf("%s = %d, want %d", tc.path, rec.Code, tc.code)
		}
		var body struct {
			Error struct{ Code, Message string }
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error.Code != tc.want {
			t.Fatalf("%s: envelope = %s", tc.path, rec.Body.String())
		}
	}
}

func TestCalendarFoldsLongValuesOnRuneBoundaries(t *testing.T) {
	rows := []model.Row{{
		K: "a|b|2026-12-02", A: strings.Repeat("ø", 60), V: "Pumpehuset", D: "2026-12-02T20:00:00+01:00", U: "https://t.test/1",
	}}
	body := buildCalendar(rows, "København", time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC))
	for _, line := range strings.Split(strings.TrimSuffix(body, "\r\n"), "\r\n") {
		if len(line) > 75 {
			t.Fatalf("line of %d octets over the limit: %q", len(line), line)
		}
		if !strings.Contains(line, "ø") && strings.Contains(line, "\xc3") && !strings.HasPrefix(line, " ") {
			t.Fatalf("a continuation line lost its space: %q", line)
		}
	}
	// Folded back together, the summary is whole: no rune was split across the fold.
	unfolded := strings.ReplaceAll(body, "\r\n ", "")
	if !strings.Contains(unfolded, "SUMMARY:"+strings.Repeat("ø", 60)+" @ Pumpehuset") {
		t.Fatalf("the folded summary did not reassemble:\n%s", body)
	}
}

func tail(value string, n int) string {
	if len(value) <= n {
		return value
	}
	return value[len(value)-n:]
}
