package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/model"
)

// fixedNow is the instant the fixture index was built around; tests that build their own handler pass it so a
// cached answer and a manifest stamp are reproducible.
func fixedNow() time.Time { return time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC) }

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
	rows := []feedRow{{
		Row: model.Row{
			K: "a|b|2026-12-02", A: strings.Repeat("ø", 60), V: "Pumpehuset", D: "2026-12-02T20:00:00+01:00", U: "https://t.test/1",
		},
		Town: "København",
	}}
	body := buildCalendar(rows, "Messer Lineup — København", "Shows in København, from the Messer Lineup index.", time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC))
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

// A subscription outlives the day it was made, so a token with no window must follow the published one. This is the
// defect that made every feed die: a token whose window had passed answered 0 events while the index held 718.
func TestCalendarWithoutAWindowFollowsThePublishedOne(t *testing.T) {
	handler := newServer(t, buildIndex(t))
	rec := get(t, handler, "/calendar/"+calendarToken(t, calendarSpec{V: 1, T: "copenhagen-dk"})+".ics")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := strings.Count(rec.Body.String(), "BEGIN:VEVENT"); got != 2 {
		t.Fatalf("events = %d, want the 2 Copenhagen shows inside the published window:\n%s", got, rec.Body.String())
	}
}

// Anywhere is a scope, not a town: every shard, and each event labelled with the town it is actually in.
func TestCalendarAnywhereSpansTownsAndLabelsEachEvent(t *testing.T) {
	handler := newServer(t, buildIndex(t))
	rec := get(t, handler, "/calendar/"+calendarToken(t, calendarSpec{V: 1, T: "anywhere"})+".ics")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if got := strings.Count(body, "BEGIN:VEVENT"); got != 3 {
		t.Fatalf("events = %d, want 3 (two towns):\n%s", got, body)
	}
	if !strings.Contains(body, "X-WR-CALNAME:Messer Lineup — Anywhere") {
		t.Fatalf("the feed is not titled for the scope:\n%s", body)
	}
	// The label is the row's own town. "Debaser, Anywhere" is not a place.
	if !strings.Contains(body, "LOCATION:Vega\\, København") || !strings.Contains(body, "LOCATION:Vega\\, Stockholm") {
		t.Fatalf("an anywhere feed did not label each event with its own town:\n%s", body)
	}
}

// The live source is a convenience with a floor under it: when the provider does not answer, the snapshot in the
// token is what the subscriber already saw.
func TestCalendarLiveSourceFallsBackToTheSnapshot(t *testing.T) {
	answered := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/rob/artists") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"payload":{"artists":[{"artist_name":"Efterklang"}]}}`))
	}))
	defer answered.Close()
	silent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer silent.Close()

	snapshot := []string{model.Key("Anna von Hausswolff"), model.Key("Efterklang")}
	spec := calendarSpec{V: 1, T: "copenhagen-dk", A: snapshot, S: &calendarSource{K: "listenbrainz", U: "rob"}}

	// The provider answers: its list wins, and the snapshot is not used.
	handler, err := Handler(Options{IndexDir: buildIndex(t), Now: fixedNow, ListenBrainzURL: answered.URL + "/"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	body := get(t, handler, "/calendar/"+calendarToken(t, spec)+".ics").Body.String()
	if got := strings.Count(body, "BEGIN:VEVENT"); got != 1 {
		t.Fatalf("events = %d, want 1 (the provider's one artist):\n%s", got, body)
	}
	if !strings.Contains(body, "SUMMARY:Efterklang @ Pumpehuset") {
		t.Fatalf("the live list did not filter to its own artist:\n%s", body)
	}

	// The provider is gone: the snapshot is the answer, so the calendar keeps working.
	handler, err = Handler(Options{IndexDir: buildIndex(t), Now: fixedNow, ListenBrainzURL: silent.URL + "/"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	body = get(t, handler, "/calendar/"+calendarToken(t, spec)+".ics").Body.String()
	if got := strings.Count(body, "BEGIN:VEVENT"); got != 2 {
		t.Fatalf("events = %d, want the 2 in the snapshot after the source failed:\n%s", got, body)
	}
}

// A source with no snapshot must never widen into the whole town: the subscriber asked for artists.
func TestCalendarLiveSourceWithoutASnapshotIsNotTheWholeTown(t *testing.T) {
	silent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer silent.Close()
	handler, err := Handler(Options{IndexDir: buildIndex(t), Now: fixedNow, ListenBrainzURL: silent.URL + "/"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	spec := calendarSpec{V: 1, T: "copenhagen-dk", S: &calendarSource{K: "listenbrainz", U: "rob"}}
	body := get(t, handler, "/calendar/"+calendarToken(t, spec)+".ics").Body.String()
	if got := strings.Count(body, "BEGIN:VEVENT"); got != 0 {
		t.Fatalf("events = %d, want 0: an unresolved source is not permission to show everything:\n%s", got, body)
	}
	if !strings.Contains(body, "BEGIN:VCALENDAR") {
		t.Fatalf("an unresolved source must still answer a calendar:\n%s", body)
	}
}

func TestCalendarRefusesHalfAWindowAndUnknownSources(t *testing.T) {
	handler := newServer(t, buildIndex(t))
	cases := []calendarSpec{
		{V: 1, T: "copenhagen-dk", F: "2026-11-01"},                              // half a window
		{V: 1, T: "copenhagen-dk", O: "2027-01-31"},                              // the other half
		{V: 1, T: "copenhagen-dk", S: &calendarSource{K: "lastfm", U: "rob"}},    // a source nobody implements
		{V: 1, T: "copenhagen-dk", S: &calendarSource{K: "listenbrainz", U: ""}}, // a source with no handle
	}
	for _, spec := range cases {
		rec := get(t, handler, "/calendar/"+calendarToken(t, spec)+".ics")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%+v = %d, want 400: %s", spec, rec.Code, rec.Body.String())
		}
	}
}
