package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/index"
	"github.com/jakobbjelver/messer-lineup/internal/model"

	"github.com/jakobbjelver/messer-lineup/internal/clock"
)

// buildIndex writes a two-town index and returns its directory.
func buildIndex(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	day := time.Date(2026, 12, 2, 20, 0, 0, 0, time.UTC)
	shows := []model.Show{
		{Artist: "Anna von Hausswolff", ArtistKey: model.Key("Anna von Hausswolff"), Venue: "Vega", VenueKey: model.Key("Vega"),
			Town: "copenhagen-dk", TownName: "København", Start: day, Status: model.OnSale, URL: "https://t.test/1", Sources: []string{"tickster"}},
		{Artist: "Efterklang", ArtistKey: model.Key("Efterklang"), Venue: "Pumpehuset", VenueKey: model.Key("Pumpehuset"),
			Town: "copenhagen-dk", TownName: "København", Start: day, Status: model.OnSale, URL: "https://t.test/2", Sources: []string{"tickster"}},
		{Artist: "Trentemøller", ArtistKey: model.Key("Trentemøller"), Venue: "Vega", VenueKey: model.Key("Vega"),
			Town: "stockholm-se", TownName: "Stockholm", Start: day, Status: model.OnSale, URL: "https://t.test/3", Sources: []string{"tickster"}},
	}
	set := index.NewTownSet()
	for _, show := range shows {
		set.Add(show)
	}
	built := time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC)
	window := model.Window{From: built, To: built.AddDate(0, 0, 120), Days: 120}
	manifest, _, err := index.Write(dir, shows, index.WriteOptions{
		BuiltAt: built,
		Window:  window,
		Sources: []model.Source{{ID: "tickster", Index: "sitemap", OK: true, Rows: len(shows)}},
		Towns:   set.Towns(built).Towns,
	})
	if err != nil {
		t.Fatalf("write index: %v", err)
	}
	if len(manifest.Towns) != 2 {
		t.Fatalf("expected two towns, got %d", len(manifest.Towns))
	}
	return dir
}

