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

// TestLiveCandidateVolume answers the only question a re-cut source mix can rest on: does this platform
// expose *volume* of events with a date and a venue, from this egress, right now?
func TestLiveCandidateVolume(t *testing.T) {
	c := New(Options{Interval: 3 * time.Second, Timeout: 45 * time.Second})

	type listing struct {
		name, url, linkPattern string
	}
	listings := []listing{
		{"dice-london", "https://dice.fm/browse/london", `href="(/event/[^"]+)"`},
		{"dice-london-p2", "https://dice.fm/browse/london?page=2", `href="(/event/[^"]+)"`},
		{"dice-newyork", "https://dice.fm/browse/new-york", `href="(/event/[^"]+)"`},
		{"eventfrog-berlin", "https://www.eventfrog.de/berlin", `href="(/event/[^"]+)"`},
		{"billetto-cph", "https://billetto.dk/en/cities/copenhagen", `href="(/e/[^"]+)"`},
	}

	for _, l := range listings {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		resp, err := c.Get(ctx, l.url)
		cancel()
		if err != nil && resp == nil {
			t.Logf("%-18s ERROR %v", l.name, err)
			continue
		}
		body := string(resp.Body)
		links := uniqueMatches(l.linkPattern, body)
		t.Logf("%-18s %d %8d b  unique event links=%d  first=%s",
			l.name, resp.Status, len(body), len(links), first(links))

		// One detail page from each listing, parsed for what a row needs: a start and a venue.
		if len(links) > 0 {
			detail := absolute(l.url, links[0])
			dctx, dcancel := context.WithTimeout(context.Background(), 60*time.Second)
			dresp, derr := c.Get(dctx, detail)
			dcancel()
			if derr != nil && dresp == nil {
				t.Logf("%-18s  detail ERROR %v", "", derr)
				continue
			}
			t.Logf("%-18s  detail %d %7d b  ld+json=%d  startDate-ish=%v  address-ish=%v | %s",
				"", dresp.Status, len(dresp.Body),
				strings.Count(string(dresp.Body), "application/ld+json"),
				strings.Contains(string(dresp.Body), "startDate"),
				strings.Contains(string(dresp.Body), `"location"`) || strings.Contains(string(dresp.Body), "address"),
				firstJSONLDType(dresp.Body))
		}
	}
}

func uniqueMatches(pattern, body string) []string {
	re := regexp.MustCompile(pattern)
	seen := map[string]bool{}
	var out []string
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		if len(m) < 2 || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		out = append(out, m[1])
	}
	return out
}

func first(items []string) string {
	if len(items) == 0 {
		return "-"
	}
	return items[0]
}

func absolute(base, link string) string {
	if strings.HasPrefix(link, "http") {
		return link
	}
	u, err := parseTarget(base)
	if err != nil {
		return link
	}
	return u.Scheme + "://" + u.Host + link
}

// firstJSONLDType names the @type of the first JSON-LD block, so "has structured data" becomes a fact.
func firstJSONLDType(body []byte) string {
	re := regexp.MustCompile(`(?s)<script[^>]+type="application/ld\+json"[^>]*>(.*?)</script>`)
	m := re.FindSubmatch(body)
	if m == nil {
		return "no json-ld"
	}
	var doc any
	if err := json.Unmarshal(m[1], &doc); err != nil {
		return "json-ld unparseable"
	}
	switch v := doc.(type) {
	case []any:
		if len(v) > 0 {
			return "@type(list)=" + typeOf(v[0])
		}
	case map[string]any:
		return "@type=" + typeOf(v)
	}
	return "json-ld shape unknown"
}

func typeOf(v any) string {
	if m, ok := v.(map[string]any); ok {
		if s, ok := m["@type"].(string); ok {
			return s
		}
	}
	return "?"
}
