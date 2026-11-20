// Command lineup is the miner, the index publisher and the local server for Messer Lineup: one binary,
// no daemon, no API keys.
//
// The clock is injected at this boundary and nowhere else — every timestamp the program uses comes from
// the clock this file constructs, so `--now` makes a whole run reproducible and `make clock-check`
// keeps the wall-clock read inside internal/clock.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/clock"
	"github.com/jakobbjelver/messer-lineup/internal/doctor"
	"github.com/jakobbjelver/messer-lineup/internal/fetch"
	"github.com/jakobbjelver/messer-lineup/internal/index"
	"github.com/jakobbjelver/messer-lineup/internal/mine"
	"github.com/jakobbjelver/messer-lineup/internal/portraits"
	"github.com/jakobbjelver/messer-lineup/internal/publish"
	"github.com/jakobbjelver/messer-lineup/internal/server"
	"github.com/jakobbjelver/messer-lineup/internal/sources/dice"
	"github.com/jakobbjelver/messer-lineup/internal/sources/tickster"
	"github.com/jakobbjelver/messer-lineup/internal/state"
	"github.com/jakobbjelver/messer-lineup/internal/version"
)

// Exit codes. 0 is clean, 1 is a refusal (the thing could not be done, and the reason is on stderr),
// 2 is a usage error.
const (
	exitOK      = 0
	exitRefused = 1
	exitUsage   = 2
)

// Defaults. The index is a build artifact, not source: it lives in a gitignored directory and is
// published to object storage from there.
const (
	defaultIndexDir   = ".lineup-index"
	defaultSourcesDir = "sources"
)

type command struct {
	name    string
	summary string
	run     func(args []string, stdout, stderr io.Writer) int
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func commands() []command {
	return []command{
		{"verify", "check a built index against the published contract", runVerify},
		{"doctor", "report the state of this install", runDoctor},
		{"mine", "sweep a platform and write the report a publish is based on", runMine},
		{"publish", "copy a verified index into the public bucket", runPublish},
		{"serve", "serve the index (and the board, once it is built) locally", runServe},
		{"portraits", "resolve artist portraits (MusicBrainz → Wikimedia Commons) into the index", runPortraits},
		{"findings", "emit the parser findings an agent acts on (P8)", notWired("P8", "the autonomous warden")},
		{"version", "print the build stamp", runVersion},
	}
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage(""))
		return exitUsage
	}
	switch args[0] {
	case "-h", "--help", "help":
		if len(args) > 1 {
			fmt.Fprint(stdout, usage(args[1]))
			return exitOK
		}
		fmt.Fprint(stdout, usage(""))
		return exitOK
	}
	for _, c := range commands() {
		if c.name == args[0] {
			return c.run(args[1:], stdout, stderr)
		}
	}
	fmt.Fprintf(stderr, "lineup: unknown command %q\n\n%s", args[0], usage(""))
	return exitUsage
}

func usage(only string) string {
	var b strings.Builder
	if only != "" {
		for _, c := range commands() {
			if c.name == only {
				fmt.Fprintf(&b, "lineup %s — %s\n\nEvery command takes --json, and --now to pin the clock.\n", c.name, c.summary)
				return b.String()
			}
		}
		return "lineup: no such command " + only + "\n"
	}
	fmt.Fprint(&b, "lineup — Messer Lineup miner and index\n\n  lineup <command> [flags]\n\n")
	for _, c := range commands() {
		fmt.Fprintf(&b, "  %-9s %s\n", c.name, c.summary)
	}
	fmt.Fprint(&b, "\nFlags common to the commands that read an index: --index <dir>, --json, --now <RFC 3339 | YYYY-MM-DD>\n")
	return b.String()
}

