// Package doctor answers one question without changing anything: is this install able to publish, and
// what state is it in? It reads the index directory, the source registry and the build stamp, and says
// so in a table a human reads or JSON an agent reads.
package doctor

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/index"
	"github.com/jakobbjelver/messer-lineup/internal/version"
)

// Report is the whole answer. Problems is empty when nothing is wrong; a missing index is a state, not
// a problem, because a fresh clone has not mined anything yet.
type Report struct {
	Version  string      `json:"version"`
	Go       string      `json:"go"`
	OS       string      `json:"os"`
	Arch     string      `json:"arch"`
	Now      time.Time   `json:"now"`
	Index    IndexState  `json:"index"`
	Sources  SourceState `json:"sources"`
	Problems []string    `json:"problems"`
}

// IndexState is what the index directory holds.
type IndexState struct {
	Path     string     `json:"path"`
	Built    bool       `json:"built"`
	Format   string     `json:"format,omitempty"`
	BuiltAt  *time.Time `json:"built_at,omitempty"`
	Window   string     `json:"window,omitempty"`
	Towns    int        `json:"towns"`
	Rows     int        `json:"rows"`
	Refusals int        `json:"refusals"`
}

// SourceState is what the registry declares. The registry is data in the repo (sources/), so an empty
// one means no country has been added yet rather than that the install is broken.
type SourceState struct {
	Path      string   `json:"path"`
	Countries []string `json:"countries"`
	Platforms []string `json:"platforms"`
}

// Probe inspects the install. It never writes and never calls the network.
func Probe(indexDir, sourcesDir string, now time.Time) Report {
	rep := Report{
		Version:  version.String(),
		Go:       runtime.Version(),
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Now:      now.UTC(),
		Problems: []string{},
	}

	rep.Index.Path = indexDir
	if _, err := os.Stat(filepath.Join(indexDir, index.ManifestFile)); err == nil {
		report, verr := index.Verify(indexDir, now)
		switch {
		case verr != nil:
			rep.Problems = append(rep.Problems, "index: "+verr.Error())
		default:
			rep.Index.Built = true
			rep.Index.Format = report.Format
			rep.Index.BuiltAt = report.BuiltAt
			rep.Index.Towns = report.Towns
			rep.Index.Rows = report.Rows
			rep.Index.Refusals = len(report.Problems)
			if report.Window != nil {
				rep.Index.Window = report.Window.From.Format("2006-01-02") + " → " + report.Window.To.Format("2006-01-02")
			}
			if !report.OK() {
				rep.Problems = append(rep.Problems, "index: refuses to publish — run `lineup verify` for the list")
			}
		}
	}

	rep.Sources.Path = sourcesDir
	rep.Sources.Countries = tomlNames(filepath.Join(sourcesDir, "countries"))
	rep.Sources.Platforms = tomlNames(filepath.Join(sourcesDir, "platforms"))

	return rep
}

// tomlNames lists the registry files by their base names, sorted. A missing directory is an empty
// list: the registry grows one country at a time.
func tomlNames(dir string) []string {
	names := []string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return names
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".toml") {
			continue
		}
		names = append(names, strings.TrimSuffix(name, ".toml"))
	}
	sort.Strings(names)
	return names
}
