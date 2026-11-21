// Package publish copies a built index into the public bucket.
//
// It is deliberately dumb: no partial uploads, no rewriting of a content-addressed object, no deletion. A shard
// whose name is its own digest cannot change, so a shard that already exists is skipped — that is the whole
// reason the format is content addressed, and it makes a re-publish cost nothing. The two mutable files
// (`manifest.json`, `towns.json`) are always uploaded, because their content is what a rebuild is for.
//
// It speaks S3 to R2 with SigV4 signed in this package: the miner stays one binary with no SDK, and the only
// secret it needs is an access key in the environment.
package publish

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	lineupindex "github.com/jakobbjelver/messer-lineup/internal/index"
	"github.com/jakobbjelver/messer-lineup/internal/model"

	"github.com/jakobbjelver/messer-lineup/internal/clock"
)

// Cache lifetimes. A shard is immutable for a year; the two files a client polls are minutes old at worst.
const (
	ShardMaxAge = 365 * 24 * time.Hour
	MetaMaxAge  = 5 * time.Minute
)

// Options is one publish.
type Options struct {
	IndexDir  string
	Endpoint  string // https://<account>.r2.cloudflarestorage.com
	Bucket    string
	AccessKey string
	SecretKey string
	DryRun    bool
	// NoGuard skips the §5.8 collapse guards. It exists for the operator who has looked at the numbers and
	// decided the drop is real; an automation must never set it.
	NoGuard bool
	Log     io.Writer
	Now     time.Time
	// Client lets a test point the publisher at its own server.
	Client *http.Client
}

// Object is one file the publish considers.
type Object struct {
	Key          string
	Path         string
	Size         int64
	CacheControl string
	ContentType  string
}

// Result is what a publish did.
type Result struct {
	Considered int      `json:"considered"`
	Uploaded   int      `json:"uploaded"`
	Skipped    int      `json:"skipped"`
	Bytes      int64    `json:"bytes"`
	Keys       []string `json:"keys"`
	// PreviousRows is what the guards compared against: 0 means nothing was published before this run.
	PreviousRows int  `json:"previous_rows"`
	DryRun       bool `json:"dry_run"`
}

// Run copies the index into the bucket. It refuses to run at all if the index does not verify — publishing an
// index the verifier would reject is how a bad build reaches the world.
func Run(ctx context.Context, opts Options) (Result, error) {
	result := Result{DryRun: opts.DryRun}
	if opts.IndexDir == "" {
		return result, fmt.Errorf("publish: no index directory")
	}
	if !opts.DryRun && (opts.Endpoint == "" || opts.Bucket == "" || opts.AccessKey == "" || opts.SecretKey == "") {
		return result, fmt.Errorf("publish: endpoint, bucket and credentials are all required (LINEUP_R2_*)")
	}
	if opts.Now.IsZero() {
		opts.Now = clock.Now()
	}
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: 60 * time.Second}
	}
	logf := func(format string, args ...any) {
		if opts.Log != nil {
			fmt.Fprintf(opts.Log, format+"\n", args...)
		}
	}

	// The guards run before anything is uploaded, and they run in a dry run too: the point is to find out that
	// this publish would be refused while looking at it, not after the fact.
	if !opts.NoGuard {
		current, err := readManifest(opts.IndexDir)
		if err != nil {
			return result, err
		}
		// A dry run on a machine with no credentials cannot read what is published, so the guards have nothing
		// to compare against. Say so rather than pretending the check ran.
		var previous *model.Manifest
		if opts.Endpoint != "" && opts.Bucket != "" && opts.AccessKey != "" && opts.SecretKey != "" {
			previous, err = opts.LoadPrevious(ctx)
			if err != nil {
				return result, err
			}
		} else {
			logf("publish: guards skipped — no credentials to read the published manifest")
		}
		if previous != nil {
			result.PreviousRows = rows(previous)
			logf("guards: %d rows published, %d in the index being replaced", rows(current), result.PreviousRows)
		}
		if problems := Check(previous, current); len(problems) > 0 {
			logf("publish: refused, nothing was uploaded:")
			for _, problem := range problems {
				logf("  %s", problem)
			}
			return result, Refused{Problems: problems}
		}
	}

	objects, err := collect(opts.IndexDir)
	if err != nil {
		return result, err
	}
	result.Considered = len(objects)

	for _, object := range objects {
		immutable := strings.HasPrefix(object.Key, "towns/")
		if immutable && !opts.DryRun {
			exists, err := opts.head(ctx, object.Key)
			if err != nil {
				return result, err
			}
			if exists {
				result.Skipped++
				logf("  = %s (already published)", object.Key)
				continue
			}
		}
		if opts.DryRun {
			result.Keys = append(result.Keys, object.Key)
			logf("  + %s (%d bytes, %s)", object.Key, object.Size, object.CacheControl)
			continue
		}
		if err := opts.put(ctx, object); err != nil {
			return result, err
		}
		result.Uploaded++
		result.Bytes += object.Size
		result.Keys = append(result.Keys, object.Key)
		logf("  + %s (%d bytes)", object.Key, object.Size)
	}
	return result, nil
}

