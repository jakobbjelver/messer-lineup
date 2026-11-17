//go:build livesmoke

package fetch

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
	"time"
)

// TestLiveDiceDetail shows exactly which JSON-LD fields a DICE event page carries, because a parser is
// written against the shape that exists, not the shape a spec promises.
func TestLiveDiceDetail(t *testing.T) {
	c := New(Options{Interval: 3 * time.Second, Timeout: 60 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	resp, err := c.Get(ctx, "https://dice.fm/event/6af6e5fac4992d0001b4d821")
	cancel()
	if resp == nil {
		t.Fatalf("detail: %v", err)
	}
	re := regexp.MustCompile(`(?s)<script[^>]+type="application/ld\+json"[^>]*>(.*?)</script>`)
	for i, m := range re.FindAllSubmatch(resp.Body, -1) {
		var doc any
		if err := json.Unmarshal(m[1], &doc); err != nil {
			t.Logf("block %d unparseable: %v", i, err)
			continue
		}
		pretty, _ := json.MarshalIndent(doc, "", " ")
		if len(pretty) > 1800 {
			pretty = pretty[:1800]
		}
		t.Logf("block %d:\n%s", i, pretty)
	}
}
