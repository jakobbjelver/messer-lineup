package index

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/model"
)

// The index directory is the whole published artifact:
//
//	manifest.json              what a client polls
//	towns.json                 the picker's data set
//	towns/<sha256>.jsonl.gz    one flat shard per town, content addressed
//
// A shard's name is the digest of the bytes a client fetches, so an immutable cache header is honest and a
// rebuilt shard is a different object rather than a silent overwrite of the same URL.

// DefaultWindowDays is the rolling publication window (ADR-001): a season board, not an archive.
const DefaultWindowDays = 120

// ShardDir is the directory inside the index root that holds the shards.
const ShardDir = "towns"

// WriteOptions is one build.
type WriteOptions struct {
	BuiltAt time.Time   // the clock, injected; it is the only non-deterministic input
	Window  model.Window // the published date range
	Sources []model.Source
	Towns   []model.Town // from TownSet; the entries whose towns ended up with rows are published
	Prune   bool         // delete shard files the new manifest does not reference
}

// Stats is what a build did, so a caller can report it without re-reading the directory.
type Stats struct {
	Shows     int // shows offered
	Published int // rows written
	Outside   int // dropped for falling outside the window
	Refused   int // dropped because they could not be projected
	Shards    int // shards written
	Bytes     int // compressed bytes written
	Towns     int // towns with at least one row
}

// Write renders an index directory from shows. It is deterministic: the same shows, window and BuiltAt
// produce byte-identical files, which is what lets a test assert the shard's digest.
func Write(dir string, shows []model.Show, opts WriteOptions) (model.Manifest, Stats, error) {
	var stats Stats
	if opts.BuiltAt.IsZero() {
		return model.Manifest{}, stats, fmt.Errorf("index: a build needs its clock (BuiltAt), because it is written into the manifest")
	}
	if opts.Window.From.IsZero() {
		return model.Manifest{}, stats, fmt.Errorf("index: a build needs a window")
	}
	if err := os.MkdirAll(filepath.Join(dir, ShardDir), 0o755); err != nil {
		return model.Manifest{}, stats, err
	}

	stats.Shows = len(shows)
	byTown := map[string][]model.Row{}
	for _, show := range shows {
		if !inWindow(show.Start, opts.Window) {
			stats.Outside++
			continue
		}
		row, err := model.Project(show)
		if err != nil {
			stats.Refused++
			continue
		}
		if row.C == "" {
			stats.Refused++
			continue
		}
		byTown[row.C] = append(byTown[row.C], row)
		stats.Published++
	}

	manifest := model.Manifest{
		Format:  model.FormatIndex,
		BuiltAt: opts.BuiltAt.UTC(),
		Window:  opts.Window,
		Sources: append([]model.Source(nil), opts.Sources...),
		Towns:   []model.Index{},
	}
	townIDs := make([]string, 0, len(byTown))
	for id := range byTown {
		townIDs = append(townIDs, id)
	}
	sort.Strings(townIDs)

	referenced := map[string]bool{}
	for _, townID := range townIDs {
		rows := byTown[townID]
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].K != rows[j].K {
				return rows[i].K < rows[j].K
			}
			return rows[i].D < rows[j].D
		})
		blob, err := encodeShard(rows)
		if err != nil {
			return model.Manifest{}, stats, fmt.Errorf("index: encoding %s: %w", townID, err)
		}
		digest := sha256.Sum256(blob)
		name := hex.EncodeToString(digest[:])
		path := filepath.Join(dir, ShardDir, name+".jsonl.gz")
		if err := os.WriteFile(path, blob, 0o644); err != nil {
			return model.Manifest{}, stats, err
		}
		referenced[name+".jsonl.gz"] = true
		stats.Shards++
		stats.Bytes += len(blob)

		manifest.Towns = append(manifest.Towns, model.Index{
			Town:    townID,
			Shard:   ShardDir + "/" + name + ".jsonl.gz",
			SHA256:  name,
			Count:   len(rows),
			Sources: sourcesOf(rows),
			BuiltAt: opts.BuiltAt.UTC(),
		})
	}
	stats.Towns = len(manifest.Towns)

	if err := writeJSONFile(filepath.Join(dir, ManifestFile), manifest); err != nil {
		return model.Manifest{}, stats, err
	}

	towns := model.Towns{
		Format:  model.FormatTowns,
		BuiltAt: opts.BuiltAt.UTC(),
		Towns:   publishedTowns(opts.Towns, byTown),
	}
	if err := writeJSONFile(filepath.Join(dir, TownsFile), towns); err != nil {
		return model.Manifest{}, stats, err
	}
	if opts.Prune {
		if err := pruneShards(filepath.Join(dir, ShardDir), referenced); err != nil {
			return manifest, stats, err
		}
	}
	return manifest, stats, nil
}

