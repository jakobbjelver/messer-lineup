// Package index reads a published index directory: manifest.json, towns.json and the content-addressed
// shards they name. It is the read side of the format — the writer lands in P1 and the projector that
// produces rows lives in internal/model.
package index

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/model"
)

// ManifestFile and TownsFile are the two files at the root of an index directory. Every other file is
// content addressed and named by the manifest, so nothing else is discovered by listing the tree.
const (
	ManifestFile = "manifest.json"
	TownsFile    = "towns.json"
)

// Problem is one reason an index may not be published. Where is a file, or file:line for a row.
type Problem struct {
	Where string `json:"where"`
	What  string `json:"what"`
}

func (p Problem) String() string { return p.Where + ": " + p.What }

// Report is the verdict. Verified is true only when Problems is empty and there was something to check:
// an index with no towns or no rows is refused rather than passed, because an empty publish is the
// failure mode that looks like a quiet success.
type Report struct {
	Dir       string        `json:"dir"`
	Verified  bool          `json:"verified"`
	Format    string        `json:"format,omitempty"`
	BuiltAt   *time.Time    `json:"built_at,omitempty"`
	Window    *model.Window `json:"window,omitempty"`
	Towns     int           `json:"towns"`
	Rows      int           `json:"rows"`
	Problems  []Problem     `json:"problems"`
	CheckedAt time.Time     `json:"checked_at"`
}

// OK reports whether the index may be published.
func (r Report) OK() bool { return r.Verified && len(r.Problems) == 0 }

// Verify reads the index in dir and checks it against the published contract. A missing manifest is a
// refusal ("nothing has been built"), not an error; an error means the directory could not be read at
// all. now is the clock, so the verdict on a stale index is reproducible.
func Verify(dir string, now time.Time) (Report, error) {
	rep := Report{Dir: dir, CheckedAt: now.UTC(), Problems: []Problem{}}
	add := func(where, format string, args ...any) {
		rep.Problems = append(rep.Problems, Problem{Where: where, What: fmt.Sprintf(format, args...)})
	}

	manifestPath := filepath.Join(dir, ManifestFile)
	rawManifest, err := os.ReadFile(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		add(ManifestFile, "no manifest: this directory holds no published index yet")
		rep.finish()
		return rep, nil
	}
	if err != nil {
		return rep, fmt.Errorf("read %s: %w", manifestPath, err)
	}
	var manifest model.Manifest
	if err := json.Unmarshal(rawManifest, &manifest); err != nil {
		add(ManifestFile, "does not parse: %v", err)
		rep.finish()
		return rep, nil
	}
	rep.Format = manifest.Format
	builtAt := manifest.BuiltAt.UTC()
	rep.BuiltAt = &builtAt
	window := manifest.Window
	rep.Window = &window

	if manifest.Format != model.FormatIndex {
		add(ManifestFile, "format is %q, this build reads %q", manifest.Format, model.FormatIndex)
	}
	if manifest.BuiltAt.IsZero() {
		add(ManifestFile, "carries no built_at")
	}
	if manifest.Window.Days <= 0 || manifest.Window.To.Before(manifest.Window.From) {
		add(ManifestFile, "window is not a range of days (%s → %s, %d days)",
			manifest.Window.From.Format("2006-01-02"), manifest.Window.To.Format("2006-01-02"), manifest.Window.Days)
	}

	towns, townProblems := loadTowns(dir)
	for _, p := range townProblems {
		rep.Problems = append(rep.Problems, p)
	}

	if len(manifest.Towns) == 0 {
		add(ManifestFile, "the manifest names no towns: an empty index is a failed build, not a quiet one")
	}
	for _, src := range manifest.Sources {
		if src.ID == "" {
			add(ManifestFile, "a source entry has no id")
		}
		if src.ParseYield < 0 || src.ParseYield > 1 {
			add(ManifestFile, "source %q has parse_yield %v, want 0..1", src.ID, src.ParseYield)
		}
	}

	// Derived artifacts: every name must exist, sit under artists/, carry its own digest, and decode as an object.
	// The digest is the whole point — a stable filename with an immutable cache header is a file that never reaches a
	// returning visitor again.
	kinds := make([]string, 0, len(manifest.Files))
	for kind := range manifest.Files {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		name := manifest.Files[kind]
		if !strings.HasPrefix(name, "artists/") {
			add(ManifestFile, "files[%s] = %q, want a path under artists/", kind, name)
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			add(ManifestFile, "files[%s] names %s, which cannot be read: %v", kind, name, err)
			continue
		}
		digest := sha256.Sum256(raw)
		if !strings.Contains(name, hex.EncodeToString(digest[:])) {
			add(ManifestFile, "files[%s] = %s does not carry its own digest", kind, name)
		}
		var object map[string]any
		if err := json.Unmarshal(raw, &object); err != nil {
			add(ManifestFile, "files[%s] is not a JSON object: %v", kind, err)
		}
	}

	rep.Towns = len(manifest.Towns)
	from := manifest.Window.From.Format(dayLayout)
	to := manifest.Window.To.Format(dayLayout)
	for _, town := range manifest.Towns {
		rows, problems := verifyShard(dir, town, towns, from, to)
		rep.Rows += rows
		rep.Problems = append(rep.Problems, problems...)
	}
	if len(manifest.Towns) > 0 && rep.Rows == 0 {
		add(ManifestFile, "the index holds no rows: nothing would be published")
	}
	rep.finish()
	return rep, nil
}

