//go:build livesmoke

package dice

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/fetch"
)

// TestLiveDiceBlocks prints each JSON-LD block of a page that failed, so the shape is a fact and not a guess.
func TestLiveDiceBlocks(t *testing.T) {
	client := fetch.New(fetch.Options{Interval: time.Second, Timeout: 30 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	resp, err := client.Get(ctx, "https://dice.fm/event/l8m7nr-the-psychology-of-horror-movies-how-horror-speaks-to-the-subconscious-18th-nov-augustine-house-hall-canterbury-tickets")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	blocks, err := jsonLDBlocks(resp.Body)
	if err != nil {
		t.Fatalf("blocks: %v", err)
	}
	for i, b := range blocks {
		var probe map[string]any
		shape := "not an object"
		if err := json.Unmarshal(b, &probe); err == nil {
			shape = "object"
			if at, ok := probe["@type"]; ok {
				shape = "object @type=" + jsonish(at)
			}
			if v, ok := probe["@graph"]; ok {
				shape += " graph=" + jsonish(v)
			}
		} else {
			var list []any
			if err := json.Unmarshal(b, &list); err == nil {
				shape = "list of " + jsonish(len(list))
			}
		}
		head := string(b)
		if len(head) > 220 {
			head = head[:220]
		}
		t.Logf("block %d [%d b] %s\n   %s", i, len(b), shape, head)
	}
}

func jsonish(v any) string {
	out, _ := json.Marshal(v)
	s := string(out)
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}
