// Package clock is the only place in the program that reads the wall clock.
//
// Every selection path takes the time as a parameter — the board's window, "new since last visit", a
// calendar's date range — because every user-input scenario asserts a date window and a function that
// calls time.Now() internally cannot be tested against one. `make clock-check` enforces the boundary:
// time.Now() may appear in this package and nowhere else.
package clock

import (
	"fmt"
	"time"
)

// Clock is what the program asks for the time. One interface, two implementations: the real one at
// the CLI boundary and a fixed one in tests, fixtures and `lineup serve --frozen-clock`.
type Clock interface {
	Now() time.Time
}

// System is the real clock. It is constructed once, in main, and passed down.
type System struct{}

// Now returns the current time in UTC.
func (System) Now() time.Time { return time.Now().UTC() }

// Now is the wall clock as a plain function, for code that takes the clock as an argument rather than
// an interface. It exists so nothing outside this package has to name the standard library's clock.
func Now() time.Time { return time.Now().UTC() }

// Fixed is a clock stuck at one instant.
type Fixed struct {
	T time.Time
}

// Now returns the fixed instant.
func (f Fixed) Now() time.Time { return f.T.UTC() }

// Parse reads an instant from a flag: RFC 3339, or a bare date (midnight UTC). A bare date is what a
// fixture and a `--now` given by hand usually are.
func Parse(s string) (Fixed, error) {
	if s == "" {
		return Fixed{}, fmt.Errorf("clock: empty value")
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return Fixed{T: t.UTC()}, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return Fixed{}, fmt.Errorf("clock: %q is neither RFC 3339 nor YYYY-MM-DD", s)
	}
	return Fixed{T: t.UTC()}, nil
}
