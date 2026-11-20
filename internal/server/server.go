// Package server serves a built index the way the edge does, so the board can be developed and tested against
// the same bytes a visitor will fetch.
//
// What it serves is deliberately narrow. The index files are served with the headers the bucket sets (a year and
// `immutable` for a shard, five minutes for the manifest), `/data/<town>.json` is a convenience for the SPA that
// is built from the shard rather than from anything else, and health is answered from the index itself. The six
// `/api/v1` routes and the calendar feed need accounts and taste providers (P5); until then they answer with the
// contract's error envelope and a 501, which is a truthful answer and keeps the shape tested.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/index"
	"github.com/jakobbjelver/messer-lineup/internal/model"
)

// Options configures a server.
type Options struct {
	IndexDir string // the built index to serve
	WebDir   string // optional SPA assets; empty means "no board yet", and that is answered honestly
	Now      func() time.Time
	Log      io.Writer
	// ArtistSearchURL overrides the MusicBrainz search endpoint. A test seam: production never sets it.
	ArtistSearchURL string
	// DeezerURL overrides the Deezer artist search behind the on-demand portraits. A test seam.
	DeezerURL string
}

// Handler builds the routes. It returns an error only when the index cannot be read at all — a server that can
// answer `/readyz` with a reason is more useful than one that refuses to start.
func Handler(opts Options) (http.Handler, error) {
	if opts.IndexDir == "" {
		return nil, errors.New("server: no index directory")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	manifest, err := index.ReadManifest(opts.IndexDir)
	if err != nil {
		return nil, fmt.Errorf("server: %w", err)
	}
	s := &server{
		opts:     opts,
		manifest: manifest,
		search:   newArtistSearcher(opts.ArtistSearchURL),
		faces:    newFaceIndex(opts.DeezerURL, opts.Now),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("GET /manifest.json", s.manifestFile)
	mux.HandleFunc("GET /towns.json", s.townsFile)
	mux.HandleFunc("GET /towns/", s.shard)
	mux.HandleFunc("GET /artists/", s.artistsFile)
	mux.HandleFunc("GET /data/", s.townData)
	mux.HandleFunc("GET /where", s.where)
	mux.HandleFunc("GET /event/", s.event)
	mux.HandleFunc("GET /api/v1/artists/search", s.artistSearch)
	// Registered above the wildcard like every specific route: Go's mux would route it correctly anyway, but the
	// order is the habit that keeps the Hono side honest.
	mux.HandleFunc("GET /api/v1/artists/portraits", s.artistPortraits)
	mux.HandleFunc("/api/v1/", s.apiNotYet)
	mux.HandleFunc("GET /calendar/{file}", s.calendar)
	mux.HandleFunc("/", s.web)
	return mux, nil
}

type server struct {
	opts     Options
	manifest model.Manifest
	search   *artistSearcher
	faces    *faceIndex
}

// health is liveness only: the process answers. It says nothing about the index, which is /readyz's job.
func (s *server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "as_of": s.opts.Now().UTC()})
}

// ready re-reads the index and verifies it. A board that loads a broken index is worse than one that says so.
func (s *server) ready(w http.ResponseWriter, r *http.Request) {
	report, err := index.Verify(s.opts.IndexDir, s.opts.Now())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "index_unreadable", err.Error())
		return
	}
	if !report.OK() {
		problems := make([]string, 0, len(report.Problems))
		for _, problem := range report.Problems {
			problems = append(problems, problem.String())
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok": false, "problems": problems, "towns": report.Towns, "rows": report.Rows,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "format": report.Format, "towns": report.Towns, "rows": report.Rows,
		"built_at": report.BuiltAt, "window": report.Window,
	})
}

// manifestFile serves the manifest with the cache header the bucket sets, so a local run cannot drift from the
// edge in a way that only shows up in production.
func (s *server) manifestFile(w http.ResponseWriter, r *http.Request) {
	s.serveFile(w, r, "manifest.json", "application/json; charset=utf-8", metaCache)
}

func (s *server) townsFile(w http.ResponseWriter, r *http.Request) {
	s.serveFile(w, r, "towns.json", "application/json; charset=utf-8", metaCache)
}

