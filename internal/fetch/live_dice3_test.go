//go:build livesmoke

package fetch

import (
	"context"
	"encoding/xml"
	"strings"
	"testing"
	"time"
)

type urlset struct {
	URLs []struct {
		Loc     string `xml:"loc"`
		LastMod string `xml:"lastmod"`
	} `xml:"url"`
}

// TestLiveDiceSitemaps sizes the DICE event index: how many URLs, what a URL looks like, and whether a
// city can be filtered from it without fetching every detail page.
func TestLiveDiceSitemaps(t *testing.T) {
	c := New(Options{Interval: 4 * time.Second, Timeout: 180 * time.Second, MaxBody: 64 << 20})
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	resp, err := c.Get(ctx, "https://dice.fm/sitemaps/sitemap1.xml")
	cancel()
	if resp == nil {
		t.Fatalf("sitemap1: %v", err)
	}
	var set urlset
	if err := xml.Unmarshal(resp.Body, &set); err != nil {
		t.Fatalf("parse: %v", err)
	}
	t.Logf("sitemap1: %d b, %d urls", len(resp.Body), len(set.URLs))
	shapes := map[string]int{}
	for _, u := range set.URLs {
		parts := strings.Split(strings.TrimPrefix(u.Loc, "https://dice.fm/"), "/")
		key := parts[0]
		if len(parts) > 1 && parts[0] == "event" {
			key = "event"
		}
		shapes[key]++
	}
	for shape, n := range shapes {
		t.Logf("  %-14s %d", shape, n)
	}
	for i, u := range set.URLs {
		if i < 6 {
			t.Logf("  %s  lastmod=%s", u.Loc, u.LastMod)
		}
	}
}
