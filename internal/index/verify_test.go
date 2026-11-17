package index

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/model"
)

// fixture is an index directory described before it is written, so a test can break exactly one thing.
type fixture struct {
	towns  []model.Town
	shards map[string][]string // town id → raw JSON lines
	window model.Window
}

const testTown = "copenhagen-dk"

// testNow is the clock a verdict is taken at. Fixed, because a verifier that answers differently at
// different wall times is a verifier nobody can assert on.
var testNow = time.Date(2026, 11, 17, 19, 0, 0, 0, time.UTC)

func goodTown() model.Town {
	return model.Town{ID: testTown, Name: "København", Country: "dk", Lat: 55.6761, Lon: 12.5683, TZ: "Europe/Copenhagen"}
}

func window() model.Window {
	from, _ := time.Parse(dayLayout, "2026-11-17")
	to, _ := time.Parse(dayLayout, "2027-03-17")
	return model.Window{From: from, To: to, Days: 120}
}

func goodRowJSON(townID string) string {
	start, _ := time.Parse(time.RFC3339, "2026-11-20T20:00:00+01:00")
	row, err := model.Project(model.Show{
		Artist: "Viagra Boys", ArtistKey: "viagra-boys",
		Venue: "Vega", VenueKey: "vega-cph",
		Town: townID, Start: start, Status: model.OnSale,
		URL: "https://www.billetlugen.dk/concert/x", Sources: []string{"billetlugen"},
		Geo: &model.Geo{Lat: 55.6707, Lon: 12.5503},
	})
	if err != nil {
		panic(err)
	}
	out, err := json.Marshal(row)
	if err != nil {
		panic(err)
	}
	return string(out)
}

// write lays the fixture down and returns the manifest it published.
func (f fixture) write(t *testing.T, dir string) model.Manifest {
	t.Helper()
	built := time.Date(2026, 11, 17, 6, 12, 0, 0, time.UTC)
	towns := model.Towns{Format: model.FormatTowns, BuiltAt: built, Towns: f.towns}
	if towns.Towns == nil {
		towns.Towns = []model.Town{}
	}
	writeJSON(t, filepath.Join(dir, TownsFile), towns)

	manifest := model.Manifest{Format: model.FormatIndex, BuiltAt: built, Window: f.window, Sources: []model.Source{
		{ID: "billetlugen", Index: "sitemap", OK: true, ParseYield: 0.9, LastOK: built, Rows: 1},
	}}
	for town, lines := range f.shards {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		for _, line := range lines {
			if _, err := gz.Write([]byte(line + "\n")); err != nil {
				t.Fatalf("gzip: %v", err)
			}
		}
		if err := gz.Close(); err != nil {
			t.Fatalf("gzip close: %v", err)
		}
		sum := sha256.Sum256(buf.Bytes())
		digest := hex.EncodeToString(sum[:])
		rel := "towns/" + town + "/" + digest[:16] + ".jsonl.gz"
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, buf.Bytes(), 0o644); err != nil {
			t.Fatalf("write shard: %v", err)
		}
		manifest.Towns = append(manifest.Towns, model.Index{
			Town: town, Shard: rel, SHA256: digest, Count: len(lines),
			Sources: []string{"billetlugen"}, BuiltAt: built,
		})
	}
	writeJSON(t, filepath.Join(dir, ManifestFile), manifest)
	return manifest
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	out, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %s: %v", filepath.Base(path), err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("write %s: %v", filepath.Base(path), err)
	}
}

func verify(t *testing.T, f fixture) Report {
	t.Helper()
	dir := t.TempDir()
	f.write(t, dir)
	rep, err := Verify(dir, testNow)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	return rep
}

func problems(rep Report) string {
	var b strings.Builder
	for _, p := range rep.Problems {
		b.WriteString(p.String())
		b.WriteString("\n")
	}
	return b.String()
}

func TestVerifyAcceptsAGoodIndex(t *testing.T) {
	rep := verify(t, fixture{
		towns:  []model.Town{goodTown()},
		shards: map[string][]string{testTown: {goodRowJSON(testTown), goodRowJSON(testTown)}},
		window: window(),
	})
	if !rep.OK() {
		t.Fatalf("a good index was refused:\n%s", problems(rep))
	}
	if rep.Rows != 2 || rep.Towns != 1 {
		t.Fatalf("counts: %d rows, %d towns", rep.Rows, rep.Towns)
	}
	if rep.Format != model.FormatIndex || rep.BuiltAt == nil || rep.Window == nil {
		t.Fatalf("report is missing its metadata: %+v", rep)
	}
}

func TestVerifyRefusesAnEmptyIndex(t *testing.T) {
	rep := verify(t, fixture{window: window(), towns: []model.Town{}, shards: map[string][]string{}})
	if rep.OK() {
		t.Fatal("an index with no towns and no rows was accepted")
	}
	if !strings.Contains(problems(rep), "empty index") {
		t.Fatalf("refusal does not say the index is empty:\n%s", problems(rep))
	}
}

func TestVerifyRefusesAMissingManifest(t *testing.T) {
	rep, err := Verify(t.TempDir(), testNow)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if rep.OK() || !strings.Contains(problems(rep), "no manifest") {
		t.Fatalf("a directory with no manifest was accepted:\n%s", problems(rep))
	}
}

