package fetch

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorder is a server that remembers the headers of the last request and how many times each path was
// asked for.
type recorder struct {
	mu      sync.Mutex
	headers http.Header
	hits    map[string]int
	body    []byte
	gzipped bool
	status  int
	robots  string
}

func newServer(t *testing.T, r *recorder) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, req *http.Request) {
		r.hit("/robots.txt")
		if r.status == 503 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(r.robots))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		r.hit(req.URL.Path)
		r.mu.Lock()
		r.headers = req.Header.Clone()
		r.mu.Unlock()
		if r.status >= 400 {
			w.WriteHeader(r.status)
			return
		}
		if r.gzipped {
			w.Header().Set("Content-Encoding", "gzip")
			var buf bytes.Buffer
			zw := gzip.NewWriter(&buf)
			_, _ = zw.Write(r.body)
			_ = zw.Close()
			_, _ = w.Write(buf.Bytes())
			return
		}
		_, _ = w.Write(r.body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func (r *recorder) hit(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.hits == nil {
		r.hits = map[string]int{}
	}
	r.hits[path]++
}

func (r *recorder) count(path string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hits[path]
}

// TestEveryHeaderIsSent is the regression that cost a week: a UA-only request is killed mid-stream by
// Eventim and Billetlugen, and the failure looks like a network block rather than a missing header.
func TestEveryHeaderIsSent(t *testing.T) {
	rec := &recorder{body: []byte("ok")}
	srv := newServer(t, rec)
	c := New(Options{Interval: -1})

	if _, err := c.Get(context.Background(), srv.URL+"/page"); err != nil {
		t.Fatalf("get: %v", err)
	}

	rec.mu.Lock()
	h := rec.headers
	rec.mu.Unlock()
	if h == nil {
		t.Fatal("the server saw no request")
	}

	want := map[string]string{
		"Accept-Encoding":           "gzip",
		"sec-ch-ua-mobile":          "?0",
		"sec-ch-ua-platform":        `"macOS"`,
		"Sec-Fetch-Dest":            "document",
		"Sec-Fetch-Mode":            "navigate",
		"Sec-Fetch-Site":            "none",
		"Sec-Fetch-User":            "?1",
		"Upgrade-Insecure-Requests": "1",
	}
	for name, value := range want {
		if got := h.Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
	for _, name := range []string{"User-Agent", "Accept", "Accept-Language", "sec-ch-ua"} {
		if h.Get(name) == "" {
			t.Errorf("%s is missing", name)
		}
	}
	if ua := h.Get("User-Agent"); !strings.HasPrefix(ua, "Mozilla/5.0") || strings.Contains(ua, "Go-http-client") {
		t.Errorf("User-Agent = %q, want a browser profile", ua)
	}
	if !strings.Contains(h.Get("Accept"), "text/html") {
		t.Errorf("Accept = %q", h.Get("Accept"))
	}
}

// TestGzippedBodyDecodes is the other half of the same trap: `Accept-Encoding: gzip` without the gunzip
// yields a 200 full of binary and a parser that reports zero events.
func TestGzippedBodyDecodes(t *testing.T) {
	payload := []byte(`{"@type":"MusicEvent","name":"Viagra Boys"}`)
	rec := &recorder{body: payload, gzipped: true}
	srv := newServer(t, rec)
	c := New(Options{Interval: -1})

	resp, err := c.Get(context.Background(), srv.URL+"/event")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !bytes.Equal(resp.Body, payload) {
		t.Fatalf("body = %q, want %q", resp.Body, payload)
	}
	if enc := resp.Header.Get("Content-Encoding"); enc != "gzip" {
		t.Fatalf("the test is not exercising gzip: Content-Encoding = %q", enc)
	}
}

func TestPlainBodyPassesThrough(t *testing.T) {
	rec := &recorder{body: []byte("plain")}
	srv := newServer(t, rec)
	c := New(Options{Interval: -1})
	resp, err := c.Get(context.Background(), srv.URL+"/plain")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(resp.Body) != "plain" {
		t.Fatalf("body = %q", resp.Body)
	}
}

func TestStatusOfCarriesTheCodeForASitemapWalk(t *testing.T) {
	rec := &recorder{status: 404}
	srv := newServer(t, rec)
	c := New(Options{Interval: -1})
	_, err := c.Get(context.Background(), srv.URL+"/events3_de.xml.gz")
	if err == nil {
		t.Fatal("a 404 was returned as success")
	}
	if got := StatusOf(err); got != 404 {
		t.Fatalf("StatusOf = %d, want 404", got)
	}
	var se *StatusError
	if !errors.As(err, &se) || se.URL == "" {
		t.Fatalf("error does not carry the URL: %v", err)
	}
}

// TestLimiterSpacesRequestsPerHost drives the limiter with a fake clock, so the rule is asserted rather
// than waited out.
func TestLimiterSpacesRequestsPerHost(t *testing.T) {
	var sleeps []time.Duration
	now := time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC)
	rec := &recorder{body: []byte("ok")}
	srv := newServer(t, rec)
	c := New(Options{
		Interval: time.Second,
		Now:      func() time.Time { return now },
		Sleep: func(d time.Duration) {
			sleeps = append(sleeps, d)
			now = now.Add(d)
		},
	})
	for i := 0; i < 3; i++ {
		if _, err := c.Get(context.Background(), srv.URL+"/page"); err != nil {
			t.Fatalf("get %d: %v", i, err)
		}
	}
	if len(sleeps) != 2 {
		t.Fatalf("slept %d times for three requests, want 2: %v", len(sleeps), sleeps)
	}
	for _, d := range sleeps {
		if d != time.Second {
			t.Fatalf("slept %v, want exactly the interval", d)
		}
	}
	// The robots fetch is one request per host and must not consume a rate-limited slot.
	if got := rec.count("/robots.txt"); got != 1 {
		t.Fatalf("robots.txt was fetched %d times, want once", got)
	}
}

func TestRobotsRefusesADisallowedPath(t *testing.T) {
	rec := &recorder{body: []byte("secret"), robots: "User-agent: *\nDisallow: /private\n"}
	srv := newServer(t, rec)
	c := New(Options{Interval: -1})

	_, err := c.Get(context.Background(), srv.URL+"/private/thing")
	if !errors.Is(err, ErrDisallowed) {
		t.Fatalf("err = %v, want ErrDisallowed", err)
	}
	if got := rec.count("/private/thing"); got != 0 {
		t.Fatalf("the disallowed path was requested %d times", got)
	}
	// An allowed path on the same host is unaffected.
	if _, err := c.Get(context.Background(), srv.URL+"/public"); err != nil {
		t.Fatalf("allowed path: %v", err)
	}
}

func TestRobotsUnavailableIsNotPermission(t *testing.T) {
	rec := &recorder{body: []byte("ok"), status: 503}
	srv := newServer(t, rec)
	c := New(Options{Interval: -1})
	if _, err := c.Get(context.Background(), srv.URL+"/page"); !errors.Is(err, ErrRobotsUnavailable) {
		t.Fatalf("err = %v, want ErrRobotsUnavailable", err)
	}
}

func TestRobotsIsFetchedOncePerTTL(t *testing.T) {
	now := time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC)
	rec := &recorder{body: []byte("ok"), robots: "User-agent: *\nDisallow: /private\n"}
	srv := newServer(t, rec)
	c := New(Options{Interval: -1, RobotsTTL: time.Hour, Now: func() time.Time { return now }})

	for i := 0; i < 3; i++ {
		if _, err := c.Get(context.Background(), srv.URL+"/public"); err != nil {
			t.Fatalf("get: %v", err)
		}
	}
	if got := rec.count("/robots.txt"); got != 1 {
		t.Fatalf("robots.txt fetched %d times inside the TTL, want 1", got)
	}
	now = now.Add(2 * time.Hour)
	if _, err := c.Get(context.Background(), srv.URL+"/public"); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := rec.count("/robots.txt"); got != 2 {
		t.Fatalf("robots.txt fetched %d times after the TTL, want 2", got)
	}
}

func TestAllowDoesNotRequestThePage(t *testing.T) {
	rec := &recorder{body: []byte("ok"), robots: "User-agent: *\nDisallow: /off\n"}
	srv := newServer(t, rec)
	c := New(Options{Interval: -1})
	if err := c.Allow(context.Background(), srv.URL+"/off/events"); !errors.Is(err, ErrDisallowed) {
		t.Fatalf("allow: %v", err)
	}
	if err := c.Allow(context.Background(), srv.URL+"/on/events"); err != nil {
		t.Fatalf("allow: %v", err)
	}
	if got := rec.count("/off/events") + rec.count("/on/events"); got != 0 {
		t.Fatalf("%d page requests were made by an allow check", got)
	}
}

func TestMissingRobotsAllowsEverything(t *testing.T) {
	// A 404 on robots.txt is "no policy published", not "forbidden".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	c := New(Options{Interval: -1})
	if _, err := c.Get(context.Background(), srv.URL+"/page"); err != nil {
		t.Fatalf("get: %v", err)
	}
}
