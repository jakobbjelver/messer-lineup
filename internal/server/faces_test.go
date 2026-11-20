package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// deezerServer answers one artist search per name, from a fixed table.
func deezerServer(t *testing.T, table map[string]string, calls *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(calls, 1)
		name := r.URL.Query().Get("q")
		picture, ok := table[name]
		w.Header().Set("content-type", "application/json")
		if !ok {
			fmt.Fprint(w, `{"data":[]}`)
			return
		}
		fmt.Fprintf(w, `{"data":[{"id":42,"name":%q,"picture_xl":%q}]}`, name, picture)
	}))
}

// portraitsHandler wires the route to a fixture index and a fixture Deezer.
func portraitsHandler(t *testing.T, deezerURL string, now func() time.Time) http.Handler {
	t.Helper()
	handler, err := Handler(Options{IndexDir: buildIndex(t), Now: now, DeezerURL: deezerURL})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	return handler
}

func getFaces(t *testing.T, handler http.Handler, query string) faceAnswer {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/artists/portraits"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var body faceAnswer
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

func TestArtistPortraitsAnswersAndCaches(t *testing.T) {
	var calls int32
	upstream := deezerServer(t, map[string]string{"Opeth": "https://cdn-images.dzcdn.net/opeth/1000x1000.jpg"}, &calls)
	defer upstream.Close()

	handler := portraitsHandler(t, upstream.URL+"?q=", nil)
	body := getFaces(t, handler, "?n=Opeth")
	portrait, ok := body.Portraits["Opeth"]
	if !ok || portrait.URL == "" {
		t.Fatalf("portraits = %+v, want one face for Opeth", body.Portraits)
	}
	if body.Unavailable {
		t.Fatal("a good answer is not unavailable")
	}

	// A second ask is a cache hit: the picker re-opens, Deezer does not get called again.
	getFaces(t, handler, "?n=Opeth")
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("upstream calls = %d, want 1", got)
	}
}

// The identity rule, at the route: Deezer answers a namesake with a picture, and the picker must keep initials.
func TestArtistPortraitsRefusesANamesake(t *testing.T) {
	var calls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("content-type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":7,"name":"Fatherland","picture_xl":"https://cdn-images.dzcdn.net/fatherland.jpg"}]}`)
	}))
	defer upstream.Close()

	handler := portraitsHandler(t, upstream.URL+"?q=", nil)
	body := getFaces(t, handler, "?n=Fatherland%20Choir")
	if len(body.Portraits) != 0 {
		t.Fatalf("portraits = %+v, want none: a different Key is a different artist", body.Portraits)
	}
}

// A miss is a fact and is remembered; a failure is neither remembered nor claimed.
func TestArtistPortraitsForgetsAFailure(t *testing.T) {
	var calls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "busy", http.StatusServiceUnavailable)
	}))
	defer upstream.Close()

	handler := portraitsHandler(t, upstream.URL+"?q=", nil)
	first := getFaces(t, handler, "?n=Opeth")
	if !first.Unavailable || len(first.Portraits) != 0 {
		t.Fatalf("first ask = %+v, want an unavailable, empty answer", first)
	}
	second := getFaces(t, handler, "?n=Opeth")
	if !second.Unavailable {
		t.Fatal("a failure must not be cached as a miss")
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("upstream calls = %d, want 2 (nothing cached)", got)
	}
}

// No names is an empty answer with no upstream call: the picker asks on every keystroke worth asking about.
func TestArtistPortraitsWithNoNames(t *testing.T) {
	var calls int32
	upstream := deezerServer(t, nil, &calls)
	defer upstream.Close()

	handler := portraitsHandler(t, upstream.URL+"?q=", nil)
	body := getFaces(t, handler, "")
	if len(body.Portraits) != 0 || body.Unavailable {
		t.Fatalf("body = %+v, want an empty answer", body)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("upstream calls = %d, want 0", calls)
	}
}