// encodeShard renders rows as gzipped JSONL. The gzip header is pinned (no name, no mtime) so the digest
// depends on the rows alone — a shard rebuilt from the same shows is the same object.
func encodeShard(rows []model.Row) ([]byte, error) {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	zw.Header = gzip.Header{OS: 255}
	enc := json.NewEncoder(zw)
	enc.SetEscapeHTML(false)
	for _, row := range rows {
		if err := enc.Encode(row); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ReadManifest loads the manifest of a built index directory. The reader half lives beside the writer so a
// caller (serve, a fixture loader, a test) does not re-implement the path.
func ReadManifest(dir string) (model.Manifest, error) {
	var manifest model.Manifest
	raw, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		return manifest, err
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return manifest, fmt.Errorf("index: %s: %w", ManifestFile, err)
	}
	if manifest.Format != model.FormatIndex {
		return manifest, fmt.Errorf("index: %s declares format %q, want %q", ManifestFile, manifest.Format, model.FormatIndex)
	}
	return manifest, nil
}

// ReadShard is the reader half, used by serve and by the fixture path. It refuses a shard whose digest is
// not the one the manifest promised.
func ReadShard(path string) ([]model.Row, error) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodeShard(blob)
}

// DecodeShard decodes gzipped JSONL rows and rejects any line that is not a row of this format.
func DecodeShard(blob []byte) ([]model.Row, error) {
	zr, err := gzip.NewReader(bytes.NewReader(blob))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	dec := json.NewDecoder(io.LimitReader(zr, 1<<28))
	var rows []model.Row
	for dec.More() {
		var row model.Row
		if err := dec.Decode(&row); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func inWindow(start time.Time, w model.Window) bool {
	if start.IsZero() {
		return false
	}
	day := start.Format("2006-01-02")
	return day >= w.From.Format("2006-01-02") && day <= w.To.Format("2006-01-02")
}

// sourcesOf lists the sources a town's rows came from, sorted, so the coverage line needs no shard fetch.
func sourcesOf(rows []model.Row) []string {
	seen := map[string]bool{}
	for _, row := range rows {
		for _, src := range row.Src {
			seen[src] = true
		}
	}
	out := make([]string, 0, len(seen))
	for src := range seen {
		out = append(out, src)
	}
	sort.Strings(out)
	return out
}

// publishedTowns keeps the picker entries for towns that actually have rows, in id order.
func publishedTowns(towns []model.Town, byTown map[string][]model.Row) []model.Town {
	out := make([]model.Town, 0, len(towns))
	for _, town := range towns {
		if len(byTown[town.ID]) == 0 {
			continue
		}
		out = append(out, town)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func writeJSONFile(path string, value any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(value); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// pruneShards deletes shard files the manifest no longer references. Content addressing means a rebuild
// writes a new name and strands the old one; without this the directory grows without bound (the same
// reason the bucket needs a lifecycle rule).
func pruneShards(dir string, keep map[string]bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || keep[entry.Name()] {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
