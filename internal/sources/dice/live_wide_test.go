//go:build livesmoke

package dice

import (
	"context"
	"os"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/fetch"
	"github.com/jakobbjelver/messer-lineup/internal/model"
)

// TestLiveDiceWideSample measures the whole index instead of one shard: a fixed number of candidates from
// every sitemap, bucketed by town. One shard is not the world — sitemap1 turned out to be almost entirely
// London — and a gate that names three towns cannot be answered from one shard.
//
//	DICE_PER_SHARD=120 go test -tags livesmoke -run TestLiveDiceWideSample ./internal/sources/dice/ -v
func TestLiveDiceWideSample(t *testing.T) {
	perShard := 40
	if v := os.Getenv("DICE_PER_SHARD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			perShard = n
		}
	}

	client := fetch.New(fetch.Options{Interval: time.Second, Timeout: 30 * time.Second})
	src := New(client)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	sitemaps, err := src.Sitemaps(ctx)
	if err != nil {
		t.Fatalf("sitemaps: %v", err)
	}

	var sampled []model.Candidate
	for _, sm := range sitemaps {
		batch, err := src.Candidates(ctx, sm)
		if err != nil {
			t.Logf("%s: %v", sm, err)
			continue
		}
		// Spread the sample through the shard rather than taking its head: the sitemap is ordered by
		// lastmod, so the head is one moment's uploads and would bias every town the same way.
		var picked []model.Candidate
		if len(batch) <= perShard {
			picked = batch
		} else {
			step := len(batch) / perShard
			for i := 0; i < len(batch) && len(picked) < perShard; i += step {
				picked = append(picked, batch[i])
			}
		}
		t.Logf("%-46s %5d candidates → sampling %d", sm, len(batch), len(picked))
		sampled = append(sampled, picked...)
	}

	t.Logf("sampling %d candidates across %d shards", len(sampled), len(sitemaps))
	towns := map[string]int{}
	failures := 0
	rows := 0
	start := time.Now()
	for i, c := range sampled {
		show, err := src.Detail(ctx, c)
		if err != nil {
			failures++
			continue
		}
		if _, err := model.Project(show); err != nil {
			continue
		}
		rows++
		towns[show.Town]++
		if i%50 == 0 {
			t.Logf("  %d/%d — %.0fs", i, len(sampled), time.Since(start).Seconds())
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

	t.Logf("RESULT: %d sampled, %d rows, %d failures, %.0fs", len(sampled), rows, failures, time.Since(start).Seconds())
	for i, p := range list {
		if i >= 20 {
			break
		}
		t.Logf("  %-18s %d", p.town, p.n)
	}
	for _, town := range []string{"berlin-de", "london-gb", "austin-us", "new-york-us", "copenhagen-dk", "stockholm-se"} {
		t.Logf("  gate town %-14s %d", town, towns[town])
	}
	t.Logf("  distinct towns: %d", len(towns))
}
