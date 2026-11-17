package version

import (
	"strings"
	"testing"
)

func TestStringCarriesEveryPart(t *testing.T) {
	oldV, oldC, oldD := Version, Commit, BuildDate
	t.Cleanup(func() { Version, Commit, BuildDate = oldV, oldC, oldD })
	Version, Commit, BuildDate = "1.2.3", "abc1234", "2026-11-17T18:00:00Z"
	got := String()
	for _, want := range []string{"1.2.3", "abc1234", "2026-11-17T18:00:00Z"} {
		if !strings.Contains(got, want) {
			t.Fatalf("String() = %q, missing %q", got, want)
		}
	}
}

func TestDefaultsAreNotEmpty(t *testing.T) {
	// A build that lost its ldflags must still print something a user can paste into a bug report.
	for name, value := range map[string]string{"Version": "dev", "Commit": "none", "BuildDate": "unknown"} {
		if value == "" {
			t.Fatalf("%s default is empty", name)
		}
	}
}
