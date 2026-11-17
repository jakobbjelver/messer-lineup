// Package version carries the build stamp. The values are set at link time by the
// Makefile (-X ...internal/version.Version=...) and are never written by hand at runtime.
package version

import "strings"

var (
	// Version is the git describe of the build, or "dev" for a working tree.
	Version = "dev"
	// Commit is the short SHA, or "none" outside a repository.
	Commit = "none"
	// BuildDate is the UTC build timestamp.
	BuildDate = "unknown"
)

// String renders the one-line stamp: dev (none) 2026-11-17T18:00:00Z.
func String() string {
	return strings.Join([]string{Version, "(" + Commit + ")", BuildDate}, " ")
}
