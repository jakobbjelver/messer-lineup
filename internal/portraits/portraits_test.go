package portraits

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
	// Deezer has no picture for this one, so the fallback chain has to reach MusicBrainz.
	mux.HandleFunc("/deezer", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[]}`)
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
		DeezerURL:  server.URL + "/deezer?q=",
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
	mux.HandleFunc("/deezer", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[]}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	opts := Options{
		BaseURL:    server.URL + "/search?query=",
		LookupURL:  server.URL + "/artist/",
		CommonsURL: server.URL + "/file/",
		DeezerURL:  server.URL + "/deezer?q=",
		Client:     server.Client(),
		Gap:        time.Millisecond,
	}
	// A fuzzy hit for a bill is a wrong face: the name is not the artist, and the score is not near-certain.
	if _, kind := resolve(context.Background(), opts, "Archspire + Crown Magnetar", t.Logf); kind != none {
		t.Fatalf("a fuzzy match on a bill returned %v, want none", kind)
	}
}

func TestDeezerByNameIsTheFallback(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/artist/mbid-1", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"relations":[]}`)
	})
	mux.HandleFunc("/deezer", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != "Noah Gundersen" {
			t.Errorf("deezer query = %q", got)
		}
		fmt.Fprint(w, `{"data":[{"id":42,"name":"Noah Gundersen","picture_xl":"https://cdn-images.dzcdn.net/images/artist/x/1000x1000.jpg"}]}`)
	})
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"artists":[{"id":"mbid-1","name":"Noah Gundersen","score":100}]}`)
	})
	mux.HandleFunc("/wiki/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"type":"standard","title":"Noah Gundersen","description":"American singer-songwriter","thumbnail":{}}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	opts := Options{
		DeezerURL:    server.URL + "/deezer?q=",
		BaseURL:      server.URL + "/search?query=",
		LookupURL:    server.URL + "/artist/",
		CommonsURL:   server.URL + "/file/",
		WikipediaURL: server.URL + "/wiki/",
		Client:       server.Client(),
		Gap:          time.Millisecond,
	}
	portrait, kind := resolve(context.Background(), opts, "Noah Gundersen", t.Logf)
	if kind != found {
		t.Fatalf("kind = %v, want found", kind)
	}
	if !strings.Contains(portrait.URL, "dzcdn.net") {
		t.Fatalf("url = %q, want the Deezer picture", portrait.URL)
	}
}

// The linked id is what keeps a namesake off the row: MusicBrainz says which Deezer artist this is, and the name
// search is never asked.
func TestLinkedDeezerIdBeatsTheNameSearch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/artist/mbid-1", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"relations":[
			{"type":"free streaming","url":{"resource":"https://www.deezer.com/artist/999"}}
		]}`)
	})
	mux.HandleFunc("/deezer-artist/999", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":999,"name":"Wicker","picture_xl":"https://cdn-images.dzcdn.net/images/artist/linked/1000x1000.jpg"}`)
	})
	mux.HandleFunc("/deezer", func(w http.ResponseWriter, r *http.Request) {
		t.Error("the name search must not run when MusicBrainz links a Deezer id")
		fmt.Fprint(w, `{"data":[]}`)
	})
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"artists":[{"id":"mbid-1","name":"Wicker","score":100}]}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	opts := Options{
		DeezerURL:    server.URL + "/deezer?q=",
		DeezerArtist: server.URL + "/deezer-artist/",
		BaseURL:      server.URL + "/search?query=",
		LookupURL:    server.URL + "/artist/",
		CommonsURL:   server.URL + "/file/",
		Client:       server.Client(),
		Gap:          time.Millisecond,
	}
	portrait, kind := resolve(context.Background(), opts, "Wicker", t.Logf)
	if kind != found {
		t.Fatalf("kind = %v, want found", kind)
	}
	if !strings.Contains(portrait.URL, "linked") {
		t.Fatalf("url = %q, want the linked artist's picture", portrait.URL)
	}
	if portrait.Credit != "https://www.deezer.com/artist/999" {
		t.Fatalf("credit = %q", portrait.Credit)
	}
}
