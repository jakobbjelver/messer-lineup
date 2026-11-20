// Package portraits resolves an artist portrait for every artist the index publishes.
//
// The artifact is deliberately separate from the event row (ADR-002): a row is nine keys of event fact, and a
// portrait is neither an event fact nor licensed by the ticketer. So the miner resolves `artistKey → image` once,
// publishes it beside the shards, and the board treats it as an enhancement — every avatar has initials to fall
// back on, and a build with no portraits at all is a supported state.
//
// The source is MusicBrainz's own `image` relations, which point straight at Wikimedia Commons files; the ADR's
// Wikidata hop is not needed (measured 2026-11-19). The Commons `Special:FilePath` URL is resolved to its final
// `thumb.wikimedia.org` form at mine time, because that URL is content-addressed and cacheable for a year.
package portraits

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/index"
	"github.com/jakobbjelver/messer-lineup/internal/model"
)

// File is the published artifact's path inside the index directory.
const File = "artists/portraits.json"

// Portrait is one artist's image: the resolved thumbnail and the Commons page that credits it.
type Portrait struct {
	URL    string `json:"u"`
	Credit string `json:"c,omitempty"`
}

// stateEntry remembers a resolved portrait or a miss, so a nightly run only asks about new artists.
type stateEntry struct {
	Portrait *Portrait `json:"p,omitempty"`
	Miss     bool      `json:"m,omitempty"`
	At       time.Time `json:"at"`
}

// Options is one run.
type Options struct {
	IndexDir     string
	StatePath    string   // default: .lineup-state/portraits.json beside the index
	Out          string   // default: <index>/artists/portraits.json
	Limit        int      // stop after N new artists (0 = all); a budget, not a page
	Only         []string // resolve these names instead of the index: a probe, not a build
	DryRun       bool
	Log          io.Writer
	Client       *http.Client
	Gap          time.Duration // minimum gap between upstream calls
	Now          time.Time
	BaseURL      string // MusicBrainz search; a test seam
	LookupURL    string // MusicBrainz artist lookup; a test seam
	CommonsURL   string // Special:FilePath base; a test seam
	WikipediaURL string // Wikipedia summary; a test seam
}

// Report is what a run did.
type Report struct {
	Indexed   int    `json:"indexed"`
	Cached    int    `json:"cached"`
	Requested int    `json:"requested"`
	Found     int    `json:"found"`
	Missed    int    `json:"missed"`
	Retry     int    `json:"retry"`
	Written   int    `json:"written"`
	Bytes     int64  `json:"bytes"`
	Path      string `json:"path"`
	DryRun    bool   `json:"dry_run"`
}

// outcome separates a fact ("this artist has no image relation") from a transport failure. A miss is remembered
// for a month; a failure is not remembered at all, because a 503 is not a property of the artist.
type outcome int

const (
	none outcome = iota
	found
	retry
)

// musicDescription is the vocabulary a Wikipedia lead page must use before its image may be an artist's portrait.
var musicDescription = regexp.MustCompile(`(?i)\b(band|musician|singer|songwriter|composer|rapper|dj|duo|trio|quartet|group|orchestra|ensemble|producer|vocalist|guitarist|drummer|bassist|pianist|conductor|singer-songwriter)\b`)

const (
	defaultBase      = "https://musicbrainz.org/ws/2/artist?query="
	defaultLookup    = "https://musicbrainz.org/ws/2/artist/"
	defaultCommons   = "https://commons.wikimedia.org/wiki/Special:FilePath/"
	defaultWikipedia = "https://en.wikipedia.org/api/rest_v1/page/summary/"
	missTTL          = 30 * 24 * time.Hour
	userAgent        = "MesserLineup/0.2 (https://lineup.messer.wtf)"
)

