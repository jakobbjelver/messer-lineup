//go:build livesmoke

package fetch

import (
	"context"
	"regexp"
	"testing"
	"time"
)

// TestLiveCandidatesSecondPass closes the two questions the first pass left: how many events a DICE city
// really exposes, whether a US city is covered, and what Eventfrog's and Billetto's links actually look like.
func TestLiveCandidatesSecondPass(t *testing.T) {
	c := New(Options{Interval: 3 * time.Second, Timeout: 45 * time.Second})
	targets := []struct {
		name, url, pattern string
	}{
		{"dice-austin", "https://dice.fm/browse/austin", `href="(/event/[^"]+)"`},
		{"dice-sitemap", "https://dice.fm/sitemap.xml", `(?m)<loc>([^<]+)</loc>`},
		{"eventfrog-berlin", "https://www.eventfrog.de/berlin", `(?:href|data-href)="([^"]*event[^"]*)"`},
		{"billetto-dk", "https://billetto.dk/", `href="(/e/[^"]+)"`},
		{"billetto-api", "https://billetto.dk/api/v3/events?limit=5", `"title"`},
	}
	for _, target := range targets {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		resp, err := c.Get(ctx, target.url)
		cancel()
		if resp == nil {
			t.Logf("%-18s ERROR %v", target.name, err)
			continue
		}
		body := string(resp.Body)
		hits := uniqueMatches(target.pattern, body)
		t.Logf("%-18s %d %9d b  matches=%d  first=%s", target.name, resp.Status, len(body), len(hits), first(hits))
		if len(hits) > 0 && len(hits) < 6 {
			for _, h := range hits {
				t.Logf("%-18s   %s", "", h)
			}
		}
	}
	_ = regexp.MustCompile
}
