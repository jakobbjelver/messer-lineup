//go:build livesmoke

package fetch

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestLiveTickster looks for Copenhagen's second source: Tickster publishes a sitemap and allows crawling with
// Crawl-delay: 2, so the question is only whether its URLs carry Denmark, and whether a detail page parses.
func TestLiveTickster(t *testing.T) {
	c := New(Options{Interval: 2 * time.Second, Timeout: 45 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	resp, err := c.Get(ctx, "https://www.tickster.com/sitemap.xml")
	cancel()
	if resp == nil {
		t.Fatalf("sitemap: %v", err)
	}
	body := string(resp.Body)
	t.Logf("sitemap %d %d b", resp.Status, len(body))
	locs := uniqueMatches(`(?m)<loc>([^<]+)</loc>`, body)
	t.Logf("locs: %d", len(locs))
	for i, l := range locs {
		if i >= 12 {
			break
		}
		t.Logf("  %s", l)
	}
	// Sample a shape rather than guessing: print the first few paths.
	shapes := map[string]int{}
	for _, l := range locs {
		parts := strings.Split(strings.TrimPrefix(l, "https://"), "/")
		key := parts[0]
		if len(parts) > 2 {
			key += "/" + parts[1] + "/" + parts[2]
		}
		shapes[key]++
	}
	for shape, n := range shapes {
		t.Logf("  shape %-40s %d", shape, n)
	}
}
