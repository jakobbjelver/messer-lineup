//go:build livesmoke

package dice

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/fetch"
)

// TestLiveDiceNoJSONLD looks at the pages that failed, instead of calling them a 10 % parser loss. If the
// same event data is in the page's hydration blob, the adapter is missing a shape, not a page.
func TestLiveDiceNoJSONLD(t *testing.T) {
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
	titleRe := regexp.MustCompile(`(?s)<title>(.*?)</title>`)
	shown := 0
	for i := 0; i < len(candidates) && shown < 4; i += 7 {
		resp, gerr := client.Get(ctx, candidates[i].URL)
		if gerr != nil || resp == nil {
			continue
		}
		body := string(resp.Body)
		if strings.Contains(body, "application/ld+json") && strings.Contains(body, "MusicEvent") {
			continue
		}
		shown++
		title := ""
		if m := titleRe.FindStringSubmatch(body); m != nil {
			title = strings.TrimSpace(m[1])
		}
		t.Logf("%s", candidates[i].URL)
		t.Logf("  status=%d bytes=%d ld+json=%v MusicEvent=%v startDate=%v __NEXT_DATA__=%v",
			resp.Status, len(body),
			strings.Contains(body, "application/ld+json"),
			strings.Contains(body, "MusicEvent"),
			strings.Contains(body, "startDate"),
			strings.Contains(body, "__NEXT_DATA__"))
		t.Logf("  title=%q", title)
		if idx := strings.Index(body, "startDate"); idx > 0 && idx < len(body) {
			snippet := body[idx:min(idx+160, len(body))]
			t.Logf("  around startDate: %s", strings.ReplaceAll(snippet, "\n", " "))
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
