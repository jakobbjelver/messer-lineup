package server

// A handle's top artists, for the calendar.
//
// A subscription's artist list is a snapshot in the token, and that is deliberate: a calendar is a promise about
// specific acts, not a mirror of what someone played last month. But a listener who wants the feed to follow their
// listening can say so — the token then carries a source (`s`), and this resolves it at fetch time.
//
// Two rules make that safe, and both are the reason this is not the default:
//
//   - **The snapshot stays.** `a` is written into the token at subscribe time and is used whenever this resolver
//     does not answer. A provider outage, a throttled call or a deleted handle degrades to the list the subscriber
//     saw, never to an empty calendar and never to the whole town.
//   - **An answer is not a failure.** ListenBrainz answers `204` for an account whose statistics have not been
//     computed yet — that is a real answer (nothing yet), and it is cached as one. `404` is a fact about the handle
//     (remembered for a day); a `5xx` or a timeout is a moment, and is retried sooner.
//
// Measured 2026-11-20 against the live API: `count=200` answers in ~1.4 s with 20 KB, the anonymous budget is 30
// requests per window (`x-ratelimit-limit`), and the list is a *listening rank*, which is why it must never be
// silently substituted for a chosen list — hence the fallback and the six-hour cache.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/model"
)

const (
	defaultListenBrainz = "https://api.listenbrainz.org/1/stats/user/"
	tasteTTL            = 6 * time.Hour
	tasteMissingTTL     = 24 * time.Hour
	tasteFailureTTL     = 5 * time.Minute
	tasteMax            = 200
)

type tasteStat struct {
	Payload struct {
		Artists []struct {
			Name string `json:"artist_name"`
		} `json:"artists"`
	} `json:"payload"`
}

type tasteEntry struct {
	keys []string
	// answered is whether the provider answered at all. A `204` and a real list are both answers; a timeout is not.
	answered bool
	at       time.Time
	retry    time.Duration
}

// tasteResolver remembers one handle's artists for six hours, and its failures for five minutes. One per server.
type tasteResolver struct {
	base   string
	client *http.Client
	now    func() time.Time

	mu      sync.Mutex
	entries map[string]tasteEntry
}

func newTasteResolver(base string, now func() time.Time) *tasteResolver {
	if base == "" {
		base = defaultListenBrainz
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	if now == nil {
		now = func() time.Time { return time.Time{} }
	}
	return &tasteResolver{
		base:    base,
		client:  &http.Client{Timeout: 8 * time.Second},
		now:     now,
		entries: map[string]tasteEntry{},
	}
}

// keys answers with identity keys, and whether the provider answered. A caller that gets `false` must use the
// token's snapshot: "I could not ask" is not "you have no artists".
func (t *tasteResolver) keys(handle string) ([]string, bool) {
	handle = strings.TrimSpace(handle)
	if handle == "" {
		return nil, false
	}
	t.mu.Lock()
	if entry, found := t.entries[handle]; found && t.now().Sub(entry.at) < entry.retry {
		t.mu.Unlock()
		return entry.keys, entry.answered
	}
	t.mu.Unlock()

	keys, answered, retry := t.fetch(handle)
	t.mu.Lock()
	t.entries[handle] = tasteEntry{keys: keys, answered: answered, at: t.now(), retry: retry}
	t.mu.Unlock()
	return keys, answered
}

func (t *tasteResolver) fetch(handle string) ([]string, bool, time.Duration) {
	endpoint := fmt.Sprintf("%s%s/artists?range=year&count=%d", t.base, url.PathEscape(handle), tasteMax)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false, tasteFailureTTL
	}
	req.Header.Set("User-Agent", "MesserLineup/0.2 (https://lineup.messer.wtf)")
	req.Header.Set("Accept", "application/json")

	res, err := t.client.Do(req)
	if err != nil {
		return nil, false, tasteFailureTTL
	}
	defer func() { _ = res.Body.Close() }()

	switch {
	case res.StatusCode == http.StatusNoContent:
		// No statistics computed yet: an answer, and an empty one.
		return nil, true, tasteTTL
	case res.StatusCode == http.StatusNotFound:
		return nil, false, tasteMissingTTL
	case res.StatusCode != http.StatusOK:
		return nil, false, tasteFailureTTL
	}

	var body tasteStat
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, false, tasteFailureTTL
	}
	seen := make(map[string]bool, len(body.Payload.Artists))
	keys := make([]string, 0, len(body.Payload.Artists))
	for _, artist := range body.Payload.Artists {
		name := strings.TrimSpace(artist.Name)
		if name == "" {
			continue
		}
		key := model.Key(name)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
		if len(keys) == tasteMax {
			break
		}
	}
	return keys, true, tasteTTL
}