// collect lists what a publish would send: the two mutable files and every shard, sorted by key so a dry run
// reads like the directory.
func collect(dir string) ([]Object, error) {
	var objects []Object
	for _, meta := range []struct {
		name   string
		maxAge time.Duration
	}{
		{"manifest.json", MetaMaxAge},
		{"towns.json", MetaMaxAge},
	} {
		path := filepath.Join(dir, meta.name)
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("publish: %s: %w", path, err)
		}
		objects = append(objects, Object{
			Key:          meta.name,
			Path:         path,
			Size:         info.Size(),
			CacheControl: cacheControl(meta.maxAge),
			ContentType:  "application/json; charset=utf-8",
		})
	}

	shards, err := os.ReadDir(filepath.Join(dir, "towns"))
	if err != nil {
		return nil, fmt.Errorf("publish: no shards: %w", err)
	}
	// Derived artifacts are optional on purpose: a build that has not resolved any is a supported state, and the board
	// falls back to initials. When one exists, its name comes from the manifest — content-addressed, cached like a
	// shard — and it is uploaded exactly as the board will ask for it.
	if manifest, err := lineupindex.ReadManifest(dir); err == nil {
		kinds := make([]string, 0, len(manifest.Files))
		for kind := range manifest.Files {
			kinds = append(kinds, kind)
		}
		sort.Strings(kinds)
		for _, kind := range kinds {
			key := manifest.Files[kind]
			if !strings.HasPrefix(key, "artists/") {
				return nil, fmt.Errorf("publish: %s names %q, which is not under artists/", kind, key)
			}
			path := filepath.Join(dir, filepath.FromSlash(key))
			info, err := os.Stat(path)
			if err != nil {
				return nil, fmt.Errorf("publish: %s lists %s: %w", kind, key, err)
			}
			objects = append(objects, Object{
				Key:          key,
				Path:         path,
				Size:         info.Size(),
				CacheControl: cacheControl(ShardMaxAge),
				ContentType:  "application/json; charset=utf-8",
			})
		}
	}
	for _, entry := range shards {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl.gz") {
			continue
		}
		path := filepath.Join(dir, "towns", entry.Name())
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		objects = append(objects, Object{
			Key:          "towns/" + entry.Name(),
			Path:         path,
			Size:         info.Size(),
			CacheControl: cacheControl(ShardMaxAge),
			ContentType:  "application/x-ndjson",
		})
	}
	sort.SliceStable(objects, func(i, j int) bool { return objects[i].Key < objects[j].Key })
	return objects, nil
}

func cacheControl(maxAge time.Duration) string {
	if maxAge >= time.Hour {
		return fmt.Sprintf("public, max-age=%d, immutable", int(maxAge.Seconds()))
	}
	return fmt.Sprintf("public, max-age=%d", int(maxAge.Seconds()))
}