// notWired is how a command the plan has not reached yet behaves: it says which phase fills it and
// exits non-zero, so a script that depends on it fails loudly instead of reading an empty success.
func runServe(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("lineup serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	indexDir := fs.String("index", ".lineup-index", "the index to serve")
	addr := fs.String("addr", "127.0.0.1:8016", "listen address")
	webDir := fs.String("web", "", "the built board (web/dist); empty serves the index only")
	nowFlag := fs.String("now", "", "pin the clock (also accepted as --frozen-clock for fixtures)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if len(*webDir) > 0 {
		if info, err := os.Stat(filepath.Join(*webDir, "index.html")); err != nil || info.IsDir() {
			fmt.Fprintf(stderr, "lineup serve: %s has no index.html\n", *webDir)
			return exitUsage
		}
	}
	clk, ok := (&flags{now: nowFlag}).clock(stderr)
	if !ok {
		return exitUsage
	}
	fmt.Fprintf(stderr, "lineup serve: index %s, board %s\n", *indexDir, orDefault(*webDir, "none yet"))
	if err := server.Run(context.Background(), server.Options{
		IndexDir: *indexDir,
		WebDir:   *webDir,
		Now:      clk.Now,
		Log:      stdout,
	}, *addr); err != nil {
		fmt.Fprintf(stderr, "lineup serve: %v\n", err)
		return exitRefused
	}
	return exitOK
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// runPortraits resolves the published artists' faces. It is a separate step from `mine` on purpose: a portrait is
// not an event fact, and a sweep must be able to publish without waiting on 500 lookups at one request per second.
func runPortraits(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("lineup portraits", flag.ContinueOnError)
	fs.SetOutput(stderr)
	indexDir := fs.String("index", ".lineup-index", "the built index whose artists to resolve")
	statePath := fs.String("state", "", "where resolved artists are remembered (default .lineup-state/portraits.json)")
	out := fs.String("out", "", "the published file (default <index>/artists/portraits.json)")
	limit := fs.Int("limit", 0, "resolve at most N new artists this run (0 = all)")
	var only stringList
	fs.Var(&only, "only", "resolve these names instead of the index: repeatable, or comma-separated (a probe, not a build)")
	dryRun := fs.Bool("dry-run", false, "report what would be resolved; write nothing")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	report, err := portraits.Run(context.Background(), portraits.Options{
		IndexDir:  *indexDir,
		StatePath: *statePath,
		Out:       *out,
		Limit:     *limit,
		Only:      only,
		DryRun:    *dryRun,
		Log:       stderr,
	})
	if err != nil {
		fmt.Fprintf(stderr, "lineup portraits: %v\n", err)
		return exitRefused
	}
	if *asJSON {
		if err := writeJSON(stdout, report); err != nil {
			fmt.Fprintf(stderr, "lineup portraits: %v\n", err)
			return exitRefused
		}
		return exitOK
	}
	fmt.Fprintf(stdout, "%d artists, %d already known, %d resolved, %d without a portrait, %d to retry%s\n",
		report.Indexed, report.Cached, report.Found, report.Missed, report.Retry,
		map[bool]string{true: " (dry run)"}[report.DryRun])
	if !report.DryRun {
		fmt.Fprintf(stdout, "wrote %s: %d portraits, %d bytes\n", report.Path, report.Written, report.Bytes)
	}
	return exitOK
}

func runPublish(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("lineup publish", flag.ContinueOnError)
	fs.SetOutput(stderr)
	indexDir := fs.String("index", ".lineup-index", "the index to publish")
	dryRun := fs.Bool("dry-run", false, "list what would be uploaded; touch nothing")
	asJSON := fs.Bool("json", false, "print the result as JSON")
	force := fs.Bool("force", false, "publish even when the guards refuse: the operator has looked at the numbers")
	nowFlag := fs.String("now", "", "pin the clock: RFC 3339 or YYYY-MM-DD")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	clk, ok := (&flags{now: nowFlag}).clock(stderr)
	if !ok {
		return exitUsage
	}

	// Verify first: an index the contract rejects must not reach a public URL, whatever the exit code says.
	report, err := index.Verify(*indexDir, clk.Now())
	if err != nil {
		fmt.Fprintf(stderr, "lineup publish: %v\n", err)
		return exitRefused
	}
	if !report.OK() {
		fmt.Fprintln(stderr, "lineup publish: the index does not verify, so it will not be published:")
		for _, problem := range report.Problems {
			fmt.Fprintf(stderr, "  %s\n", problem)
		}
		return exitRefused
	}

	result, err := publish.Run(context.Background(), publish.Options{
		IndexDir:  *indexDir,
		Endpoint:  os.Getenv("LINEUP_R2_ENDPOINT"),
		Bucket:    envOr("LINEUP_R2_BUCKET", "messer-lineup-index"),
		AccessKey: os.Getenv("LINEUP_R2_ACCESS_KEY_ID"),
		SecretKey: os.Getenv("LINEUP_R2_SECRET_ACCESS_KEY"),
		DryRun:    *dryRun,
		NoGuard:   *force,
		Now:       clk.Now(),
		Log:       stderr,
	})
	if err != nil {
		fmt.Fprintf(stderr, "lineup publish: %v\n", err)
		return exitRefused
	}
	if *asJSON {
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "lineup publish: %v\n", err)
			return exitRefused
		}
		return exitOK
	}
	mode := "published"
	if result.DryRun {
		mode = "would publish"
	}
	fmt.Fprintf(stdout, "%s %d of %d object(s), %d skipped, %d bytes\n",
		mode, len(result.Keys), result.Considered, result.Skipped, result.Bytes)
	return exitOK
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func notWired(phase, what string) func([]string, io.Writer, io.Writer) int {
	return func(args []string, stdout, stderr io.Writer) int {
		fmt.Fprintf(stderr, "lineup: this command is not wired yet — %s arrives in %s.\n", what, phase)
		return exitRefused
	}
}

// flags bundles what every reading command accepts, so --json and --now mean the same thing everywhere.
type flags struct {
	set     *flag.FlagSet
	index   *string
	json    *bool
	now     *string
	sources *string
}

func newFlags(name string, stderr io.Writer) *flags {
	set := flag.NewFlagSet("lineup "+name, flag.ContinueOnError)
	set.SetOutput(stderr)
	f := &flags{set: set}
	f.index = set.String("index", defaultIndexDir, "directory holding manifest.json, towns.json and the shards")
	f.json = set.Bool("json", false, "machine-readable output")
	f.now = set.String("now", "", "pin the clock: RFC 3339 or YYYY-MM-DD (default: the system clock)")
	return f
}

func (f *flags) clock(stderr io.Writer) (clock.Clock, bool) {
	if *f.now == "" {
		return clock.System{}, true
	}
	fixed, err := clock.Parse(*f.now)
	if err != nil {
		fmt.Fprintf(stderr, "lineup: %v\n", err)
		return nil, false
	}
	return fixed, true
}

func runVerify(args []string, stdout, stderr io.Writer) int {
	f := newFlags("verify", stderr)
	if err := f.set.Parse(args); err != nil {
		return exitUsage
	}
	if f.set.NArg() > 0 {
		fmt.Fprintf(stderr, "lineup verify: unexpected argument %q\n", f.set.Arg(0))
		return exitUsage
	}
	clk, ok := f.clock(stderr)
	if !ok {
		return exitUsage
	}
	report, err := index.Verify(*f.index, clk.Now())
	if err != nil {
		fmt.Fprintf(stderr, "lineup verify: %v\n", err)
		return exitRefused
	}
	if *f.json {
		if err := writeJSON(stdout, report); err != nil {
			fmt.Fprintf(stderr, "lineup verify: %v\n", err)
			return exitRefused
		}
	} else {
		writeVerifyText(stdout, report)
	}
	if !report.OK() {
		return exitRefused
	}
	return exitOK
}

func writeVerifyText(out io.Writer, rep index.Report) {
	fmt.Fprintf(out, "%s\n", rep.Dir)
	if rep.BuiltAt != nil {
		basis := fmt.Sprintf("  built %s · %d town(s) · %d row(s) · format %s",
			rep.BuiltAt.Format(time.RFC3339), rep.Towns, rep.Rows, rep.Format)
		if rep.Window != nil {
			basis += fmt.Sprintf(" · window %s → %s (%d days)",
				rep.Window.From.Format("2006-01-02"), rep.Window.To.Format("2006-01-02"), rep.Window.Days)
		}
		fmt.Fprintln(out, basis)
	}
	if rep.OK() {
		fmt.Fprintln(out, "  VERIFIED — this index may be published")
		return
	}
	fmt.Fprintf(out, "  REFUSED — %d problem(s):\n", len(rep.Problems))
	for _, p := range rep.Problems {
		fmt.Fprintf(out, "    %s: %s\n", p.Where, p.What)
	}
}

func runDoctor(args []string, stdout, stderr io.Writer) int {
	f := newFlags("doctor", stderr)
	f.sources = f.set.String("sources", defaultSourcesDir, "directory holding the source registry")
	if err := f.set.Parse(args); err != nil {
		return exitUsage
	}
	if f.set.NArg() > 0 {
		fmt.Fprintf(stderr, "lineup doctor: unexpected argument %q\n", f.set.Arg(0))
		return exitUsage
	}
	clk, ok := f.clock(stderr)
	if !ok {
		return exitUsage
	}
	rep := doctor.Probe(*f.index, *f.sources, clk.Now())
	if *f.json {
		if err := writeJSON(stdout, rep); err != nil {
			fmt.Fprintf(stderr, "lineup doctor: %v\n", err)
			return exitRefused
		}
	} else {
		writeDoctorText(stdout, rep)
	}
	if len(rep.Problems) > 0 {
		return exitRefused
	}
	return exitOK
}

func writeDoctorText(out io.Writer, rep doctor.Report) {
	rows := [][2]string{
		{"version", rep.Version},
		{"go", rep.Go + " " + rep.OS + "/" + rep.Arch},
		{"clock", rep.Now.Format(time.RFC3339)},
	}
	indexLine := rep.Index.Path + " — not built (no manifest.json)"
	if rep.Index.Built {
		indexLine = fmt.Sprintf("%s — %d town(s), %d row(s)", rep.Index.Path, rep.Index.Towns, rep.Index.Rows)
		if rep.Index.Window != "" {
			indexLine += ", window " + rep.Index.Window
		}
		if rep.Index.Refusals > 0 {
			indexLine += fmt.Sprintf(" — %d refusal(s), see `lineup verify`", rep.Index.Refusals)
		}
	}
	rows = append(rows, [2]string{"index", indexLine})
	rows = append(rows, [2]string{"sources", fmt.Sprintf("%s — %d country file(s), %d platform file(s)",
		rep.Sources.Path, len(rep.Sources.Countries), len(rep.Sources.Platforms))})
	if len(rep.Sources.Countries) > 0 {
		rows = append(rows, [2]string{"countries", strings.Join(rep.Sources.Countries, " ")})
	}
	width := 0
	for _, r := range rows {
		if len(r[0]) > width {
			width = len(r[0])
		}
	}
	for _, r := range rows {
		fmt.Fprintf(out, "  %-*s  %s\n", width, r[0], r[1])
	}
	for _, p := range rep.Problems {
		fmt.Fprintf(out, "  ! %s\n", p)
	}
}

func runVersion(args []string, stdout, stderr io.Writer) int {
	set := flag.NewFlagSet("lineup version", flag.ContinueOnError)
	set.SetOutput(stderr)
	asJSON := set.Bool("json", false, "machine-readable output")
	if err := set.Parse(args); err != nil {
		return exitUsage
	}
	if *asJSON {
		if err := writeJSON(stdout, map[string]string{
			"version": version.Version, "commit": version.Commit, "build_date": version.BuildDate,
		}); err != nil {
			fmt.Fprintf(stderr, "lineup version: %v\n", err)
			return exitRefused
		}
		return exitOK
	}
	fmt.Fprintln(stdout, "lineup "+version.String())
	return exitOK
}

// stringList is a repeatable flag: --town berlin --town london, and --town berlin,london.
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(value string) error {
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			*s = append(*s, part)
		}
	}
	return nil
}

