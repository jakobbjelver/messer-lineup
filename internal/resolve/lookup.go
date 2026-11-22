package resolve

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/model"
)

// defaultBase is Deezer's keyless artist search: one call, no key, and an exact name in the results or not. It is the
// same provider the edge already asks for portraits, so a name that resolves here is a name the board can also draw.
const defaultBase = "https://api.deezer.com/search/artist"

// defaultGap keeps a burst of names from becoming a burst of requests. Deezer's public limit is per-IP and generous,
// but a sweep asks hundreds of questions in a row; 200 ms is five a second and still finishes 200 names in 40 s.
const defaultGap = 200 * time.Millisecond

// Deezer is the default Matcher.
type Deezer struct {
	Client    *http.Client
	BaseURL   string
	Gap       time.Duration
	UserAgent string

	mu   sync.Mutex
	last time.Time
}

// NewDeezer returns a matcher using the given client (nil gets a 15-second default).
func NewDeezer(client *http.Client) *Deezer {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Deezer{Client: client, BaseURL: defaultBase, Gap: defaultGap, UserAgent: "MesserLineup/0.2 (https://lineup.messer.wtf)"}
}

// Matches reports whether name is a real artist's exact name (folded through the identity function, so a case or
// diacritic difference still matches and a namesake does not).
func (d *Deezer) Matches(ctx context.Context, name string) (bool, error) {
	base := d.BaseURL
	if base == "" {
		base = defaultBase
	}
	gap := d.Gap
	if gap == 0 {
		gap = defaultGap
	}
	userAgent := d.UserAgent
	if userAgent == "" {
		userAgent = "MesserLineup/0.2 (https://lineup.messer.wtf)"
	}

	// The lock is held across the request on purpose: the resolver is one goroutine, and serialising here is what
	// makes the gap a real gap rather than a suggestion.
	d.mu.Lock()
	defer d.mu.Unlock()
	if wait := gap - time.Since(d.last); wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false, ctx.Err()
		case <-timer.C:
		}
	}
	d.last = time.Now()

	target := base + "?q=" + url.QueryEscape(name) + "&limit=5"
	for attempt := 0; ; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return false, err
		}
		request.Header.Set("user-agent", userAgent)
		request.Header.Set("accept", "application/json")
		response, err := d.Client.Do(request)
		if err != nil {
			return false, fmt.Errorf("deezer: %s: %w", name, err)
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		response.Body.Close()
		if readErr != nil {
			return false, fmt.Errorf("deezer: %s: %w", name, readErr)
		}
		if (response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500) && attempt < 2 {
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return false, ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if response.StatusCode != http.StatusOK {
			// A 404 is "no such artist", which is an answer, not a failure. Any other status is the provider refusing
			// to answer, and must not become a decision.
			if response.StatusCode == http.StatusNotFound {
				return false, nil
			}
			return false, fmt.Errorf("deezer: %s: status %d", name, response.StatusCode)
		}
		var document struct {
			Data []struct {
				Name string `json:"name"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &document); err != nil {
			return false, fmt.Errorf("deezer: %s: %w", name, err)
		}
		want := model.Key(name)
		for _, artist := range document.Data {
			if model.Key(strings.TrimSpace(artist.Name)) == want && want != "" {
				return true, nil
			}
		}
		return false, nil
	}
}
