package resolve

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeezerMatchesTheExactNameOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Deezer answers a query with its best guesses; the reader must not accept a guess.
		fmt.Fprint(w, `{"data":[{"name":"Aura Onda"}]}`)
	}))
	defer server.Close()

	matcher := &Deezer{Client: server.Client(), BaseURL: server.URL, Gap: time.Millisecond}
	ok, err := matcher.Matches(context.Background(), "Aural Onsdag")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("a namesake was accepted as the name")
	}
}

func TestDeezerAcceptsAFoldedName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"name":"Mötley Crüe"}]}`)
	}))
	defer server.Close()

	matcher := &Deezer{Client: server.Client(), BaseURL: server.URL, Gap: time.Millisecond}
	ok, err := matcher.Matches(context.Background(), "motley crue")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("a case/diacritic fold should match")
	}
}

func TestDeezerRetriesAThrottle(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 2 {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `{"data":[{"name":"Opeth"}]}`)
	}))
	defer server.Close()

	matcher := &Deezer{Client: server.Client(), BaseURL: server.URL, Gap: time.Millisecond}
	ok, err := matcher.Matches(context.Background(), "Opeth")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || calls.Load() != 3 {
		t.Fatalf("ok = %v after %d calls, want true after 3", ok, calls.Load())
	}
}

func TestDeezer404IsAnAnswer(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	matcher := &Deezer{Client: server.Client(), BaseURL: server.URL, Gap: time.Millisecond}
	ok, err := matcher.Matches(context.Background(), "Nobody")
	if err != nil || ok {
		t.Fatalf("(ok, err) = (%v, %v), want (false, nil)", ok, err)
	}
}
