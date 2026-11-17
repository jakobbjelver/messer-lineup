//go:build livesmoke

package fetch

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestLiveDiceDepth asks whether DICE is a spine or a sample: how many events a city page really carries,
// whether its robots.txt permits an API, and whether Copenhagen and Berlin are covered at all.
func TestLiveDiceDepth(t *testing.T) {
	c := New(Options{Interval: 3 * time.Second, Timeout: 45 * time.Second})

	for _, city := range []string{"copenhagen", "berlin", "london", "austin"} {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		resp, err := c.Get(ctx, "https://dice.fm/browse/"+city)
		cancel()
		if resp == nil {
			t.Logf("%-12s ERROR %v", city, err)
			continue
		}
		body := string(resp.Body)
		t.Logf("%-12s %d %8d b  event-links=%d  start_date-in-body=%v  __NEXT_DATA__=%v  hydration=%v",
			city, resp.Status, len(body),
			len(uniqueMatches(`href="(/event/[^"]+)"`, body)),
			strings.Contains(body, "start_date") || strings.Contains(body, "startDate"),
			strings.Contains(body, "__NEXT_DATA__"),
			strings.Contains(body, "__remixContext") || strings.Contains(body, "window.__data"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	robots, err := c.Get(ctx, "https://dice.fm/robots.txt")
	cancel()
	if robots != nil {
		body := string(robots.Body)
		t.Logf("robots       %d %6d b", robots.Status, len(body))
		for _, line := range strings.Split(body, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(strings.ToLower(line), "disallow") || strings.HasPrefix(strings.ToLower(line), "sitemap") {
				t.Logf("  %s", line)
			}
		}
	} else {
		t.Logf("robots ERROR %v", err)
	}
	_ = regexp.MustCompile
}
