package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jakobbjelver/messer-lineup/internal/clock"
	"github.com/jakobbjelver/messer-lineup/internal/findings"
	"github.com/jakobbjelver/messer-lineup/internal/mine"
)

// The ledger's flags are shared by the three places that touch it — `mine` raises, `publish` raises, `findings` reads
// and closes — so the path is derived the same way everywhere: beside the miner's state file unless a flag says
// otherwise. One directory names the miner's memory.
func findingsPath(explicit, statePath string) string {
	if explicit != "" {
		return explicit
	}
	if statePath != "" {
		return findings.DefaultPath(statePath)
	}
	return filepath.Join(".lineup-state", "findings.json")
}

// recordMineFindings turns a finished report into ledger rows.
//
// The conditions are the ones the run already concluded: a source that warned (yield collapse, a budget that ran out, a
// walk that stopped early) and a queued target that ran out of attempts. Nothing here re-derives anything — the evidence
// is the numbers the report carries, so `findings show` needs no second measurement to explain itself.
func recordMineFindings(store *findings.Store, report mine.Report) int {
	raised := 0
	for _, town := range report.Towns {
		for _, source := range town.Sources {
			if source.Warning != "" {
				if _, created := store.Raise(findings.Finding{
					Kind:     findings.KindYield,
					Source:   source.ID,
					Town:     town.Slug,
					OpenedAt: report.FinishedAt,
					Evidence: map[string]any{
						"warning":    source.Warning,
						"candidates": source.Candidates,
						"rows":       source.Rows,
						"seconds":    source.Seconds,
					},
				}); created {
					raised++
				}
			}
			if source.GivenUp > 0 {
				if _, created := store.Raise(findings.Finding{
					Kind:     findings.KindGivenUp,
					Source:   source.ID,
					Town:     town.Slug,
					OpenedAt: report.FinishedAt,
					Evidence: map[string]any{"given_up": source.GivenUp, "candidates": source.Candidates},
				}); created {
					raised++
				}
			}
		}
	}
	return raised
}

// recordPublishFinding files a refusal. A refusal is not an incident — the published index stays — but it is exactly the
// thing that used to live only in a 04:10 log line.
func recordPublishFinding(store *findings.Store, indexDir string, cause error, problems []string) {
	if store == nil {
		return
	}
	evidence := map[string]any{"reason": cause.Error(), "index": indexDir}
	if len(problems) > 0 {
		evidence["problems"] = problems
	}
	store.Raise(findings.Finding{
		Kind:     findings.KindRefused,
		Evidence: evidence,
		OpenedAt: clock.Now(),
	})
	_ = store.Save()
}

