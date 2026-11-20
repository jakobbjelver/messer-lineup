package publish

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/clock"
)

// bucket is a tiny S3: it remembers what was PUT and answers HEAD from the same map.
type bucket struct {
	mu      sync.Mutex
	objects map[string]record
	puts    []string
	// previous is the manifest the guards read before uploading; nil means nothing is published yet.
	previous []byte
}

type record struct {
	body         []byte
	cacheControl string
	contentType  string
}

func (b *bucket) handler(t *testing.T, existing ...string) http.Handler {
	for _, key := range existing {
		b.objects[key] = record{body: []byte("already here")}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if !strings.HasPrefix(path, "messer-lineup-index/") {
			t.Errorf("request went to the wrong bucket path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		key := strings.TrimPrefix(path, "messer-lineup-index/")

		// Every request must be signed and must declare the payload hash S3 verifies.
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=key/") {
			t.Errorf("%s %s: unsigned request", r.Method, key)
		}
		if r.Header.Get("x-amz-content-sha256") == "" || r.Header.Get("x-amz-date") == "" {
			t.Errorf("%s %s: missing sigv4 headers", r.Method, key)
		}

		b.mu.Lock()
		defer b.mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			// The guards read the published manifest before uploading. A bucket with none is the first publish.
			if b.previous == nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(b.previous)
		case http.MethodHead:
			if _, ok := b.objects[key]; ok {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPut:
			body := new(bytes.Buffer)
			if _, err := body.ReadFrom(r.Body); err != nil {
				t.Fatalf("read body: %v", err)
			}
			sum := sha256.Sum256(body.Bytes())
			if got := r.Header.Get("x-amz-content-sha256"); got != hex.EncodeToString(sum[:]) {
				t.Errorf("%s: payload hash %s does not match the body", key, got)
			}
			b.objects[key] = record{
				body:         body.Bytes(),
				cacheControl: r.Header.Get("Cache-Control"),
				contentType:  r.Header.Get("Content-Type"),
			}
			b.puts = append(b.puts, key)
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

// index writes a minimal but valid index directory: two meta files and two gzipped shards.
func index(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "towns"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	write := func(path string, body []byte) {
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	write(filepath.Join(dir, "manifest.json"), []byte(`{"format":"lineup-index/1"}`))
	write(filepath.Join(dir, "towns.json"), []byte(`{"format":"lineup-towns/1"}`))
	for _, name := range []string{"aaa", "bbb"} {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		if _, err := zw.Write([]byte(`{"k":"x|y|2026-12-01"}` + "\n")); err != nil {
			t.Fatalf("gz: %v", err)
		}
		if err := zw.Close(); err != nil {
			t.Fatalf("gz close: %v", err)
		}
		write(filepath.Join(dir, "towns", name+".jsonl.gz"), buf.Bytes())
	}
	return dir
}

func options(dir string, srv *httptest.Server) Options {
	return Options{
		IndexDir:  dir,
		Endpoint:  srv.URL,
		Bucket:    "messer-lineup-index",
		AccessKey: "key",
		SecretKey: "secret",
		Now:       time.Date(2026, 11, 18, 7, 0, 0, 0, time.UTC),
		Client:    srv.Client(),
	}
}

func TestPublishUploadsTheIndexWithTheRightCacheHeaders(t *testing.T) {
	dir := index(t)
	b := &bucket{objects: map[string]record{}}
	srv := httptest.NewServer(b.handler(t))
	defer srv.Close()

	result, err := Run(context.Background(), options(dir, srv))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Considered != 4 || result.Uploaded != 4 || result.Skipped != 0 {
		t.Fatalf("result = %+v, want 4 objects uploaded", result)
	}
	if result.Bytes == 0 {
		t.Fatal("no bytes counted")
	}

	shard := b.objects["towns/aaa.jsonl.gz"]
	if shard.cacheControl != "public, max-age=31536000, immutable" {
		t.Fatalf("shard cache-control = %q, want a year and immutable", shard.cacheControl)
	}
	if shard.contentType != "application/x-ndjson" {
		t.Fatalf("shard content-type = %q", shard.contentType)
	}
	manifest := b.objects["manifest.json"]
	if manifest.cacheControl != "public, max-age=300" {
		t.Fatalf("manifest cache-control = %q, want five minutes — a poll must see a rebuild", manifest.cacheControl)
	}
	if manifest.contentType != "application/json; charset=utf-8" {
		t.Fatalf("manifest content-type = %q", manifest.contentType)
	}
	// The bytes are published, not rebuilt.
	local, err := os.ReadFile(filepath.Join(dir, "towns", "aaa.jsonl.gz"))
	if err != nil {
		t.Fatalf("read local shard: %v", err)
	}
	if !bytes.Equal(local, shard.body) {
		t.Fatal("the uploaded shard differs from the built one")
	}
}

func TestAnExistingShardIsSkippedButTheManifestIsAlwaysSent(t *testing.T) {
	dir := index(t)
	b := &bucket{objects: map[string]record{}}
	srv := httptest.NewServer(b.handler(t, "towns/aaa.jsonl.gz", "manifest.json"))
	defer srv.Close()

	result, err := Run(context.Background(), options(dir, srv))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Skipped != 1 {
		t.Fatalf("skipped = %d, want the shard that already existed", result.Skipped)
	}
	// manifest.json and towns.json are mutable: a rebuild must always be able to replace them.
	sent := strings.Join(b.puts, " ")
	if strings.Contains(sent, "towns/aaa.jsonl.gz") {
		t.Fatalf("an existing shard was rewritten: %s", sent)
	}
	if !strings.Contains(sent, "manifest.json") {
		t.Fatalf("the manifest was skipped: %s", sent)
	}
	if !strings.Contains(sent, "towns/bbb.jsonl.gz") {
		t.Fatalf("a new shard was skipped: %s", sent)
	}
}

func TestDryRunTouchesNothing(t *testing.T) {
	dir := index(t)
	b := &bucket{objects: map[string]record{}}
	srv := httptest.NewServer(b.handler(t))
	defer srv.Close()

	opts := options(dir, srv)
	opts.DryRun = true
	var log bytes.Buffer
	opts.Log = &log
	result, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Uploaded != 0 || len(b.puts) != 0 {
		t.Fatalf("a dry run uploaded %v", b.puts)
	}
	if len(result.Keys) != 4 {
		t.Fatalf("dry run listed %d keys, want 4", len(result.Keys))
	}
	if !strings.Contains(log.String(), "towns/aaa.jsonl.gz") || !strings.Contains(log.String(), "immutable") {
		t.Fatalf("dry run log does not say what it would do:\n%s", log.String())
	}
}

func TestATruncatedShardIsRefused(t *testing.T) {
	dir := index(t)
	full, err := os.ReadFile(filepath.Join(dir, "towns", "aaa.jsonl.gz"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "towns", "aaa.jsonl.gz"), full[:len(full)-6], 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	b := &bucket{objects: map[string]record{}}
	srv := httptest.NewServer(b.handler(t))
	defer srv.Close()

	// The objects are walked in key order, so the manifest goes first; the refusal is what matters.
	_, err = Run(context.Background(), options(dir, srv))
	if err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("err = %v, want a refusal to publish a truncated shard", err)
	}
}

func TestCredentialsAreRequiredUnlessDryRunning(t *testing.T) {
	dir := index(t)
	_, err := Run(context.Background(), Options{IndexDir: dir, Endpoint: "https://example.test", Bucket: "b"})
	if err == nil {
		t.Fatal("a publish without credentials was attempted")
	}
	if _, err := Run(context.Background(), Options{IndexDir: dir, DryRun: true, Now: clock.Now()}); err != nil {
		t.Fatalf("a dry run needs no credentials: %v", err)
	}
}

func TestSignatureIsDeterministicAndWellFormed(t *testing.T) {
	opts := Options{AccessKey: "AKIDEXAMPLE", SecretKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", Now: time.Date(2026, 11, 18, 7, 0, 0, 0, time.UTC)}
	req, err := http.NewRequest(http.MethodPut, "https://0238.example.r2.cloudflarestorage.com/messer-lineup-index/towns/a.jsonl.gz", strings.NewReader("body"))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	opts.sign(req, []byte("body"))
	auth := req.Header.Get("Authorization")
	for _, want := range []string{
		"AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20261118/auto/s3/aws4_request",
		"SignedHeaders=host;x-amz-content-sha256;x-amz-date",
		"Signature=",
	} {
		if !strings.Contains(auth, want) {
			t.Fatalf("authorization %q is missing %q", auth, want)
		}
	}
	// The payload hash is of the body, not of nothing: this is the mistake that yields 403 XAmzContentSHA256Mismatch.
	sum := sha256.Sum256([]byte("body"))
	if got := req.Header.Get("x-amz-content-sha256"); got != hex.EncodeToString(sum[:]) {
		t.Fatalf("content sha = %s", got)
	}
	// Signing twice in the same second produces the same signature.
	second, _ := http.NewRequest(http.MethodPut, req.URL.String(), strings.NewReader("body"))
	opts.sign(second, []byte("body"))
	if second.Header.Get("Authorization") != auth {
		t.Fatal("the signature is not deterministic")
	}
}
