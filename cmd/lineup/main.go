// Command lineup is the miner, the index publisher and the local server for Messer Lineup.
//
// P0 scaffold state: only `version` is wired. The subcommands land in P0.5 and on
// (mine, publish, verify, doctor, serve, findings) — see .hermes/plans/2026-11-17_start-here-p0.md.
// Nothing here reads the network or the clock on import.
package main

import (
	"fmt"
	"os"

	"github.com/jakobbjelver/messer-lineup/internal/version"
)

const usage = `lineup — Messer Lineup miner and index

  lineup version     print the build stamp

Subcommands (mine, publish, verify, doctor, serve, findings) arrive in P0.5.
`

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch args[0] {
	case "version", "-v", "--version":
		fmt.Println("lineup " + version.String())
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "lineup: unknown command %q\n\n%s", args[0], usage)
		os.Exit(2)
	}
}
