// Package artifact publishes a derived JSON file beside the town shards under a name that carries its own digest.
//
// The two files that use it — `portraits` and, later, `genres` — are rewritten every night while their readers cache
// aggressively. A stable name plus `immutable` is a file that never updates: the board fetched `artists/portraits.json`
// with `force-cache` and the bucket served it for a year, so every returning visitor kept the first copy while the
// miner kept adding faces. Content addressing makes the name the version: a new file is a new URL, an old one is
// pruned, and the manifest's `files` map is what tells a client which name this build published.
package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jakobbjelver/messer-lineup/internal/index"
)

// Dir is where artifacts live inside an index.
const Dir = "artists"

// Name returns the content-addressed path for a kind of artifact (`portraits`, `genres`).
func Name(kind string, payload []byte) string {
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("%s/%s-%s.json", Dir, kind, hex.EncodeToString(sum[:]))
}

// Write stores payload as `artists/<kind>-<sha256>.json`, removes the other files of that kind, and records the name in
// the manifest's `files` map. The manifest is read and rewritten as a document rather than as a struct, so a field a
// newer build added survives this one.
//
// It returns the path inside the index. The write is the caller's statement that the bytes are final: no attempt is
// made to merge with a previous artifact of the same kind.
func Write(indexDir, kind string, payload []byte) (string, error) {
	if indexDir == "" {
		return "", fmt.Errorf("artifact: no index directory")
	}
	if kind == "" || strings.ContainsAny(kind, `/\`) || strings.Contains(kind, "..") {
		return "", fmt.Errorf("artifact: %q is not a kind", kind)
	}
	dir := filepath.Join(indexDir, Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("artifact: %w", err)
	}

	name := Name(kind, payload)
	path := filepath.Join(indexDir, filepath.FromSlash(name))
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return "", fmt.Errorf("artifact: %w", err)
	}
	if err := prune(indexDir, kind, name); err != nil {
		return "", err
	}
	if err := record(indexDir, kind, name); err != nil {
		return "", err
	}
	return name, nil
}

// prune deletes the previous versions of one kind: the content-addressed siblings and the old stable name
// (`artists/portraits.json`) that predates content addressing. It is scoped to those two shapes, so a hand-placed
// file (or a different artifact) is never collateral.
func prune(indexDir, kind, keep string) error {
	dir := filepath.Join(indexDir, Dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("artifact: %w", err)
	}
	prefix, suffix := kind+"-", ".json"
	legacy := kind + ".json"
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		contentAddressed := strings.HasPrefix(name, prefix) && strings.HasSuffix(name, suffix)
		if !contentAddressed && name != legacy {
			continue
		}
		if Dir+"/"+name == keep {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("artifact: prune %s: %w", name, err)
		}
	}
	return nil
}

// record rewrites the manifest's `files` map. A manifest that does not exist yet is an error: an artifact is part of a
// build, and a build without a manifest has nothing to attach it to.
func record(indexDir, kind, name string) error {
	path := filepath.Join(indexDir, index.ManifestFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("artifact: %s: %w", index.ManifestFile, err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("artifact: %s: %w", index.ManifestFile, err)
	}
	files := map[string]string{}
	if existing, ok := doc["files"]; ok {
		if err := json.Unmarshal(existing, &files); err != nil {
			return fmt.Errorf("artifact: %s: files: %w", index.ManifestFile, err)
		}
	}
	files[kind] = name
	encoded, err := json.Marshal(files)
	if err != nil {
		return fmt.Errorf("artifact: %w", err)
	}
	doc["files"] = encoded
	out, err := json.MarshalIndent(doc, "", " ")
	if err != nil {
		return fmt.Errorf("artifact: %w", err)
	}
	out = append(out, '\n')
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("artifact: %w", err)
	}
	return nil
}
