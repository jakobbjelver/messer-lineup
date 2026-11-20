package portraits

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCommonsFile(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"https://commons.wikimedia.org/wiki/File:Bj%C3%B6rk_1997.jpg", "Björk_1997.jpg", true},
		{"https://commons.wikimedia.org/wiki/File:Bjork_at_eden_project.jpg", "Bjork_at_eden_project.jpg", true},
		{"https://example.com/artist/123", "", false},
		{"https://commons.wikimedia.org/wiki/Category:Something", "", false},
	}
	for _, tc := range cases {
		got, ok := commonsFile(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("commonsFile(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestResolveWalksSearchLookupAndThumbnail(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("query"); got != "Björk" {
			t.Errorf("search query = %q", got)
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("no user agent")
		}
		fmt.Fprint(w, `{"artists":[{"id":"mbid-1","name":"Björk","score":100}]}`)
	})
	mux.HandleFunc("/artist/mbid-1", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"relations":[
			{"type":"official homepage","url":{"resource":"https://bjork.com/"}},
			{"type":"image","url":{"resource":"https://commons.wikimedia.org/wiki/File:Bj%C3%B6rk_1997.jpg"}}
		]}`)
	})
	mux.HandleFunc("/file/", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/file/Björk_1997.jpg" {
			t.Errorf("file path = %q", got)
		}
		if r.URL.Query().Get("width") != "240" {
			t.Errorf("width = %q", r.URL.Query().Get("width"))
		}
		http.Redirect(w, r, "/thumb/250px-Bjork_1997.jpg", http.StatusFound)
	})
	mux.HandleFunc("/thumb/250px-Bjork_1997.jpg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "image/jpeg")
		w.Write([]byte("not really a jpeg"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	opts := Options{
		BaseURL:    server.URL + "/search?query=",
		LookupURL:  server.URL + "/artist/",
		CommonsURL: server.URL + "/file/",
		Client:     server.Client(),
		Gap:        time.Millisecond,
	}
	portrait, kind := resolve(context.Background(), opts, "Björk", t.Logf)
	if kind != found {
		t.Fatalf("resolve returned %v, want found", kind)
	}
	if portrait.URL != server.URL+"/thumb/250px-Bjork_1997.jpg" {
		t.Fatalf("url = %q, want the resolved thumbnail", portrait.URL)
	}
	if portrait.Credit == "" {
		t.Fatal("no credit")
	}
}

func TestResolveRefusesABill(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"artists":[{"id":"mbid-1","name":"Archspire","score":90}]}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	opts := Options{
		BaseURL:    server.URL + "/search?query=",
		LookupURL:  server.URL + "/artist/",
		CommonsURL: server.URL + "/file/",
		Client:     server.Client(),
		Gap:        time.Millisecond,
	}
	// A fuzzy hit for a bill is a wrong face: the name is not the artist, and the score is not near-certain.
	if _, kind := resolve(context.Background(), opts, "Archspire + Crown Magnetar", t.Logf); kind != none {
		t.Fatalf("a fuzzy match on a bill returned %v, want none", kind)
	}
}