// runFindings is the reader and the closing hand of the ledger.
func runFindings(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, "lineup findings: name what to do: list, show <id>, close <id>\n")
		return exitUsage
	}
	sub, rest := args[0], args[1:]
	// Go's flag package stops at the first non-flag argument, so `show <id> --json` would leave the flags unparsed. The
	// id is pulled off the front before parsing, and `show --json <id>` still works through `fs.Arg(0)`.
	lead, rest := positionalFirst(rest)

	fs := flag.NewFlagSet("lineup findings "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("findings", "", "the ledger (default .lineup-state/findings.json)")
	statePath := fs.String("state", "", "the miner's state file; the ledger is written beside it")
	asJSON := fs.Bool("json", false, "print as JSON")
	openOnly := fs.Bool("open", false, "only what is still unresolved")
	note := fs.String("note", "", "why it is closed")
	nowFlag := fs.String("now", "", "pin the clock: RFC 3339 or YYYY-MM-DD")
	if err := fs.Parse(rest); err != nil {
		return exitUsage
	}
	clk, ok := (&flags{now: nowFlag}).clock(stderr)
	if !ok {
		return exitUsage
	}

	store, err := findings.Open(findingsPath(*path, *statePath))
	if err != nil {
		fmt.Fprintf(stderr, "lineup findings: %v\n", err)
		return exitRefused
	}

	switch sub {
	case "list":
		list := store.List(*openOnly)
		if *asJSON {
			if err := writeJSON(stdout, list); err != nil {
				fmt.Fprintf(stderr, "lineup findings: %v\n", err)
				return exitRefused
			}
			return exitOK
		}
		if len(list) == 0 {
			fmt.Fprintln(stdout, "no findings")
			return exitOK
		}
		for _, finding := range list {
			state := "open"
			if !finding.Open() {
				state = "closed"
			}
			fmt.Fprintf(stdout, "%-7s %-28s %s\n", state, finding.ID, summary(finding))
		}
		return exitOK

	case "show":
		id := lead
		if id == "" {
			id = fs.Arg(0)
		}
		if id == "" {
			fmt.Fprint(stderr, "lineup findings show: name one finding\n")
			return exitUsage
		}
		for _, finding := range store.List(false) {
			if finding.ID != id {
				continue
			}
			if *asJSON {
				if err := writeJSON(stdout, finding); err != nil {
					fmt.Fprintf(stderr, "lineup findings: %v\n", err)
					return exitRefused
				}
				return exitOK
			}
			fmt.Fprintf(stdout, "%s\n", finding.ID)
			fmt.Fprintf(stdout, "  kind    %s\n", finding.Kind)
			if finding.Source != "" {
				fmt.Fprintf(stdout, "  source  %s\n", finding.Source)
			}
			if finding.Town != "" {
				fmt.Fprintf(stdout, "  town    %s\n", finding.Town)
			}
			fmt.Fprintf(stdout, "  opened  %s\n", finding.OpenedAt.Format("2006-01-02 15:04"))
			if !finding.Open() {
				fmt.Fprintf(stdout, "  closed  %s\n", finding.ClosedAt.Format("2006-01-02 15:04"))
			}
			if finding.Note != "" {
				fmt.Fprintf(stdout, "  note    %s\n", finding.Note)
			}
			if raw, err := json.MarshalIndent(finding.Evidence, "  ", " "); err == nil {
				fmt.Fprintf(stdout, "  evidence %s\n", strings.TrimSpace(string(raw)))
			}
			return exitOK
		}
		fmt.Fprintf(stderr, "lineup findings: no such finding %q\n", id)
		return exitRefused

	case "close":
		id := lead
		if id == "" {
			id = fs.Arg(0)
		}
		if id == "" {
			fmt.Fprint(stderr, "lineup findings close: name one finding\n")
			return exitUsage
		}
		if err := store.Close(id, *note, clk.Now()); err != nil {
			fmt.Fprintf(stderr, "lineup findings: %v\n", err)
			return exitRefused
		}
		if err := store.Save(); err != nil {
			fmt.Fprintf(stderr, "lineup findings: %v\n", err)
			return exitRefused
		}
		fmt.Fprintf(stdout, "closed %s\n", id)
		return exitOK

	default:
		fmt.Fprintf(stderr, "lineup findings: unknown action %q (list, show, close)\n", sub)
		return exitUsage
	}
}

// summary is the one line `list` prints: what the finding is about, in as few words as the evidence allows.
func summary(finding findings.Finding) string {
	switch finding.Kind {
	case findings.KindYield:
		if warning, ok := finding.Evidence["warning"].(string); ok {
			return warning
		}
		return "yield collapse"
	case findings.KindRefused:
		if reason, ok := finding.Evidence["reason"].(string); ok {
			return reason
		}
		return "publish refused"
	case findings.KindGivenUp:
		if n, ok := finding.Evidence["given_up"].(float64); ok {
			return fmt.Sprintf("%.0f target(s) given up", n)
		}
		return "retries exhausted"
	}
	return finding.Kind
}

// positionalFirst lifts a leading non-flag argument off the front of a command's arguments.
func positionalFirst(args []string) (string, []string) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0], args[1:]
	}
	return "", args
}

// openFindings reads the ledger without failing a caller that has none: a missing file is "nothing to look at".
func openFindings(path string) *findings.Store {
	store, err := findings.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lineup: findings: %v\n", err)
		return nil
	}
	return store
}
