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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/clock"
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

// --- Last.fm: the second taste source, and the only one that needs a key.
//
// ListenBrainz is keyless but rare among listeners; Last.fm is where most of them already are, and
// `user.getTopArtists` is a public read with an API key as the app's identity. The route therefore lives on the
// server (the key must not travel to the tab) and answers **names**, the same currency the import already speaks,
// so no second identity function has to exist.
//
// Last.fm reports failures in the body with HTTP 200, so the status code is not the answer:
//
//	error 6   user not found        a fact about the handle — remembered for a day
//	error 29  rate limited          a moment — remembered for five minutes
//	error 10/26 bad/suspended key   an operator problem, and still not a fact about the handle
//
// A twelve-month period is asked for because a taste board is about what someone listens to now: `overall` is a
// lifetime chart (a handle can be fifteen years of one genre), and the year window is the same one ListenBrainz is
// asked for, so the two imports mean the same thing.
const (
	defaultLastFM    = "https://ws.audioscrobbler.com/2.0/"
	lastfmTTL        = 6 * time.Hour
	lastfmMissingTTL = 24 * time.Hour
	lastfmFailedTTL  = 5 * time.Minute
	lastfmPeriod     = "12month"
)

// lastfmAnswer is why the route can tell "no such user" from "the provider blinked": the first is a 404, the
// second a 503. A client that mixes them up either hides a real account or claims one exists.
type lastfmAnswer int

const (
	lastfmOK lastfmAnswer = iota
	lastfmUnknownUser
	lastfmNotAnswered
)

type lastfmEntry struct {
	names  []string
	answer lastfmAnswer
	at     time.Time
	retry  time.Duration
}

// lastfmResolver remembers one handle's top artists for six hours. One per server; the edge caches separately.
type lastfmResolver struct {
	key      string
	endpoint string
	client   *http.Client
	now      func() time.Time

	mu      sync.Mutex
	entries map[string]lastfmEntry
}

func newLastFMResolver(endpoint, key string, now func() time.Time) *lastfmResolver {
	if endpoint == "" {
		endpoint = defaultLastFM
	}
	if !strings.HasSuffix(endpoint, "/") {
		endpoint += "/"
	}
	if now == nil {
		now = clock.Now
	}
	return &lastfmResolver{
		key:      key,
		endpoint: endpoint,
		client:   &http.Client{Timeout: 8 * time.Second},
		now:      now,
		entries:  map[string]lastfmEntry{},
	}
}

// topArtists answers with the provider's own spellings, in its own order, deduped by identity key.
func (l *lastfmResolver) topArtists(handle string) ([]string, lastfmAnswer) {
	handle = strings.TrimSpace(handle)
	if handle == "" {
		return nil, lastfmUnknownUser
	}
	l.mu.Lock()
	if entry, found := l.entries[handle]; found && l.now().Sub(entry.at) < entry.retry {
		l.mu.Unlock()
		return entry.names, entry.answer
	}
	l.mu.Unlock()

	names, answer, retry := l.fetch(handle)
	l.mu.Lock()
	l.entries[handle] = lastfmEntry{names: names, answer: answer, at: l.now(), retry: retry}
	l.mu.Unlock()
	return names, answer
}

type lastfmTop struct {
	TopArtists struct {
		Artist []struct {
			Name string `json:"name"`
		} `json:"artist"`
	} `json:"topartists"`
	Error   int    `json:"error"`
	Message string `json:"message"`
}

func (l *lastfmResolver) fetch(handle string) ([]string, lastfmAnswer, time.Duration) {
	query := url.Values{}
	query.Set("method", "user.getTopArtists")
	query.Set("user", handle)
	query.Set("api_key", l.key)
	query.Set("format", "json")
	query.Set("limit", strconv.Itoa(tasteMax))
	query.Set("period", lastfmPeriod)

	req, err := http.NewRequest(http.MethodGet, l.endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return nil, lastfmNotAnswered, lastfmFailedTTL
	}
	req.Header.Set("User-Agent", "MesserLineup/0.2 (https://lineup.messer.wtf)")
	req.Header.Set("Accept", "application/json")

	res, err := l.client.Do(req)
	if err != nil {
		return nil, lastfmNotAnswered, lastfmFailedTTL
	}
	defer func() { _ = res.Body.Close() }()

	// **Last.fm's error envelope rides on a non-2xx status.** A missing user answers `404` with
	// `{"error":6,"message":"User not found"}` (measured), and a 5xx carries an error code too — so a client that
	// returns on the status alone reports a real, named provider answer as "did not answer". The body is read first;
	// the status is only consulted when the body carries no error field at all.
	if res.StatusCode >= 500 {
		return nil, lastfmNotAnswered, lastfmFailedTTL
	}
	var body lastfmTop
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, lastfmNotAnswered, lastfmFailedTTL
	}
	if body.Error != 0 {
		if body.Error == 6 {
			return nil, lastfmUnknownUser, lastfmMissingTTL
		}
		return nil, lastfmNotAnswered, lastfmFailedTTL
	}
	if res.StatusCode != http.StatusOK {
		return nil, lastfmNotAnswered, lastfmFailedTTL
	}

	seen := make(map[string]bool, len(body.TopArtists.Artist))
	names := make([]string, 0, len(body.TopArtists.Artist))
	for _, artist := range body.TopArtists.Artist {
		name := strings.TrimSpace(artist.Name)
		if name == "" {
			continue
		}
		key := model.Key(name)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		names = append(names, name)
		if len(names) == tasteMax {
			break
		}
	}
	return names, lastfmOK, lastfmTTL
}
