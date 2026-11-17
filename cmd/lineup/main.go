// Command lineup is the miner, the index publisher and the local server for Messer Lineup: one binary,
// no daemon, no API keys.
//
// The clock is injected at this boundary and nowhere else — every timestamp the program uses comes from
// the clock this file constructs, so `--now` makes a whole run reproducible and `make clock-check`
// keeps the wall-clock read inside internal/clock.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/clock"
	"github.com/jakobbjelver/messer-lineup/internal/doctor"
	"github.com/jakobbjelver/messer-lineup/internal/index"
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
		{"mine", "walk the sources and write an index (P0.7)", notWired("P0.7", "the first adapter")},
		{"publish", "copy a verified index to object storage (P3)", notWired("P3", "the object-storage publisher")},
		{"serve", "serve the index and the board locally (P3)", notWired("P3", "the API and the board")},
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

func writeJSON(out io.Writer, value any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}