// artistsFile serves the portraits artifact (and nothing else) with the same immutable header the bucket sets, so
// a local run and the edge hand the board the same bytes. An allowlist, not a directory: the index directory also
// holds reports and state, and a static handler over it would publish whatever a run happened to leave behind.
func (s *server) artistsFile(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/artists/")
	if name != "portraits.json" {
		writeError(w, http.StatusNotFound, "unknown_artifact", "that artifact is not published")
		return
	}
	s.serveFile(w, r, filepath.Join("artists", name), "application/json; charset=utf-8", shardCache)
}

// shard serves one immutable shard. The name is content-addressed, so the path is checked against the manifest
// rather than the filesystem: an arbitrary file in that directory is not part of the index.
func (s *server) shard(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/towns/")
	for _, entry := range s.manifest.Towns {
		if strings.HasSuffix(entry.Shard, "/"+name) && name != "" {
			s.serveFile(w, r, filepath.Join("towns", filepath.Base(name)), "application/x-ndjson", shardCache)
			return
		}
	}
	writeError(w, http.StatusNotFound, "unknown_shard", "that shard is not in the manifest")
}

// townData is the SPA's convenience view: one town's rows as a JSON array, decoded from the same shard the client
// would fetch. It exists so a browser can render without a gzip decoder in its fetch path.
func (s *server) townData(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/data/"), ".json")
	var entry *model.Index
	for i := range s.manifest.Towns {
		if s.manifest.Towns[i].Town == id {
			entry = &s.manifest.Towns[i]
		}
	}
	if entry == nil {
		writeError(w, http.StatusNotFound, "unknown_town", fmt.Sprintf("%q is not a published town", id))
		return
	}
	rows, err := index.ReadShard(filepath.Join(s.opts.IndexDir, filepath.FromSlash(entry.Shard)))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "shard_unreadable", err.Error())
		return
	}
	w.Header().Set("Cache-Control", metaCache)
	writeJSON(w, http.StatusOK, map[string]any{
		"format":   s.manifest.Format,
		"town":     entry.Town,
		"sources":  entry.Sources,
		"built_at": entry.BuiltAt,
		"rows":     rows,
	})
}

// apiNotYet answers the routes that need accounts. The envelope is the contract, so it is worth getting right
// even when the status is 501: a client written against it will not need changing when the route is real.
func (s *server) apiNotYet(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "not_implemented",
		fmt.Sprintf("%s arrives with the taste providers (P5); this build serves the index only", r.URL.Path))
}

// web serves the board when there is one. Without assets every other path is a 404 that says why, rather than a
// blank page or a directory listing.
func (s *server) web(w http.ResponseWriter, r *http.Request) {
	if s.opts.WebDir == "" {
		writeError(w, http.StatusNotFound, "no_board", "this server has no board: pass --web <dir> once the SPA is built")
		return
	}
	clean := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if clean == "." || clean == "" {
		clean = "index.html"
	}
	path := filepath.Join(s.opts.WebDir, clean)
	// A path that escapes the web root is not a page: the SPA fallback below must not become a file read.
	if !strings.HasPrefix(path, filepath.Clean(s.opts.WebDir)+string(os.PathSeparator)) {
		writeError(w, http.StatusNotFound, "not_found", "no such page")
		return
	}
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		http.ServeFile(w, r, path)
		return
	}
	// History fallback: a query-parameter route renders the same document.
	http.ServeFile(w, r, filepath.Join(s.opts.WebDir, "index.html"))
}

const (
	shardCache = "public, max-age=31536000, immutable"
	metaCache  = "public, max-age=300"
)

func (s *server) serveFile(w http.ResponseWriter, r *http.Request, name, contentType, cache string) {
	path := filepath.Join(s.opts.IndexDir, name)
	info, err := os.Stat(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("%s is not in this index", name))
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", cache)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	http.ServeFile(w, r, path)
}

// writeJSON is the only way a handler writes a body, so the encoding cannot drift between routes.
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(value)
}

// writeError is the contract's error shape: {"error":{"code","message"}}.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

// Run serves until the context is cancelled.
func Run(ctx context.Context, opts Options, addr string) error {
	handler, err := Handler(opts)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	if opts.Log != nil {
		fmt.Fprintf(opts.Log, "lineup serve: http://%s (index %s)\n", ln.Addr(), opts.IndexDir)
	}
	errs := make(chan error, 1)
	go func() { errs <- srv.Serve(ln) }()
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdown)
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
