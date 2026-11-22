package genres

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/artifact"
	"github.com/jakobbjelver/messer-lineup/internal/clock"
	"github.com/jakobbjelver/messer-lineup/internal/index"
	"github.com/jakobbjelver/messer-lineup/internal/model"
)

// Format is the artifact's own version, separate from the index format: a reader that does not know `2` must refuse it
// rather than guess at the shape.
const Format = "lineup-genres/1"

// StateFormat versions the cache of raw tags. It carries the provider's answer, not ours, so a mapping change does not
// invalidate it — only a change to what is asked would.
const StateFormat = "lineup-genres-state/1"

// NegativeTTL is how long "this artist has no tags" is believed. A band can gain a page; a nightly that re-asks every
// unknown every night is a tax on the provider for an answer that almost never changes.
const NegativeTTL = 30 * 24 * time.Hour

// Provider is the tag source. Last.fm today; an interface so the mapping can be tested without a network.
type Provider interface {
	TopTags(ctx context.Context, artist string) ([]Tag, error)
}

// LastFM reads `artist.getTopTags`. One request per artist, 1/s, three tries on a throttle.
type LastFM struct {
	Key    string
	Client *http.Client
	Base   string
	Gap    time.Duration

	mu   sync.Mutex
	last time.Time
}

func NewLastFM(key string, client *http.Client) *LastFM {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &LastFM{Key: key, Client: client, Base: "https://ws.audioscrobbler.com/2.0/", Gap: time.Second}
}