func newServer(t *testing.T, dir string) http.Handler {
	t.Helper()
	handler, err := Handler(Options{
		IndexDir: dir,
		Now:      func() time.Time { return time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	return handler
}

func get(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHealthAndReady(t *testing.T) {
	dir := buildIndex(t)
	handler := newServer(t, dir)

	if rec := get(t, handler, "/healthz"); rec.Code != 200 {
		t.Fatalf("healthz = %d", rec.Code)
	}
	rec := get(t, handler, "/readyz")
	if rec.Code != 200 {
		t.Fatalf("readyz = %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("readyz body: %v", err)
	}
	if body["ok"] != true || body["towns"].(float64) != 2 || body["rows"].(float64) != 3 {
		t.Fatalf("readyz = %v", body)
	}
}

func TestReadyRefusesABrokenIndex(t *testing.T) {
	dir := buildIndex(t)
	manifest, err := index.ReadManifest(dir)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	// Truncate one shard: the server must say so rather than serve half a town.
	path := filepath.Join(dir, filepath.FromSlash(manifest.Towns[0].Shard))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read shard: %v", err)
	}
	if err := os.WriteFile(path, raw[:len(raw)-8], 0o644); err != nil {
		t.Fatalf("write shard: %v", err)
	}
	rec := get(t, newServer(t, dir), "/readyz")
	if rec.Code != 503 {
		t.Fatalf("readyz on a broken index = %d, want 503", rec.Code)
	}
	if !json.Valid(rec.Body.Bytes()) {
		t.Fatalf("readyz body is not JSON: %s", rec.Body.String())
	}
}

func TestIndexFilesCarryTheEdgeHeaders(t *testing.T) {
	dir := buildIndex(t)
	handler := newServer(t, dir)
	manifest, err := index.ReadManifest(dir)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}

	rec := get(t, handler, "/manifest.json")
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "public, max-age=300" {
		t.Fatalf("manifest: %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("manifest content-type = %q", got)
	}

	rec = get(t, handler, "/towns/"+filepath.Base(manifest.Towns[0].Shard))
	if rec.Code != 200 {
		t.Fatalf("shard = %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("shard cache-control = %q", got)
	}
	local, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(manifest.Towns[0].Shard)))
	if err != nil {
		t.Fatalf("read local: %v", err)
	}
	if rec.Body.Len() != len(local) {
		t.Fatalf("served %d bytes, file is %d", rec.Body.Len(), len(local))
	}
}

func TestAnUnknownOrEscapingShardIsRefused(t *testing.T) {
	handler := newServer(t, buildIndex(t))
	for _, path := range []string{"/towns/nope.jsonl.gz", "/towns/..%2fmanifest.json", "/towns/"} {
		rec := get(t, handler, path)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s = %d, want 404", path, rec.Code)
		}
		var body struct {
			Error struct{ Code, Message string }
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error.Code == "" {
			t.Fatalf("%s: body is not the error envelope: %s", path, rec.Body.String())
		}
	}
}

func TestAParentPathNeverReachesAFileRead(t *testing.T) {
	// The mux cleans `..` into a redirect before any handler runs, and the shard handler only serves names that
	// are in the manifest — so neither route can read outside the index. Prove the second half directly.
	handler := newServer(t, buildIndex(t))
	rec := get(t, handler, "/towns/../manifest.json")
	if rec.Code == http.StatusOK {
		t.Fatalf("a parent path returned 200 with %q", rec.Body.String())
	}
	if rec.Code == http.StatusTemporaryRedirect && rec.Header().Get("Location") != "/manifest.json" {
		t.Fatalf("redirect went somewhere unexpected: %q", rec.Header().Get("Location"))
	}
}

// The wildcard is the contract's "not mine": a path this build does not serve answers 501 in the envelope, never a
// 404 that reads like a broken index and never a route that half-exists.
func TestAnUnknownAPIPathIsAnHonest501(t *testing.T) {
	handler := newServer(t, buildIndex(t))
	for _, path := range []string{
		"/api/v1/nope",
		"/api/v1/taste/handle?p=lastfm&u=x",
	} {
		rec := get(t, handler, path)
		if rec.Code != http.StatusNotImplemented {
			t.Fatalf("%s = %d, want 501", path, rec.Code)
		}
		var body struct {
			Error struct{ Code, Message string }
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error.Code != "not_implemented" {
			t.Fatalf("%s: envelope = %s", path, rec.Body.String())
		}
	}
}

func TestNoBoardIsA404ThatExplainsItself(t *testing.T) {
	handler := newServer(t, buildIndex(t))
	rec := get(t, handler, "/")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("root without --web = %d", rec.Code)
	}
	if rec.Body.String() == "" {
		t.Fatal("no message")
	}
}

func TestTheBoardServesWithAHistoryFallback(t *testing.T) {
	dir := buildIndex(t)
	web := t.TempDir()
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("<html>board</html>"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	handler, err := Handler(Options{IndexDir: dir, WebDir: web, Now: clock.Now})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec := get(t, handler, "/"); rec.Code != 200 || rec.Body.String() != "<html>board</html>" {
		t.Fatalf("root = %d %q", rec.Code, rec.Body.String())
	}
	// A query-parameter route and a deep link both render the document.
	if rec := get(t, handler, "/some/deep/route"); rec.Code != 200 {
		t.Fatalf("deep link = %d", rec.Code)
	}
	// Escaping the web root is not a page.
	if rec := get(t, handler, "/../../etc/passwd"); rec.Code == 200 {
		t.Fatal("a traversal returned 200")
	}
}

func TestAServerWithoutAnIndexRefusesToStart(t *testing.T) {
	if _, err := Handler(Options{IndexDir: filepath.Join(t.TempDir(), "nothing")}); err == nil {
		t.Fatal("started against a directory that is not an index")
	}
	if _, err := Handler(Options{}); err == nil {
		t.Fatal("started with no index directory")
	}
}
