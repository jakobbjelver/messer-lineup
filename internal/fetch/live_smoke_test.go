//go:build livesmoke

// A live smoke test: it touches the network, so it only runs when asked.
//
//	go test -tags livesmoke -run TestLiveHeaderProfile ./internal/fetch/ -v
//
// It exists because the header profile is the difference between "this host is blocked" and "this host
// answers 200 with the full page", and that difference is only observable against the real host.
package fetch

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLiveHeaderProfile(t *testing.T) {
	c := New(Options{Interval: time.Second})
	targets := map[string]string{
		// The one that was recorded as blocked for a week.
		"billetlugen": "https://www.billetlugen.dk/",
		// The sitemap index P0.7 walks.
		"eventim-sitemap": "https://www.eventim.de/staticsite/sitemap/EVE/sitemapindex_de.xml",
	}
	for name, url := range targets {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			resp, err := c.Get(ctx, url)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if resp.Status != 200 {
				t.Fatalf("%s: status %d", name, resp.Status)
			}
			if len(resp.Body) < 500 {
				t.Fatalf("%s: only %d bytes", name, len(resp.Body))
			}
			t.Logf("%s: 200, %d bytes decoded, content-type %q", name, len(resp.Body), resp.Header.Get("Content-Type"))
			if name == "eventim-sitemap" && !strings.Contains(string(resp.Body), "<sitemapindex") && !strings.Contains(string(resp.Body), "<urlset") {
				t.Fatalf("eventim sitemap did not return XML: %q", string(resp.Body[:min(120, len(resp.Body))]))
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