func (l *LastFM) TopTags(ctx context.Context, artist string) ([]Tag, error) {
	if l.Key == "" {
		return nil, fmt.Errorf("genres: no Last.fm key")
	}
	gap := l.Gap
	if gap <= 0 {
		gap = time.Second
	}
	l.mu.Lock()
	wait := gap - time.Since(l.last)
	l.mu.Unlock()
	if wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	l.mu.Lock()
	l.last = clock.Now()
	l.mu.Unlock()

	base := l.Base
	if base == "" {
		base = "https://ws.audioscrobbler.com/2.0/"
	}
	query := url.Values{}
	query.Set("method", "artist.getTopTags")
	query.Set("artist", artist)
	query.Set("api_key", l.Key)
	query.Set("format", "json")
	query.Set("autocorrect", "1")
	target := base + "?" + query.Encode()

	for attempt := 0; attempt < 3; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("user-agent", "MesserLineup/0.2 (https://lineup.messer.wtf)")
		response, err := l.Client.Do(request)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		response.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if (response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500) && attempt < 2 {
			timer := time.NewTimer(1100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
			continue
		}
		var document struct {
			TopTags struct {
				Tag []struct {
					Name  string `json:"name"`
					Count any    `json:"count"`
				} `json:"tag"`
			} `json:"toptags"`
			Error   int    `json:"error"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(body, &document); err != nil {
			return nil, fmt.Errorf("genres: %s: %w", artist, err)
		}
		// Last.fm's error rides on a non-2xx status: 404 + {"error":6} is "no such artist", an answer, not a failure.
		if document.Error == 6 {
			return nil, nil
		}
		if document.Error != 0 {
			return nil, fmt.Errorf("genres: %s: %d %s", artist, document.Error, document.Message)
		}
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("genres: %s: status %d", artist, response.StatusCode)
		}
		tags := make([]Tag, 0, len(document.TopTags.Tag))
		for _, tag := range document.TopTags.Tag {
			tags = append(tags, Tag{Name: tag.Name, Count: countOf(tag.Count)})
		}
		return tags, nil
	}
	return nil, fmt.Errorf("genres: %s: provider did not answer", artist)
}

// countOf reads Last.fm's count, which is a number in the docs and a string in the JSON.
func countOf(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case string:
		number, _ := strconv.Atoi(typed)
		return number
	default:
		return 0
	}
}

// Entry is what the provider said about one artist, cached so a nightly only asks about names it has never seen.
type Entry struct {
	Name string    `json:"name"`
	Tags []Tag     `json:"tags"`
	At   time.Time `json:"at"`
}

// State is the cache. It is keyed by identity key, so a renamed artist is a new question (which is the point).
type State struct {
	Format string           `json:"format"`
	Map    map[string]Entry `json:"map"`
}

func emptyState() *State {
	return &State{Format: StateFormat, Map: map[string]Entry{}}
}

// LoadState reads the cache, or starts an empty one for a path that does not exist yet.
func LoadState(path string) (*State, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return emptyState(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("genres: reading %s: %w", path, err)
	}
	var state State
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("genres: %s is not a state file: %w", path, err)
	}
	if state.Format != StateFormat {
		return nil, fmt.Errorf("genres: %s is %q, want %q", path, state.Format, StateFormat)
	}
	if state.Map == nil {
		state.Map = map[string]Entry{}
	}
	return &state, nil
}

// Save writes the cache atomically: a killed nightly must not leave half a JSON file for the next one to read.
func (s *State) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return err
	}
	temp := path + ".tmp"
	file, err := os.Create(temp)
	if err != nil {
		return err
	}
	if _, err := file.Write(raw); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

// Report is what one build measured.
type Report struct {
	Artists   int     `json:"artists"`
	WithGenre int     `json:"with_genre"`
	Coverage  float64 `json:"coverage"`
	Fetched   int     `json:"fetched"`
	Failed    int     `json:"failed"`
	Pending   int     `json:"pending"`
	Unmapped  []Tag   `json:"unmapped,omitempty"`
}

// Artifact is the published document: both directions, because the board asks "what genre is this artist" per row and
// the feed asks "which artists are metal" once.
type Artifact struct {
	Format  string              `json:"format"`
	BuiltAt time.Time           `json:"built_at"`
	Artist  map[string][]string `json:"artist"`
	Genres  map[string][]string `json:"genres"`
}

// Options are one build's inputs.
type Options struct {
	IndexDir string
	State    *State
	Provider Provider
	Limit    int
	Now      time.Time
	// Log receives one line per fetch failure; nil silences it.
	Log io.Writer
}

// Build reads every published shard, asks the provider about artists it has never seen (bounded by Limit), maps the
// tags, and writes the artifact. It returns the artifact's path inside the index and what it measured.
func Build(ctx context.Context, opts Options) (string, Report, error) {
	report := Report{}
	if opts.State == nil {
		return "", report, fmt.Errorf("genres: no state")
	}
	if opts.Now.IsZero() {
		opts.Now = clock.Now()
	}
	manifest, err := index.ReadManifest(opts.IndexDir)
	if err != nil {
		return "", report, fmt.Errorf("genres: %w", err)
	}

	// Distinct artists, with the name the index publishes: a row's `a` is what the board shows and what Last.fm is
	// asked about. The key is the first segment of `k`, which is the identity the row already uses.
	names := map[string]string{}
	for _, entry := range manifest.Towns {
		rows, err := index.ReadShard(filepath.Join(opts.IndexDir, filepath.FromSlash(entry.Shard)))
		if err != nil {
			return "", report, fmt.Errorf("genres: %s: %w", entry.Shard, err)
		}
		for _, row := range rows {
			key := strings.SplitN(row.K, "|", 2)[0]
			if key == "" {
				key = model.Key(row.A)
			}
			if key != "" {
				names[key] = row.A
			}
		}
	}
	report.Artists = len(names)

	keys := make([]string, 0, len(names))
	for key := range names {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	fetched := 0
	for _, key := range keys {
		entry, known := opts.State.Map[key]
		// A negative expires; a positive does not.
		if known && (len(entry.Tags) > 0 || opts.Now.Sub(entry.At) < NegativeTTL) {
			continue
		}
		if opts.Limit > 0 && fetched >= opts.Limit {
			report.Pending++
			continue
		}
		if opts.Provider == nil {
			report.Pending++
			continue
		}
		tags, err := opts.Provider.TopTags(ctx, names[key])
		fetched++
		if err != nil {
			report.Failed++
			if opts.Log != nil {
				fmt.Fprintf(opts.Log, "genres: %s: %v\n", names[key], err)
			}
			continue
		}
		opts.State.Map[key] = Entry{Name: names[key], Tags: tags, At: opts.Now.UTC()}
	}
	report.Fetched = fetched

	document := Artifact{
		Format:  Format,
		BuiltAt: opts.Now.UTC(),
		Artist:  map[string][]string{},
		Genres:  map[string][]string{},
	}
	unmapped := make([]Tag, 0, 16)
	for key := range names {
		entry, ok := opts.State.Map[key]
		if !ok {
			continue
		}
		unmapped = append(unmapped, Unmapped(entry.Tags)...)
		buckets := Bucket(entry.Tags)
		if len(buckets) == 0 {
			continue
		}
		document.Artist[key] = buckets
		for _, bucket := range buckets {
			document.Genres[bucket] = append(document.Genres[bucket], key)
		}
	}
	report.WithGenre = len(document.Artist)
	if report.Artists > 0 {
		report.Coverage = float64(report.WithGenre) / float64(report.Artists)
	}
	for _, keys := range document.Genres {
		sort.Strings(keys)
	}
	report.Unmapped = topUnmapped(unmapped, 12)

	payload, err := json.Marshal(document)
	if err != nil {
		return "", report, err
	}
	name, err := artifact.Write(opts.IndexDir, "genres", payload)
	if err != nil {
		return "", report, err
	}
	return name, report, nil
}

// topUnmapped merges the per-artist unmapped tags into one list, most frequent first. A tag used by ten artists is a
// vocabulary gap; one used once is a typo in the long tail.
func topUnmapped(tags []Tag, limit int) []Tag {
	counts := map[string]int{}
	for _, tag := range tags {
		counts[tag.Name]++
	}
	out := make([]Tag, 0, len(counts))
	for name, count := range counts {
		out = append(out, Tag{Name: name, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
