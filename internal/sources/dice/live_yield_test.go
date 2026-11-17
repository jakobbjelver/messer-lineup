//go:build livesmoke

package dice

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/fetch"
	"github.com/jakobbjelver/messer-lineup/internal/model"
)

// TestLiveDiceTownYield answers the only question the re-cut source mix rests on: from this egress, does
// DICE actually yield events for the towns a gate names? It is a sample, not a full sweep, and it says so.
//
//	go test -tags livesmoke -run TestLiveDiceTownYield ./internal/sources/dice/ -v
func TestLiveDiceTownYield(t *testing.T) {
	if testing.Short() {
		t.Skip("live")
	}
	const sampleCap = 300

	client := fetch.New(fetch.Options{Interval: time.Second, Timeout: 30 * time.Second})
	src := New(client)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	sitemaps, err := src.Sitemaps(ctx)
	if err != nil {
		t.Fatalf("sitemaps: %v", err)
	}
	t.Logf("sitemap index: %d shards, first %s", len(sitemaps), firstOr(sitemaps))

	var candidates []model.Candidate
	for _, sm := range sitemaps {
		if len(candidates) >= sampleCap {
			break
		}
		batch, err := src.Candidates(ctx, sm)
		if err != nil {
			t.Logf("%s: %v", sm, err)
			continue
		}
		t.Logf("%s: %d candidates", sm, len(batch))
		candidates = append(candidates, batch...)
	}
	if len(candidates) > sampleCap {
		candidates = candidates[:sampleCap]
	}
	t.Logf("sampling %d candidates", len(candidates))

	towns := map[string]int{}
	failures := 0
	rows := 0
	start := time.Now()
	for i, c := range candidates {
		show, err := src.Detail(ctx, c)
		if err != nil {
			failures++
			continue
		}
		if _, err := model.Project(show); err != nil {
			t.Logf("  projected row refused: %v", err)
			continue
		}
		rows++
		towns[show.Town]++
		if i%50 == 0 {
			t.Logf("  %d/%d — %d towns so far, %v", i, len(candidates), len(towns), elapsed(start))
		}
	}

	type pair struct {
		town string
		n    int
	}
	var list []pair
	for town, n := range towns {
		list = append(list, pair{town, n})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].n > list[j].n })

	t.Logf("RESULT: %d candidates, %d rows, %d failures, %.0fs", len(candidates), rows, failures, time.Since(start).Seconds())
	for i, p := range list {
		if i >= 15 {
			break
		}
		t.Logf("  %-18s %d", p.town, p.n)
	}
	for _, town := range []string{"berlin-de", "london-gb", "austin-us", "new-york-us"} {
		t.Logf("  gate town %-14s %d", town, towns[town])
	}
}

func firstOr(items []string) string {
	if len(items) == 0 {
		return "-"
	}
	return items[0]
}

func elapsed(start time.Time) string { return time.Since(start).Round(time.Second).String() }
