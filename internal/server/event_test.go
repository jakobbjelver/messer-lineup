package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func eventToken(t *testing.T, spec map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func TestOneShowIsACalendarFile(t *testing.T) {
	handler := newServer(t, buildIndex(t))
	token := eventToken(t, map[string]any{
		"v": 1, "k": "opeth|pumpehuset|2027-02-13", "a": "Opeth", "v2": "Pumpehuset",
		"d": "2027-02-13T20:00:00+01:00", "u": "https://www.tickster.com/dk/da/events/abc/2027-02-13/opeth-pumpehuset",
		"n": "København",
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/event/"+token+".ics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/calendar") {
		t.Fatalf("content-type = %q", got)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"BEGIN:VCALENDAR",
		"BEGIN:VEVENT",
		"UID:opeth|pumpehuset|2027-02-13@lineup.messer.wtf",
		"DTSTART:20270213T190000Z", // 20:00 +01:00, in UTC
		"DTEND:20270213T220000Z",   // three hours after, never invented as a running time
		"SUMMARY:Opeth @ Pumpehuset",
		"LOCATION:Pumpehuset\\, København",
		"END:VEVENT",
		"END:VCALENDAR",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body is missing %q:\n%s", want, body)
		}
	}
	// One event, not a feed: no calendar-level name to overwrite the reader's.
	if strings.Contains(body, "X-WR-CALNAME") {
		t.Fatalf("a single event carried a calendar name:\n%s", body)
	}
	// CRLF throughout, and folded lines are continuations, not new properties.
	if strings.Contains(strings.ReplaceAll(body, "\r\n", ""), "\n") {
		t.Fatal("a bare LF survived into the file")
	}
}

func TestOneShowRefusesWhatIsNotAShow(t *testing.T) {
	handler := newServer(t, buildIndex(t))
	cases := map[string]string{
		"garbage":        "not-base64-!",
		"wrong version":  eventToken(t, map[string]any{"v": 2, "k": "k", "a": "a", "v2": "v", "d": "2027-02-13T20:00:00Z", "u": "https://x.test", "n": "n"}),
		"no artist":      eventToken(t, map[string]any{"v": 1, "k": "k", "a": "", "v2": "v", "d": "2027-02-13T20:00:00Z", "u": "https://x.test", "n": "n"}),
		"bad date":       eventToken(t, map[string]any{"v": 1, "k": "k", "a": "a", "v2": "v", "d": "13 February", "u": "https://x.test", "n": "n"}),
		"not a web link": eventToken(t, map[string]any{"v": 1, "k": "k", "a": "a", "v2": "v", "d": "2027-02-13T20:00:00Z", "u": "javascript:alert(1)", "n": "n"}),
		"no town label":  eventToken(t, map[string]any{"v": 1, "k": "k", "a": "a", "v2": "v", "d": "2027-02-13T20:00:00Z", "u": "https://x.test", "n": ""}),
	}
	for name, token := range cases {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/event/"+token+".ics", nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", name, rec.Code)
		}
	}

	// And a file that is not an .ics is not this route's business.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/event/abc.txt", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf(".txt = %d, want 404", rec.Code)
	}
}