// Run resolves every artist the published shards name.
func Run(ctx context.Context, opts Options) (Report, error) {
	rep := Report{DryRun: opts.DryRun}
	if opts.IndexDir == "" {
		return rep, errors.New("portraits: no index directory")
	}
	if opts.Out == "" {
		opts.Out = filepath.Join(opts.IndexDir, File)
	}
	if opts.StatePath == "" {
		opts.StatePath = filepath.Join(filepath.Dir(filepath.Clean(opts.IndexDir)), ".lineup-state", "portraits.json")
	}
	if opts.Gap == 0 {
		opts.Gap = 1500 * time.Millisecond
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: 20 * time.Second}
	}
	if opts.BaseURL == "" {
		opts.BaseURL = defaultBase
	}
	if opts.LookupURL == "" {
		opts.LookupURL = defaultLookup
	}
	if opts.CommonsURL == "" {
		opts.CommonsURL = defaultCommons
	}
	if opts.WikipediaURL == "" {
		opts.WikipediaURL = defaultWikipedia
	}
	logf := func(format string, args ...any) {
		if opts.Log != nil {
			fmt.Fprintf(opts.Log, format+"\n", args...)
		}
	}

	artists, err := artistsIn(opts.IndexDir)
	if err != nil {
		return rep, err
	}
	if len(opts.Only) > 0 {
		// A named probe: resolve exactly these, ignore the shards. Used to check the pipeline against a known
		// artist without waiting on five hundred of them.
		artists = map[string]artistEntry{}
		for _, name := range opts.Only {
			if name = strings.TrimSpace(name); name != "" {
				artists[model.Key(name)] = artistEntry{name: name, count: 1}
			}
		}
	}
	rep.Indexed = len(artists)

	state := loadState(opts.StatePath)
	result := map[string]Portrait{}
	for key, entry := range state {
		if entry.Portrait != nil {
			result[key] = *entry.Portrait
		}
	}

	keys := make([]string, 0, len(artists))
	for key := range artists {
		keys = append(keys, key)
	}
	// Most-booked first: the artists with the most rows are the ones the board shows most often, so a run with a
	// budget spends it on the faces a reader will actually see.
	sort.Slice(keys, func(i, j int) bool {
		if artists[keys[i]].count != artists[keys[j]].count {
			return artists[keys[i]].count > artists[keys[j]].count
		}
		return keys[i] < keys[j]
	})

	fresh := 0
	for _, key := range keys {
		if _, done := state[key]; done {
			rep.Cached++
			continue
		}
		if opts.Limit > 0 && fresh >= opts.Limit {
			break
		}
		fresh++
		rep.Requested++
		portrait, kind := resolve(ctx, opts, artists[key].name, logf)
		switch kind {
		case found:
			state[key] = stateEntry{Portrait: &portrait, At: opts.Now.UTC()}
			result[key] = portrait
			rep.Found++
		case none:
			state[key] = stateEntry{Miss: true, At: opts.Now.UTC()}
			rep.Missed++
		default:
			// Not remembered: the next run asks again, because the artist did not answer the question.
			rep.Retry++
		}
		if fresh%20 == 0 {
			if err := saveState(opts.StatePath, state); err != nil {
				logf("portraits: saving state: %v", err)
			}
		}
	}
	if err := saveState(opts.StatePath, state); err != nil {
		return rep, err
	}

	rep.Written = len(result)
	rep.Path = opts.Out
	if opts.DryRun {
		return rep, nil
	}

	// The published file holds only the portraits that exist: a miss is state, not an entry.
	encoded, err := json.MarshalIndent(result, "", " ")
	if err != nil {
		return rep, err
	}
	encoded = append(encoded, '\n')
	if err := os.MkdirAll(filepath.Dir(opts.Out), 0o755); err != nil {
		return rep, err
	}
	if err := os.WriteFile(opts.Out, encoded, 0o644); err != nil {
		return rep, err
	}
	rep.Bytes = int64(len(encoded))
	return rep, nil
}

type artistEntry struct {
	name  string
	count int
}

