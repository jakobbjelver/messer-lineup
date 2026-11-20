package server

// Faces on demand: a portrait for an artist the published file has never heard of.
//
// The board's own faces come from the published artifact (`artists/portraits.json`) and cost nothing to render. The
// artist picker is the other case entirely: its rows are MusicBrainz search hits, most of them artists the index
// has no row for, so the published file answers nothing and every row wore two initials. This route asks Deezer —
// keyless, one call, `picture_xl` — for the names the picker actually shows, and caches the answer for a week.
//
// Three rules carried over from the miner, because they were learned there:
//
//   - **Identity or nothing.** `portraits.PortraitFor` refuses a namesake, so the worst case is initials, never a
//     stranger's face.
//   - **A miss is a fact, a failure is not.** A confirmed miss is remembered for 30 days (the artist has no
//     picture); a transport failure is remembered for no time at all and reported as `unavailable`, because a 503
//     is not a property of the artist.
//   - **One upstream call at a time**, with a gap. Deezer is generous but not infinite, and a combobox in flight
//     asks about six names at once.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/model"
	"github.com/jakobbjelver/messer-lineup/internal/portraits"

	"github.com/jakobbjelver/messer-lineup/internal/clock"
)

const (
	defaultDeezerSearch = "https://api.deezer.com/search/artist?q="
	faceHitTTL          = 7 * 24 * time.Hour
	faceMissTTL         = 30 * 24 * time.Hour
	faceGap             = 250 * time.Millisecond
	faceMaxNames        = 8
)

type faceEntry struct {
	portrait portraits.Portrait
	hit      bool
	at       time.Time
}

// faceAnswer is the route's body. Portraits are keyed by the **name the caller sent**, not by `model.Key`, because
// the two runtimes would then have to agree on the whole identity function to read one picture; the client already
// knows the name it asked about and maps it to its own key. `unavailable` says Deezer failed rather than answered.
type faceAnswer struct {
	Portraits   map[string]portraits.Portrait `json:"portraits"`
	Unavailable bool                          `json:"unavailable,omitempty"`
}

// faceIndex remembers pictures and misses, and serialises the calls that fill it. One per server.
type faceIndex struct {
	endpoint string
	client   *http.Client
	gap      time.Duration
	now      func() time.Time

	mu       sync.Mutex
	cache    map[string]faceEntry
	lastCall time.Time
}

func newFaceIndex(endpoint string, now func() time.Time) *faceIndex {
	if endpoint == "" {
		endpoint = defaultDeezerSearch
	}
	if now == nil {
		now = clock.Now
	}
	return &faceIndex{
		endpoint: endpoint,
		client:   &http.Client{Timeout: 8 * time.Second},
		gap:      faceGap,
		now:      now,
		cache:    map[string]faceEntry{},
	}
}

// lookup answers for the names it can, and says so when Deezer did not answer at all.
func (f *faceIndex) lookup(ctx context.Context, names []string) faceAnswer {
	answer := faceAnswer{Portraits: map[string]portraits.Portrait{}}
	if len(names) == 0 {
		return answer
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	for _, name := range names {
		key := model.Key(name)
		if key == "" {
			continue
		}
		if entry, ok := f.cache[key]; ok {
			if entry.hit && f.now().Sub(entry.at) < faceHitTTL {
				answer.Portraits[name] = entry.portrait
				continue
			}
			if !entry.hit && f.now().Sub(entry.at) < faceMissTTL {
				continue
			}
		}

		if wait := f.gap - f.now().Sub(f.lastCall); wait > 0 {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return answer
			}
		}
		f.lastCall = f.now()

		portrait, hit, err := f.fetch(ctx, name)
		if err != nil {
			// Neither cached nor claimed: the client may ask again, and the next ask may be the whole answer.
			answer.Unavailable = true
			continue
		}
		f.cache[key] = faceEntry{portrait: portrait, hit: hit, at: f.now()}
		if hit {
			answer.Portraits[name] = portrait
		}
	}
	return answer
}

// fetch is one Deezer artist search. A 404 and an empty list are both "no such picture"; anything else that is not
// a 200 is a failure, not a miss.
func (f *faceIndex) fetch(ctx context.Context, name string) (portraits.Portrait, bool, error) {
	target := f.endpoint + url.QueryEscape(name) + "&limit=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return portraits.Portrait{}, false, err
	}
	req.Header.Set("User-Agent", "MesserLineup/0.2 (https://lineup.messer.wtf)")
	req.Header.Set("Accept", "application/json")

	res, err := f.client.Do(req)
	if err != nil {
		return portraits.Portrait{}, false, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return portraits.Portrait{}, false, nil
	}
	if res.StatusCode != http.StatusOK {
		return portraits.Portrait{}, false, fmt.Errorf("deezer: status %d", res.StatusCode)
	}

	var body struct {
		Data []struct {
			ID       int64  `json:"id"`
			Name     string `json:"name"`
			PictureX string `json:"picture_xl"`
		} `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return portraits.Portrait{}, false, err
	}
	if len(body.Data) == 0 {
		return portraits.Portrait{}, false, nil
	}
	artist := body.Data[0]
	portrait, ok := portraits.PortraitFor(artist.ID, artist.Name, artist.PictureX, name)
	return portrait, ok, nil
}

// readFaceNames trims, dedupes and bounds the query: eight names is one combobox's worth, and a caller cannot ask
// this process to walk a list.
func readFaceNames(raw []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, value := range raw {
		name := strings.Join(strings.Fields(value), " ")
		if len([]rune(name)) < 2 || len([]rune(name)) > 120 {
			continue
		}
		key := model.Key(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
		if len(out) == faceMaxNames {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return model.Key(out[i]) < model.Key(out[j]) })
	return out
}

// artistPortraits is the route. No name is an empty answer, not an error: the picker asks per keystroke worth
// asking about.
func (s *server) artistPortraits(w http.ResponseWriter, r *http.Request) {
	names := readFaceNames(r.URL.Query()["n"])
	answer := s.faces.lookup(r.Context(), names)
	if answer.Unavailable {
		w.Header().Set("Cache-Control", "no-store")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=86400")
	}
	writeJSON(w, http.StatusOK, answer)
}