func (r *Report) finish() {
	sort.SliceStable(r.Problems, func(i, j int) bool {
		if r.Problems[i].Where != r.Problems[j].Where {
			return r.Problems[i].Where < r.Problems[j].Where
		}
		return r.Problems[i].What < r.Problems[j].What
	})
	r.Verified = len(r.Problems) == 0
}

// loadTowns reads the picker file. A town the picker does not know is a town nobody can choose, so a
// missing towns.json fails the whole verify rather than degrading the board.
func loadTowns(dir string) (map[string]model.Town, []Problem) {
	var problems []Problem
	raw, err := os.ReadFile(filepath.Join(dir, TownsFile))
	if err != nil {
		return nil, []Problem{{Where: TownsFile, What: "cannot be read: " + err.Error()}}
	}
	var towns model.Towns
	if err := json.Unmarshal(raw, &towns); err != nil {
		return nil, []Problem{{Where: TownsFile, What: "does not parse: " + err.Error()}}
	}
	if towns.Format != model.FormatTowns {
		problems = append(problems, Problem{Where: TownsFile,
			What: fmt.Sprintf("format is %q, this build reads %q", towns.Format, model.FormatTowns)})
	}
	out := make(map[string]model.Town, len(towns.Towns))
	for _, t := range towns.Towns {
		if t.ID == "" {
			problems = append(problems, Problem{Where: TownsFile, What: "a town entry has no id"})
			continue
		}
		if _, dup := out[t.ID]; dup {
			problems = append(problems, Problem{Where: TownsFile, What: "town " + t.ID + " appears twice"})
		}
		if strings.ToLower(t.Country) != t.Country || len(t.Country) != 2 {
			problems = append(problems, Problem{Where: TownsFile,
				What: "town " + t.ID + " has country " + t.Country + ", want a lower-case ISO 3166-1 alpha-2"})
		}
		out[t.ID] = t
	}
	return out, problems
}

// verifyShard checks one town's shard and returns how many rows it carried.
func verifyShard(dir string, entry model.Index, towns map[string]model.Town, from, to string) (int, []Problem) {
	var problems []Problem
	add := func(where, format string, args ...any) {
		problems = append(problems, Problem{Where: where, What: fmt.Sprintf(format, args...)})
	}
	where := entry.Shard
	if entry.Town == "" {
		add(ManifestFile, "a town entry has no town id")
		return 0, problems
	}
	if _, ok := towns[entry.Town]; !ok {
		add(ManifestFile, "town %s is in the manifest but not in %s, so nobody can pick it", entry.Town, TownsFile)
	}
	if entry.Shard == "" {
		add(ManifestFile, "town %s names no shard", entry.Town)
		return 0, problems
	}
	if !strings.HasSuffix(entry.Shard, ".jsonl.gz") {
		add(where, "a shard must be gzipped JSONL (.jsonl.gz)")
	}
	path := filepath.Join(dir, filepath.FromSlash(entry.Shard))
	info, err := os.Stat(path)
	if err != nil {
		add(where, "cannot be read: %v", err)
		return 0, problems
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		add(where, "cannot be read: %v", err)
		return 0, problems
	}
	sum := sha256.Sum256(raw)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, entry.SHA256) {
		add(where, "sha256 is %s, the manifest says %s — the object is not what was published", got, entry.SHA256)
	}

	if info.Size() == 0 {
		add(where, "is an empty object")
		return 0, problems
	}
	rows, lineProblems := scanShard(raw, entry, from, to)
	problems = append(problems, lineProblems...)
	if entry.Count != rows {
		add(where, "carries %d rows, the manifest says %d", rows, entry.Count)
	}
	if rows == 0 {
		add(where, "town %s has no rows", entry.Town)
	}
	return rows, problems
}

// scanShard parses every line and holds each one to the nine-key contract.
func scanShard(raw []byte, entry model.Index, from, to string) (int, []Problem) {
	var problems []Problem
	add := func(line int, format string, args ...any) {
		problems = append(problems, Problem{Where: fmt.Sprintf("%s:%d", filepath.ToSlash(entry.Shard), line),
			What: fmt.Sprintf(format, args...)})
	}
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return 0, []Problem{{Where: entry.Shard, What: "is not gzip: " + err.Error()}}
	}
	defer gz.Close()

	scanner := bufio.NewScanner(gz)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	rows := 0
	for line := 1; scanner.Scan(); line++ {
		text := bytes.TrimSpace(scanner.Bytes())
		if len(text) == 0 {
			add(line, "empty line")
			continue
		}
		rows++
		if p := checkShape(text); p != "" {
			add(line, "%s", p)
			continue
		}
		var row model.Row
		dec := json.NewDecoder(bytes.NewReader(text))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&row); err != nil {
			add(line, "does not decode into a row: %v", err)
			continue
		}
		if p := checkRow(row); p != "" {
			add(line, "%s", p)
			continue
		}
		if row.C != entry.Town {
			add(line, "row belongs to town %q, the shard is %q", row.C, entry.Town)
		}
		if rowDay := dayOf(row.D); rowDay != "" && (rowDay < from || rowDay > to) {
			add(line, "row is dated %s, outside the published window %s → %s", rowDay, from, to)
		}
	}
	if err := scanner.Err(); err != nil {
		problems = append(problems, Problem{Where: entry.Shard, What: "cannot be decompressed: " + err.Error()})
	}
	return rows, problems
}