func runMine(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("lineup mine", flag.ContinueOnError)
	fs.SetOutput(stderr)
	sources := fs.String("source", "dice", "platforms to read: dice, tickster (comma-separated)")
	var towns stringList
	fs.Var(&towns, "town", "town slug; repeatable, or comma-separated")
	countries := fs.String("country", "", "keep only towns in these countries (dk,se)")
	dryRun := fs.Bool("dry-run", false, "measure and report; write no index")
	indexDir := fs.String("index", ".lineup-index", "where to write the index (unless --dry-run)")
	prune := fs.Bool("prune", true, "delete shards the new manifest does not name")
	reportPath := fs.String("report", "", "write the run report (JSON) here")
	coveragePath := fs.String("coverage", "", "rewrite the measured block of this coverage file")
	maxPerTown := fs.Int("max-per-town", 0, "cap candidates per town and source (0 = everything listed)")
	statePath := fs.String("state", ".lineup-state/state.json", "the miner's memory: rate budget and yield history")
	budget := fs.Int("budget", 0, "requests per source per day (0 = no cap)")
	interval := fs.Duration("interval", 0, "per-request gap (0 = the client default)")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	nowFlag := fs.String("now", "", "pin the clock: RFC 3339 or YYYY-MM-DD")
	fromReport := fs.String("from", "", "render a saved report (JSON) instead of sweeping")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "lineup mine: unexpected argument %q\n", fs.Arg(0))
		return exitUsage
	}
	// A saved report can be re-rendered without touching the network: the coverage record is written from a
	// measurement, and re-running a sweep to rewrite its own table would be a second measurement, not the same
	// one.
	if *fromReport != "" {
		report, err := readReport(*fromReport)
		if err != nil {
			fmt.Fprintf(stderr, "lineup mine: %v\n", err)
			return exitRefused
		}
		return writeMineOutputs(stdout, stderr, report, *reportPath, *coveragePath, *asJSON)
	}
	if len(towns) == 0 {
		fmt.Fprintln(stderr, "lineup mine: name at least one town: --town copenhagen")
		return exitUsage
	}
	clk, ok := (&flags{now: nowFlag}).clock(stderr)
	if !ok {
		return exitUsage
	}
	client := fetch.New(fetch.Options{Interval: *interval})
	adapters, err := buildAdapters(client, *sources)
	if err != nil {
		fmt.Fprintf(stderr, "lineup mine: %v\n", err)
		return exitUsage
	}
	mem, err := state.Open(*statePath)
	if err != nil {
		fmt.Fprintf(stderr, "lineup mine: %v\n", err)
		return exitRefused
	}

	report, err := mine.Run(context.Background(), mine.Options{
		Adapters:     adapters,
		Towns:        towns,
		Countries:    []string{*countries},
		DryRun:       *dryRun,
		IndexDir:     *indexDir,
		Prune:        *prune,
		Interval:     *interval,
		MaxPerTown:   *maxPerTown,
		Now:          clk.Now(),
		Log:          stderr,
		State:        mem,
		BudgetPerDay: *budget,
	})
	if err != nil {
		// A run that died still measured something; the report is written before the complaint.
		_ = report.WriteFile(*reportPath)
		fmt.Fprintf(stderr, "lineup mine: %v\n", err)
		return exitRefused
	}

	if code := writeMineOutputs(stdout, stderr, report, *reportPath, *coveragePath, *asJSON); code != exitOK {
		return code
	}

	if report.Totals.Rows == 0 {
		fmt.Fprintln(stderr, "lineup mine: the run produced no rows")
		return exitRefused
	}
	return exitOK
}

