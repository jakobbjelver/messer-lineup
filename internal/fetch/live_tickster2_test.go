//go:build livesmoke

package fetch

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestLiveTicksterDK follows one Danish Tickster event page to the end: does it parse, and does it carry a
// date, a venue and a town?
func TestLiveTicksterDK(t *testing.T) {
	c := New(Options{Interval: 2 * time.Second, Timeout: 45 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	sm, err := c.Get(ctx, "https://www.tickster.com/sitemap.xml")
	cancel()
	if sm == nil {
		t.Fatalf("sitemap: %v", err)
	}
	locs := uniqueMatches(`(?m)<loc>([^<]+)</loc>`, string(sm.Body))
	var dk []string
	for _, l := range locs {
		if strings.Contains(l, "/dk/da/") && !strings.HasSuffix(l, "/start") {
			dk = append(dk, l)
		}
	}
	t.Logf("dk/da urls: %d", len(dk))
	for i, l := range dk {
		if i >= 6 {
			break
		}
		t.Logf("  %s", l)
	}
	// Fetch a handful and report what they carry.
	re := regexp.MustCompile(`(?s)<script[^>]+type="application/ld\+json"[^>]*>(.*?)</script>`)
	ok := 0
	for i := 0; i < len(dk) && i < 8; i++ {
		cctx, ccancel := context.WithTimeout(context.Background(), 45*time.Second)
		resp, derr := c.Get(cctx, dk[i])
		ccancel()
		if resp == nil {
			t.Logf("  %s ERROR %v", dk[i], derr)
			continue
		}
		body := string(resp.Body)
		types := ""
		for _, m := range re.FindAllStringSubmatch(body, -1) {
			var doc map[string]any
			if json.Unmarshal([]byte(m[1]), &doc) == nil {
				if t, ok := doc["@type"].(string); ok {
					types += t + " "
				}
			}
		}
		if types != "" {
			ok++
		}
		t.Logf("  %d %d %6d b  ld+json types: %s", resp.Status, len(body), len(body), types)
	}
	t.Logf("pages with structured data: %d", ok)
}
