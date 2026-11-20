package server

// Artist search, proxied.
//
// The browser could call MusicBrainz directly — it sends `Access-Control-Allow-Origin: *` — but a combobox is
// exactly the client that provokes a `503 busy`: MusicBrainz answered a second call within one second with that
// status while this was being written. So the proxy exists for two reasons, and neither is CORS: a cache keyed by
// the normalised query (six hours), and one upstream call at a time.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// defaultMusicBrainz is the search endpoint. A field on Options overrides it in tests.
const defaultMusicBrainz = "https://musicbrainz.org/ws/2/artist"

// artistHit is one search result. Only fields the UI can name are carried; a score below the cut is dropped.
type artistHit struct {
	Name           string `json:"name"`
	MBID           string `json:"mbid"`
	Type           string `json:"type,omitempty"`
	Country        string `json:"country,omitempty"`
	Disambiguation string `json:"disambiguation,omitempty"`
	Score          int    `json:"score,omitempty"`
}

type artistAnswer struct {
	Artists     []artistHit `json:"artists"`
	Unavailable bool        `json:"unavailable,omitempty"`
}

type cacheEntry struct {
	answer artistAnswer
	at     time.Time
}

// artistSearcher serialises upstream calls and remembers answers. One per server.
type artistSearcher struct {
	upstream string
	client   *http.Client
	gap      time.Duration

	mu       sync.Mutex
	cache    map[string]cacheEntry
	lastCall time.Time
}

func newArtistSearcher(upstream string) *artistSearcher {
	if upstream == "" {
		upstream = defaultMusicBrainz
	}
	return &artistSearcher{
		upstream: upstream,
		client:   &http.Client{Timeout: 8 * time.Second},
		gap:      searchGap,
		cache:    map[string]cacheEntry{},
	}
}

const (
	searchTTL      = 6 * time.Hour
	searchGap      = 1100 * time.Millisecond
	searchCacheMax = 512
)

func normalizeQuery(raw string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(raw))), " ")
}

// lookup returns the cached answer or asks upstream, never more than once per searchGap. A query under two
// characters is answered here as well as at the route, so no caller can spend the budget on noise.
func (a *artistSearcher) lookup(q string) artistAnswer {
	if len([]rune(q)) < 2 {
		return artistAnswer{Artists: []artistHit{}}
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	if entry, ok := a.cache[q]; ok && time.Since(entry.at) < searchTTL {
		return entry.answer
	}
	if wait := a.gap - time.Since(a.lastCall); wait > 0 {
		time.Sleep(wait)
	}
	a.lastCall = time.Now()

	answer := a.fetch(q)
	if len(a.cache) >= searchCacheMax {
		a.cache = map[string]cacheEntry{}
	}
	a.cache[q] = cacheEntry{answer: answer, at: time.Now()}
	return answer
}

func (a *artistSearcher) fetch(q string) artistAnswer {
	endpoint := fmt.Sprintf("%s?query=%s&fmt=json&limit=6", a.upstream, url.QueryEscape(q))
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return artistAnswer{Artists: []artistHit{}, Unavailable: true}
	}
	// MusicBrainz asks every client to identify itself; a bare Go UA is one of the ways to earn a block.
	req.Header.Set("User-Agent", "MesserLineup/0.2 (https://lineup.messer.wtf)")
	req.Header.Set("Accept", "application/json")

	res, err := a.client.Do(req)
	if err != nil {
		return artistAnswer{Artists: []artistHit{}, Unavailable: true}
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		// A 503 here is the rate limiter, and it is a state the picker knows how to show.
		_, _ = io.Copy(io.Discard, res.Body)
		return artistAnswer{Artists: []artistHit{}, Unavailable: true}
	}

	var body struct {
		Artists []struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			Type           string `json:"type"`
			Country        string `json:"country"`
			Disambiguation string `json:"disambiguation"`
			Score          int    `json:"score"`
		} `json:"artists"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return artistAnswer{Artists: []artistHit{}, Unavailable: true}
	}

	hits := make([]artistHit, 0, len(body.Artists))
	for _, artist := range body.Artists {
		if artist.Name == "" || artist.ID == "" || artist.Score < 60 {
			continue
		}
		hits = append(hits, artistHit{
			Name:           artist.Name,
			MBID:           artist.ID,
			Type:           strings.ToLower(artist.Type),
			Country:        artist.Country,
			Disambiguation: artist.Disambiguation,
			Score:          artist.Score,
		})
		if len(hits) == 6 {
			break
		}
	}
	return artistAnswer{Artists: hits}
}

// artistSearch is the route. A query under two characters is an empty answer, not an error: the picker calls it
// on every keystroke worth asking about.
func (s *server) artistSearch(w http.ResponseWriter, r *http.Request) {
	q := normalizeQuery(r.URL.Query().Get("q"))
	if len([]rune(q)) < 2 {
		writeJSON(w, http.StatusOK, artistAnswer{Artists: []artistHit{}})
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=21600")
	writeJSON(w, http.StatusOK, s.search.lookup(q))
}