// readManifest is the index's own manifest, which the guards compare against the published one.
func readManifest(dir string) (*model.Manifest, error) {
	body, err := os.ReadFile(filepath.Join(dir, lineupindex.ManifestFile))
	if err != nil {
		return nil, fmt.Errorf("publish: %w", err)
	}
	var manifest model.Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("publish: %s: %w", lineupindex.ManifestFile, err)
	}
	return &manifest, nil
}

func (o Options) objectURL(key string) string {
	return strings.TrimRight(o.Endpoint, "/") + "/" + o.Bucket + "/" + key
}

// head asks whether an object is already in the bucket. Only content-addressed keys are ever skipped, so a
// 200 here means the identical bytes are already published.
func (o Options) head(ctx context.Context, key string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, o.objectURL(key), nil)
	if err != nil {
		return false, err
	}
	o.sign(req, nil)
	resp, err := o.Client.Do(req)
	if err != nil {
		return false, fmt.Errorf("publish: HEAD %s: %w", key, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
		return true, nil
	case resp.StatusCode == http.StatusNotFound:
		return false, nil
	}
	return false, fmt.Errorf("publish: HEAD %s: %s", key, resp.Status)
}

func (o Options) put(ctx context.Context, object Object) error {
	body, err := os.ReadFile(object.Path)
	if err != nil {
		return fmt.Errorf("publish: %s: %w", object.Path, err)
	}
	// A shard leaves this machine exactly as the verifier saw it: gzipped, unmodified. Decoding it here would
	// be re-building the artifact rather than publishing it.
	if err := checkShard(object, body); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, o.objectURL(object.Key), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Type", object.ContentType)
	req.Header.Set("Cache-Control", object.CacheControl)
	o.sign(req, body)
	resp, err := o.Client.Do(req)
	if err != nil {
		return fmt.Errorf("publish: PUT %s: %w", object.Key, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("publish: PUT %s: %s: %s", object.Key, resp.Status, strings.TrimSpace(string(detail)))
	}
	return nil
}

// checkShard refuses a shard that is not gzip of JSONL: the bucket is public, and a truncated file would be a
// 200 with nothing behind it.
func checkShard(object Object, body []byte) error {
	if !strings.HasPrefix(object.Key, "towns/") {
		var probe any
		return json.Unmarshal(body, &probe)
	}
	zr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("publish: %s is not gzip: %w", object.Key, err)
	}
	defer zr.Close()
	if _, err := io.Copy(io.Discard, zr); err != nil {
		return fmt.Errorf("publish: %s is a truncated gzip stream: %w", object.Key, err)
	}
	return nil
}

// sign adds SigV4 headers. The signed set is the minimum S3 accepts: host, the payload hash and the date.
func (o Options) sign(req *http.Request, body []byte) {
	payloadHash := sha256.Sum256(body)
	payload := hex.EncodeToString(payloadHash[:])
	now := o.Now.UTC()
	amzDate := now.Format("20060102T150405Z")
	day := now.Format("20060102")

	req.Header.Set("x-amz-content-sha256", payload)
	req.Header.Set("x-amz-date", amzDate)

	host := req.URL.Host
	canonicalHeaders := "host:" + host + "\n" + "x-amz-content-sha256:" + payload + "\n" + "x-amz-date:" + amzDate + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalRequest := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		req.URL.RawQuery,
		canonicalHeaders,
		signedHeaders,
		payload,
	}, "\n")

	scope := day + "/auto/s3/aws4_request"
	requestHash := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := strings.Join([]string{"AWS4-HMAC-SHA256", amzDate, scope, hex.EncodeToString(requestHash[:])}, "\n")

	key := hmacSHA256([]byte("AWS4"+o.SecretKey), day)
	key = hmacSHA256(key, "auto")
	key = hmacSHA256(key, "s3")
	key = hmacSHA256(key, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(key, stringToSign))

	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		o.AccessKey, scope, signedHeaders, signature))
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}
