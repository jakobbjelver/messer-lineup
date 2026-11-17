//go:build livesmoke

package dice

import (
	"context"
	"testing"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/fetch"
)

// TestLiveDiceTownGate answers G0-global's question town by town: does a DICE city page hold ≥20 events
// that become rows? One request per town for the candidate list, then one detail fetch each.
//
//	go test -tags livesmoke -run TestLiveDiceTownGate ./internal/sources/dice/ -v
func TestLiveDiceTownGate(t *testing.T) {
	client := fetch.New(fetch.Options{Interval: time.Second, Timeout: 30 * time.Second})
	src := New(client)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	towns := []struct{ slug, want string }{
		{"berlin", "berlin-de"},
		{"london", "london-gb"},
		{"austin", "austin-us"},
		{"new-york", "new-york-us"},
	}
	for _, town := range towns {
		candidates, err := src.TownCandidates(ctx, town.slug)
		if err != nil {
			t.Logf("%-10s ERROR %v", town.slug, err)
			continue
		}
		matched, other, failures := 0, 0, 0
		for _, c := range candidates {
			show, err := src.Detail(ctx, c)
			if err != nil {
				failures++
				continue
			}
			if show.Town == town.want {
				matched++
			} else {
				other++
			}
		}
		t.Logf("%-10s candidates=%2d  rows in %-12s= %2d  elsewhere=%2d  failures=%d  gate(20)=%v",
			town.slug, len(candidates), town.want, matched, other, failures, matched >= 20)
	}
}
