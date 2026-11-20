package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// The upstream is a local server: the shape the picker parses, a low score that must be dropped, and a UA the
// real MusicBrainz would refuse to serve without.
func TestArtistSearchMapsAndCaches(t *testing.T) {
	var calls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if ua := r.Header.Get("User-Agent"); ua == "" || ua == "Go-http-client/1.1" {
			t.Errorf("upstream saw a bare user agent: %q", ua)
		}
		if got := r.URL.Query().Get("query"); got != "björk" {
			t.Errorf("upstream query = %q, want björk", got)
		}
		w.Header().Set("content-type", "application/json")
		fmt.Fprint(w, `{"artists":[
			{"id":"87c5dedd","name":"Björk","type":"Person","country":"IS","score":100},
			{"id":"low-score","name":"Someone","score":12},
			{"name":"Nameless","score":90}
		]}`)
	}))
	defer upstream.Close()

	searcher := newArtistSearcher(upstream.URL)
	searcher.gap = 0

	first := searcher.lookup("björk")
	if len(first.Artists) != 1 {
		t.Fatalf("hits = %d, want 1 (the low score is dropped)", len(first.Artists))
	}
	if got := first.Artists[0]; got.Name != "Björk" || got.MBID != "87c5dedd" || got.Type != "person" {
		t.Fatalf("hit = %+v", got)
	}
	if first.Unavailable {
		t.Fatal("a good answer is not unavailable")
	}

	second := searcher.lookup("björk")
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("upstream calls = %d, want 1: the second lookup must come from the cache", calls)
	}
	if len(second.Artists) != 1 || second.Artists[0].Name != "Björk" {
		t.Fatalf("cached answer = %+v", second)
	}
}

func TestArtistSearchUnavailableIsAnAnswer(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "busy", http.StatusServiceUnavailable)
	}))
	defer upstream.Close()

	searcher := newArtistSearcher(upstream.URL)
	searcher.gap = 0
	answer := searcher.lookup("anything")
	if !answer.Unavailable || answer.Artists == nil || len(answer.Artists) != 0 {
		t.Fatalf("a 503 must be an empty, unavailable answer: %+v", answer)
	}
}

func TestArtistSearchShortQueryDoesNotCallUpstream(t *testing.T) {
	var calls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
	}))
	defer upstream.Close()

	searcher := newArtistSearcher(upstream.URL)
	searcher.gap = 0
	if got := len(searcher.lookup("a").Artists); got != 0 {
		t.Fatalf("a one-letter query returned %d hits", got)
	}
	time.Sleep(10 * time.Millisecond)
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("a one-letter query reached upstream %d times", calls)
	}
}