// artistsIn reads every shard the manifest names and returns the distinct artist key → its spelling and weight.
func artistsIn(dir string) (map[string]artistEntry, error) {
	manifest, err := index.ReadManifest(dir)
	if err != nil {
		return nil, err
	}
	seen := map[string]artistEntry{}
	for _, town := range manifest.Towns {
		rows, err := index.ReadShard(filepath.Join(dir, filepath.FromSlash(town.Shard)))
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if row.A == "" {
				continue
			}
			// Clean before keying: a shard mined before the title fix still carries "1/28 NAME | VENUE", and a
			// portrait keyed on that name could never match the cleaned row the next sweep publishes.
			name := model.CleanArtistName(row.A, row.V)
			key := model.Key(name)
			// The longest spelling wins: the same key never has two spellings in practice, so this only keeps the
			// choice deterministic across towns.
			current := seen[key]
			current.count++
			if current.name == "" || len(name) > len(current.name) {
				current.name = name
			}
			seen[key] = current
		}
	}
	return seen, nil
}

// resolve is one artist: search, look up the relations, pick an image, and resolve its final URL.
func resolve(ctx context.Context, opts Options, name string, logf func(string, ...any)) (Portrait, outcome) {
	var search struct {
		Artists []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Score int    `json:"score"`
		} `json:"artists"`
	}
	call := func() error {
		return getJSON(ctx, opts, opts.BaseURL+url.QueryEscape(name)+"&fmt=json&limit=1", &search)
	}
	if err := call(); err != nil {
		logf("  ? %s: search: %v", name, err)
		return Portrait{}, retry
	}
	if len(search.Artists) == 0 {
		return Portrait{}, none
	}
	hit := search.Artists[0]
	// Only an identity match becomes a portrait. A bill ("Archspire + Crown Magnetar + …"), a festival pass
	// ("15 BANEFF 2026 PASS") and a lecture all have plausible-looking search hits, and a fuzzy score is how a
	// board ends up with the wrong face on the wrong row.
	if model.Key(hit.Name) != model.Key(name) {
		return Portrait{}, none
	}

	// Two sources, best first. MusicBrainz's own `image` relations are the most precise (Björk carries nine), but
	// they are rare: a small band usually has none (measured — two exact-name hits, zero image relations). So the
	// fallback is the artist's Wikipedia lead image, which is one call and is the same Commons file behind a
	// different door. Both are credited; a miss is a miss and the avatar keeps its initials.
	portrait, kind := fromRelations(ctx, opts, hit.ID, logf, name)
	if kind != none {
		return portrait, kind
	}
	return fromWikipedia(ctx, opts, name, logf)
}

// fromRelations is the MusicBrainz path: the first `image` relation that points at a Commons file.
func fromRelations(ctx context.Context, opts Options, mbid string, logf func(string, ...any), name string) (Portrait, outcome) {
	var artist struct {
		Relations []struct {
			Type string `json:"type"`
			URL  struct {
				Resource string `json:"resource"`
			} `json:"url"`
		} `json:"relations"`
	}
	if err := getJSON(ctx, opts, opts.LookupURL+mbid+"?inc=url-rels&fmt=json", &artist); err != nil {
		logf("  ? %s: lookup: %v", name, err)
		return Portrait{}, retry
	}
	for _, relation := range artist.Relations {
		if relation.Type != "image" {
			continue
		}
		file, ok := commonsFile(relation.URL.Resource)
		if !ok {
			continue
		}
		final, err := resolveThumb(ctx, opts, file)
		if err != nil {
			logf("  ? %s: thumbnail: %v", name, err)
			return Portrait{}, retry
		}
		return Portrait{URL: final, Credit: relation.URL.Resource}, found
	}
	return Portrait{}, none
}

