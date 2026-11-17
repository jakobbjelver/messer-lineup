//go:build livesmoke

package dice

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/fetch"
)

// TestLiveDiceFailureReasons separates "the page is gone" from "the parser could not read it". A sitemap
// keeps URLs for events that have ended, so a fetch failure is not automatically a parse failure — and the
// gate is about parsing.
func TestLiveDiceFailureReasons(t *testing.T) {
	client := fetch.New(fetch.Options{Interval: time.Second, Timeout: 30 * time.Second})
	src := New(client)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	sitemaps, err := src.Sitemaps(ctx)
	if err != nil {
		t.Fatalf("sitemaps: %v", err)
	}
	candidates, err := src.Candidates(ctx, sitemaps[2])
	if err != nil {
		t.Fatalf("candidates: %v", err)
	}
	const n = 80
	step := len(candidates) / n
	reasons := map[string]int{}
	for i, idx := 0, 0; i < len(candidates) && idx < n; i, idx = i+step, idx+1 {
		_, err := src.Detail(ctx, candidates[i])
		reasons[classify(err)]++
	}
	type pair struct {
		why string
		n   int
	}
	var list []pair
	for why, count := range reasons {
		list = append(list, pair{why, count})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].n > list[j].n })
	for _, p := range list {
		t.Logf("%-28s %d", p.why, p.n)
	}
}

func classify(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, fetch.ErrDisallowed):
		return "robots"
	case fetch.StatusOf(err) == 404 || fetch.StatusOf(err) == 410:
		return "gone (404/410)"
	case fetch.StatusOf(err) > 0:
		return "http " + itoa(fetch.StatusOf(err))
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "no schema.org"):
		return "no MusicEvent"
	case strings.Contains(msg, "startDate"):
		return "no start date"
	case strings.Contains(msg, "town address"):
		return "no town"
	case strings.Contains(msg, "venue name"):
		return "no venue"
	}
	return "other: " + msg
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