func TestVerifyRefusesATownWithoutRows(t *testing.T) {
	rep := verify(t, fixture{
		towns:  []model.Town{goodTown()},
		shards: map[string][]string{testTown: {}},
		window: window(),
	})
	// A shard with no rows still has a digest and a count of zero; both are refusals.
	if rep.OK() {
		t.Fatal("a town with no rows was accepted")
	}
	if !strings.Contains(problems(rep), "has no rows") {
		t.Fatalf("refusal does not name the town with no rows:\n%s", problems(rep))
	}
}

func TestVerifyRefusesAZeroByteShard(t *testing.T) {
	dir := t.TempDir()
	f := fixture{towns: []model.Town{goodTown()}, shards: map[string][]string{testTown: {goodRowJSON(testTown)}}, window: window()}
	manifest := f.write(t, dir)
	shard := filepath.Join(dir, filepath.FromSlash(manifest.Towns[0].Shard))
	if err := os.WriteFile(shard, nil, 0o644); err != nil {
		t.Fatalf("truncate shard: %v", err)
	}
	rep, err := Verify(dir, testNow)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if rep.OK() || !strings.Contains(problems(rep), "empty object") {
		t.Fatalf("a zero-byte shard was accepted:\n%s", problems(rep))
	}
}

func TestVerifyRefusesAContentField(t *testing.T) {
	row := strings.Replace(goodRowJSON(testTown), `"st":"o"`, `"st":"o","p":[295,450]`, 1)
	rep := verify(t, fixture{
		towns:  []model.Town{goodTown()},
		shards: map[string][]string{testTown: {row}},
		window: window(),
	})
	got := problems(rep)
	if rep.OK() || !strings.Contains(got, `"p"`) || !strings.Contains(got, "nine keys") {
		t.Fatalf("a published price was accepted:\n%s", got)
	}
}

func TestVerifyRefusesATamperedShard(t *testing.T) {
	dir := t.TempDir()
	f := fixture{
		towns:  []model.Town{goodTown()},
		shards: map[string][]string{testTown: {goodRowJSON(testTown)}},
		window: window(),
	}
	manifest := f.write(t, dir)
	shard := filepath.Join(dir, filepath.FromSlash(manifest.Towns[0].Shard))
	raw, err := os.ReadFile(shard)
	if err != nil {
		t.Fatalf("read shard: %v", err)
	}
	raw = append(raw, 0)
	if err := os.WriteFile(shard, raw, 0o644); err != nil {
		t.Fatalf("write shard: %v", err)
	}
	rep, err := Verify(dir, testNow)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if rep.OK() || !strings.Contains(problems(rep), "sha256") {
		t.Fatalf("a shard that no longer matches its digest was accepted:\n%s", problems(rep))
	}
}

func TestVerifyRefusesRowsThatBreakTheContract(t *testing.T) {
	cases := map[string]struct{ mutate func(string) string }{
		"outside the window": {func(r string) string {
			return strings.Replace(r, "2026-11-20", "2028-01-01", -1)
		}},
		"key disagrees with the date": {func(r string) string {
			return strings.Replace(r, "|2026-11-20\"", "|2026-11-21\"", 1)
		}},
		"plain http": {func(r string) string {
			return strings.Replace(r, "https://", "http://", 1)
		}},
		"bad status": {func(r string) string {
			return strings.Replace(r, `"st":"o"`, `"st":"onsale"`, 1)
		}},
		"null position": {func(r string) string {
			return strings.Replace(r, `"g":[55.6707,12.5503]`, `"g":null`, 1)
		}},
		"no source": {func(r string) string {
			return strings.Replace(r, `"src":["billetlugen"],`, `"src":[],`, 1)
		}},
		"wrong town": {func(r string) string {
			return strings.Replace(r, `"c":"copenhagen-dk"`, `"c":"berlin-de"`, 1)
		}},
	}
	for name, c := range cases {
		f := fixture{
			towns:  []model.Town{goodTown()},
			shards: map[string][]string{testTown: {c.mutate(goodRowJSON(testTown))}},
			window: window(),
		}
		rep := verify(t, f)
		if rep.OK() {
			t.Fatalf("%s: the row was accepted", name)
		}
	}
}

func TestVerifyRefusesATownThePickerDoesNotKnow(t *testing.T) {
	f := fixture{
		towns:  []model.Town{},
		shards: map[string][]string{testTown: {goodRowJSON(testTown)}},
		window: window(),
	}
	rep := verify(t, f)
	if rep.OK() || !strings.Contains(problems(rep), "not in towns.json") {
		t.Fatalf("a town nobody can pick was accepted:\n%s", problems(rep))
	}
}

func TestValidKey(t *testing.T) {
	for _, good := range []string{"vega", "vega-cph", "blink-182", "x-ebd798351b"} {
		if !validKey(good) {
			t.Fatalf("%q should be a valid key", good)
		}
	}
	for _, bad := range []string{"", "-vega", "vega-", "ve--ga", "Vega", "vega cph", "vega_cph"} {
		if validKey(bad) {
			t.Fatalf("%q should be rejected", bad)
		}
	}
}
