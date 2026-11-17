//go:build livesmoke

package fetch

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestLiveDiceRobotsAndSitemap reads DICE's whole policy and its event sitemap: the policy decides whether
// DICE may be used at all, the sitemap decides how many events exist.
func TestLiveDiceRobotsAndSitemap(t *testing.T) {
	c := New(Options{Interval: 3 * time.Second, Timeout: 60 * time.Second})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	robots, err := c.Get(ctx, "https://dice.fm/robots.txt")
	cancel()
	if robots == nil {
		t.Fatalf("robots: %v", err)
	}
	t.Logf("=== dice.fm/robots.txt (%d b) ===", len(robots.Body))
	for _, line := range strings.Split(string(robots.Body), "\n") {
		t.Logf("  %s", strings.TrimRight(line, "\r"))
	}

	ctx, cancel = context.WithTimeout(context.Background(), 120*time.Second)
	sm, serr := c.Get(ctx, "https://dice.fm/sitemaps/sitemap.xml")
	cancel()
	if sm == nil {
		t.Logf("sitemap ERROR %v", serr)
		return
	}
	body := string(sm.Body)
	locs := uniqueMatches(`(?m)<loc>([^<]+)</loc>`, body)
	t.Logf("sitemap %d %d b  locs=%d  first=%s", sm.Status, len(body), len(locs), first(locs))
	if len(locs) > 0 && len(locs) < 12 {
		for _, l := range locs {
			t.Logf("  %s", l)
		}
	}
}
