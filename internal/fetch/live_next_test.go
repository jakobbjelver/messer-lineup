//go:build livesmoke

package fetch

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestLiveTownDiscovery asks two things at once: can Tickster DK give Copenhagen its second source, and does a
// DICE city page carry the town's name, country, coordinates and timezone in its hydration blob?
func TestLiveTownDiscovery(t *testing.T) {
	c := New(Options{Interval: 2 * time.Second, Timeout: 45 * time.Second})

	// Tickster: robots, then a DK listing.
	for _, u := range []string{"https://www.tickster.com/robots.txt", "https://www.tickster.com/da/danmark/kobenhavn"} {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		resp, err := c.Get(ctx, u)
		cancel()
		if resp == nil {
			t.Logf("%-46s ERROR %v", u, err)
			continue
		}
		body := string(resp.Body)
		t.Logf("%-46s %d %7d b  /event-links=%d  ld+json=%d", u, resp.Status, len(body),
			len(uniqueMatches(`href="([^"]*/event/[^"]+)"`, body)), strings.Count(body, "application/ld+json"))
		if strings.HasSuffix(u, "robots.txt") && len(body) < 4000 {
			flat := strings.Join(strings.Fields(body), " ")
			if len(flat) > 600 {
				flat = flat[:600]
			}
			t.Logf("   robots: %s", flat)
		}
		if resp.Status == 200 && strings.Contains(u, "kobenhavn") {
			t.Logf("   first links: %v", firstN(uniqueMatches(`href="([^"]*/event/[^"]+)"`, body), 3))
		}
	}

	// DICE city page: look for the city object in the hydration blob.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	resp, err := c.Get(ctx, "https://dice.fm/browse/berlin")
	cancel()
	if resp == nil {
		t.Fatalf("dice berlin: %v", err)
	}
	body := string(resp.Body)
	re := regexp.MustCompile(`"(city|cityName|country|countryCode|timeZone|latitude|longitude)"\s*:\s*("[^"]{0,40}"|-?[0-9][0-9.]*)`)
	hits := re.FindAllStringSubmatch(body, -1)
	seen := map[string]bool{}
	for _, h := range hits {
		key := h[1] + "=" + h[2]
		if seen[key] || len(seen) > 14 {
			continue
		}
		seen[key] = true
		t.Logf("   dice blob: %s", key)
	}
}

func firstN(items []string, n int) []string {
	if len(items) < n {
		return items
	}
	return items[:n]
}
