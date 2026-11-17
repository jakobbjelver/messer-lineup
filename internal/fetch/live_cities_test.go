//go:build livesmoke

package fetch

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestLiveDiceCityIndex asks whether DICE publishes a city list, because country-first mining needs one: a
// country is a set of towns, and a set has to come from the platform rather than a hand-written file.
func TestLiveDiceCityIndex(t *testing.T) {
	c := New(Options{Interval: 2 * time.Second, Timeout: 60 * time.Second})
	for _, u := range []string{
		"https://dice.fm/browse",
		"https://dice.fm/sitemaps/venues/sitemap.xml",
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		resp, err := c.Get(ctx, u)
		cancel()
		if resp == nil {
			t.Logf("%-52s ERROR %v", u, err)
			continue
		}
		body := string(resp.Body)
		cityLinks := uniqueMatches(`href="(/browse/[a-z0-9-]+)"`, body)
		locLocs := uniqueMatches(`(?m)<loc>([^<]+)</loc>`, body)
		t.Logf("%-52s %d %8d b  /browse links=%d  locs=%d", u, resp.Status, len(body), len(cityLinks), len(locLocs))
		if len(cityLinks) > 0 {
			t.Logf("   cities: %v", firstN(cityLinks, 12))
		}
		if len(locLocs) > 0 {
			t.Logf("   first locs: %v", firstN(locLocs, 5))
		}
		// A country field next to a city would make the mapping direct.
		re := regexp.MustCompile(`"country"\s*:\s*"([A-Za-z]{2})"`)
		seen := map[string]bool{}
		var codes []string
		for _, m := range re.FindAllStringSubmatch(body, -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				codes = append(codes, m[1])
			}
		}
		if len(codes) > 0 {
			t.Logf("   country codes seen: %s", strings.Join(codes, " "))
		}
	}
}