// fromWikipedia is the fallback: the article's own lead image, which lives on the same Commons CDN.
func fromWikipedia(ctx context.Context, opts Options, name string, logf func(string, ...any)) (Portrait, outcome) {
	var summary struct {
		Type        string `json:"type"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Thumbnail   struct {
			Source string `json:"source"`
		} `json:"thumbnail"`
		ContentURLs struct {
			Desktop struct {
				Page string `json:"page"`
			} `json:"desktop"`
		} `json:"content_urls"`
	}
	target := opts.WikipediaURL + url.PathEscape(strings.ReplaceAll(name, " ", "_"))
	if err := getJSON(ctx, opts, target, &summary); err != nil {
		// A 404 is "no article", not a failure of the run.
		if strings.Contains(err.Error(), "status 404") {
			return Portrait{}, none
		}
		logf("  ? %s: wikipedia: %v", name, err)
		return Portrait{}, retry
	}
	// A disambiguation page has no lead image that belongs to this artist.
	if summary.Type != "standard" || summary.Thumbnail.Source == "" {
		return Portrait{}, none
	}
	// A Wikipedia title is not a search: "Fatherland" resolves to the concept (and its lead image is a Delacroix
	// painting), "Wicker" to woven material. Only an article that says it is about music may supply a face. This
	// is the check that keeps a wrong portrait off a row, which is worse than no portrait at all.
	if !musicDescription.MatchString(summary.Description) {
		return Portrait{}, none
	}
	if !strings.HasPrefix(summary.Thumbnail.Source, "https://upload.wikimedia.org/") &&
		!strings.HasPrefix(summary.Thumbnail.Source, "https://thumb.wikimedia.org/") {
		return Portrait{}, none
	}
	// The API appends campaign parameters; a content-addressed CDN path is the whole URL, and the query only
	// fragments the cache.
	source := summary.Thumbnail.Source
	if parsed, err := url.Parse(source); err == nil {
		parsed.RawQuery = ""
		source = parsed.String()
	}
	return Portrait{URL: source, Credit: summary.ContentURLs.Desktop.Page}, found
}

// getJSON is one upstream GET, politely spaced from the previous one. A 5xx or a transport failure is retried
// with a widening backoff: MusicBrainz's "busy" answer is intermittent (measured), not a verdict on the artist.
func getJSON(ctx context.Context, opts Options, target string, into any) error {
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		time.Sleep(opts.Gap)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/json")
		res, err := opts.Client.Do(req)
		if err != nil {
			last = err
			time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
			continue
		}
		if res.StatusCode != http.StatusOK {
			_, _ = io.Copy(io.Discard, res.Body)
			res.Body.Close()
			last = fmt.Errorf("status %d", res.StatusCode)
			if res.StatusCode < 500 {
				return last
			}
			time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
			continue
		}
		err = json.NewDecoder(res.Body).Decode(into)
		res.Body.Close()
		return err
	}
	return last
}

// resolveThumb follows `Special:FilePath` to the thumbnail URL that the CDN actually serves.
func resolveThumb(ctx context.Context, opts Options, file string) (string, error) {
	time.Sleep(opts.Gap)
	target := opts.CommonsURL + url.PathEscape(file) + "?width=240"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	res, err := opts.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", res.StatusCode)
	}
	if !strings.HasPrefix(res.Header.Get("Content-Type"), "image/") {
		return "", fmt.Errorf("not an image: %s", res.Header.Get("Content-Type"))
	}
	if res.Request == nil || res.Request.URL == nil {
		return "", errors.New("no final URL")
	}
	return res.Request.URL.String(), nil
}

// commonsFile turns `https://commons.wikimedia.org/wiki/File:Bj%C3%B6rk_1997.jpg` into the file name.
func commonsFile(resource string) (string, bool) {
	const marker = "/wiki/File:"
	at := strings.Index(resource, marker)
	if at < 0 {
		return "", false
	}
	name, err := url.PathUnescape(resource[at+len(marker):])
	if err != nil || strings.TrimSpace(name) == "" {
		return "", false
	}
	return name, true
}

func loadState(path string) map[string]stateEntry {
	state := map[string]stateEntry{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return state
	}
	_ = json.Unmarshal(raw, &state)
	for key, entry := range state {
		// A miss expires: a new MusicBrainz release or a new relation is worth re-asking about.
		if entry.Miss && time.Since(entry.At) > missTTL {
			delete(state, key)
		}
	}
	return state
}

func saveState(path string, state map[string]stateEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}