// readReport loads a report written by an earlier run.
func readReport(path string) (mine.Report, error) {
	var report mine.Report
	raw, err := os.ReadFile(path)
	if err != nil {
		return report, err
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		return report, fmt.Errorf("%s: %w", path, err)
	}
	return report, nil
}

// writeMineOutputs is everything a run does with its report: the file, the coverage block, stdout.
func writeMineOutputs(stdout, stderr io.Writer, report mine.Report, reportPath, coveragePath string, asJSON bool) int {
	if reportPath != "" {
		if err := report.WriteFile(reportPath); err != nil {
			fmt.Fprintf(stderr, "lineup mine: writing %s: %v\n", reportPath, err)
			return exitRefused
		}
	}
	if coveragePath != "" {
		if err := mine.UpdateCoverageFile(coveragePath, report); err != nil {
			fmt.Fprintf(stderr, "lineup mine: %v\n", err)
			return exitRefused
		}
	}
	if asJSON {
		if err := writeJSON(stdout, report); err != nil {
			fmt.Fprintf(stderr, "lineup mine: %v\n", err)
			return exitRefused
		}
		return exitOK
	}
	writeMineText(stdout, report)
	return exitOK
}

// buildAdapters turns source ids into readers. A name that is not here is a usage error, not a silent skip.
func buildAdapters(client *fetch.Client, list string) ([]mine.Adapter, error) {
	var out []mine.Adapter
	for _, raw := range strings.Split(list, ",") {
		switch id := strings.ToLower(strings.TrimSpace(raw)); id {
		case "":
			continue
		case "dice":
			out = append(out, dice.New(client))
		case "tickster":
			out = append(out, tickster.New(client))
		default:
			return nil, fmt.Errorf("unknown source %q (known: dice, tickster)", id)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no source named")
	}
	return out, nil
}

func writeMineText(out io.Writer, rep mine.Report) {
	mode := "measured"
	if rep.DryRun {
		mode = "dry run"
	}
	fmt.Fprintf(out, "%s · %s · %s\n", strings.Join(rep.Sources, ", "), mode, rep.StartedAt.Format(time.RFC3339))
	for _, town := range rep.Towns {
		for _, src := range town.Sources {
			if src.Error != "" {
				fmt.Fprintf(out, "  %-12s %-9s could not be read: %s\n", town.Slug, src.ID, src.Error)
				continue
			}
			fmt.Fprintf(out, "  %-12s %-9s %3d candidates · %3d rows · %2d filtered · %d failures · %.0fs%s\n",
				town.Slug, src.ID, src.Candidates, src.Rows, src.Filtered, sumFailures(src.Failures), src.Seconds,
				mine.QueueNote(src))
			if src.Warning != "" {
				fmt.Fprintf(out, "  %-12s %-9s ⚠ %s\n", "", "", src.Warning)
			}
		}
	}
	if rep.Totals.Retried > 0 || rep.Totals.Queued > 0 || rep.Totals.GivenUp > 0 {
		fmt.Fprintf(out, "  %-12s %-9s %d retried · %d queued for the next run · %d given up\n",
			"queue", "", rep.Totals.Retried, rep.Totals.Queued, rep.Totals.GivenUp)
	}
	fmt.Fprintf(out, "  %-12s %-9s %3d candidates · %3d rows · %3d towns discovered\n",
		"total", "", rep.Totals.Candidates, rep.Totals.Rows, rep.TownCount)
	if rep.Merge.Raw > 0 {
		fmt.Fprintf(out, "  merged %d → %d shows, duplicate rate %.1f %%\n",
			rep.Merge.Raw, rep.Merge.Out, 100*rep.Merge.DuplicateRate)
	}
	if rep.Build != nil {
		fmt.Fprintf(out, "  wrote %s: %d shard(s), %d rows, %d bytes\n",
			rep.Build.Dir, rep.Build.Shards, rep.Build.Published, rep.Build.Bytes)
	}
	if rep.Note != "" {
		fmt.Fprintf(out, "  note  %s\n", rep.Note)
	}
}

func sumFailures(m map[string]int) int {
	n := 0
	for _, count := range m {
		n += count
	}
	return n
}

func writeJSON(out io.Writer, value any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}
