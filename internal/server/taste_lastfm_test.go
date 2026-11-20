package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// lastfmFixture is a Last.fm that answers from a fixture and counts what it was asked.
func lastfmFixture(t *testing.T, body string, status int) (*httptest.Server, *int64, *string) {
	t.Helper()
	var calls int64
	var lastQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&calls, 1)
		lastQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls, &lastQuery
}

func newLastFMServer(t *testing.T, endpoint, key string) http.Handler {
	t.Helper()
	handler, err := Handler(Options{
		IndexDir:  buildIndex(t),
		Now:       func() time.Time { return time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC) },
		LastFMKey: key,
		LastFMURL: endpoint,
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	return handler
}

// The route's body is names in the provider's own order, deduped by identity key — the currency the picker speaks.
func TestLastFMTasteAnswersNames(t *testing.T) {
	fixture := `{"topartists":{"artist":[{"name":"Opeth"},{"name":"Opeth"},{"name":" Meshuggah "},{"name":""}]}}`
	srv, calls, _ := lastfmFixture(t, fixture, http.StatusOK)
	handler := newLastFMServer(t, srv.URL, "test-key")

	rec := get(t, handler, "/api/v1/taste/lastfm?user=messeratgalten")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Artists []string `json:"artists"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	if len(body.Artists) != 2 || body.Artists[0] != "Opeth" || body.Artists[1] != "Meshuggah" {
		t.Fatalf("artists = %v", body.Artists)
	}
	if cache := rec.Header().Get("Cache-Control"); !strings.Contains(cache, "max-age=21600") {
		t.Fatalf("cache-control = %q", cache)
	}
	// A second request in the same window is answered from the resolver: one upstream call, not two.
	get(t, handler, "/api/v1/taste/lastfm?user=messeratgalten")
	if got := atomic.LoadInt64(calls); got != 1 {
		t.Fatalf("upstream calls = %d, want 1", got)
	}
}

// Last.fm reports a missing user in the body **on a 404** (`{"error":6}`), so a status-only client calls it an
// outage and a body-only client calls a broken response a missing account. Both halves have to be read.
func TestLastFMUnknownUserIsA404AndIsRemembered(t *testing.T) {
	srv, calls, _ := lastfmFixture(t, `{"error":6,"message":"User not found"}`, http.StatusNotFound)
	handler := newLastFMServer(t, srv.URL, "test-key")

	rec := get(t, handler, "/api/v1/taste/lastfm?user=nobody")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error.Code != "unknown_user" {
		t.Fatalf("envelope = %s", rec.Body.String())
	}
	get(t, handler, "/api/v1/taste/lastfm?user=nobody")
	if got := atomic.LoadInt64(calls); got != 1 {
		t.Fatalf("a missing user was asked for again: %d calls", got)
	}
}

// A server without a key says so and never calls the provider: an outage message for a missing setting is a lie.
func TestLastFMWithoutAKeyIsNotConfigured(t *testing.T) {
	srv, calls, _ := lastfmFixture(t, `{"topartists":{"artist":[]}}`, http.StatusOK)
	handler := newLastFMServer(t, srv.URL, "")

	rec := get(t, handler, "/api/v1/taste/lastfm?user=someone")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not_configured") {
		t.Fatalf("envelope = %s", rec.Body.String())
	}
	if got := atomic.LoadInt64(calls); got != 0 {
		t.Fatalf("upstream calls = %d, want 0", got)
	}
}

// A provider that failed is a moment: 503, `no-store`, and nothing remembered for six hours.
func TestLastFMFailureIsNotCachedAsAFact(t *testing.T) {
	srv, calls, _ := lastfmFixture(t, `{"error":29,"message":"Rate limit exceeded"}`, http.StatusOK)
	handler := newLastFMServer(t, srv.URL, "test-key")

	rec := get(t, handler, "/api/v1/taste/lastfm?user=someone")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if cache := rec.Header().Get("Cache-Control"); cache != "no-store" {
		t.Fatalf("cache-control = %q, want no-store", cache)
	}
	if !strings.Contains(rec.Body.String(), "provider_unavailable") {
		t.Fatalf("envelope = %s", rec.Body.String())
	}
	if got := atomic.LoadInt64(calls); got != 1 {
		t.Fatalf("upstream calls = %d, want 1", got)
	}
}

// The request itself is the contract with the provider: method, handle, key, JSON, the cap and the period.
func TestLastFMAsksForTheYearWindowAndTheCap(t *testing.T) {
	srv, _, query := lastfmFixture(t, `{"topartists":{"artist":[]}}`, http.StatusOK)
	handler := newLastFMServer(t, srv.URL, "test-key")

	if rec := get(t, handler, "/api/v1/taste/lastfm?user=someone"); rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{"method=user.getTopArtists", "user=someone", "api_key=test-key", "format=json", "limit=200", "period=12month"} {
		if !strings.Contains(*query, want) {
			t.Fatalf("query %q is missing %q", *query, want)
		}
	}
}

// An empty handle is a client error, not a provider call.
func TestLastFMWithoutAUserIsA400(t *testing.T) {
	srv, calls, _ := lastfmFixture(t, `{"topartists":{"artist":[]}}`, http.StatusOK)
	handler := newLastFMServer(t, srv.URL, "test-key")

	if rec := get(t, handler, "/api/v1/taste/lastfm"); rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", rec.Code)
	}
	if got := atomic.LoadInt64(calls); got != 0 {
		t.Fatalf("upstream calls = %d, want 0", got)
	}
}
