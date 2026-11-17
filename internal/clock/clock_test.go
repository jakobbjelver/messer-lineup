package clock

import (
	"testing"
	"time"
)

func TestFixedDoesNotMove(t *testing.T) {
	at := time.Date(2026, 11, 17, 6, 12, 0, 0, time.UTC)
	c := Fixed{T: at}
	if !c.Now().Equal(at) {
		t.Fatalf("Fixed.Now() = %v, want %v", c.Now(), at)
	}
	// A clock handed to a selector must not be able to drift mid-run.
	if !c.Now().Equal(c.Now()) {
		t.Fatal("Fixed returned two different instants")
	}
}

func TestParseAcceptsTheTwoForms(t *testing.T) {
	full, err := Parse("2026-11-17T06:12:00+01:00")
	if err != nil {
		t.Fatalf("RFC 3339: %v", err)
	}
	if want := time.Date(2026, 11, 17, 5, 12, 0, 0, time.UTC); !full.Now().Equal(want) {
		t.Fatalf("offset not normalised: %v", full.Now())
	}
	day, err := Parse("2026-11-17")
	if err != nil {
		t.Fatalf("date: %v", err)
	}
	if want := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC); !day.Now().Equal(want) {
		t.Fatalf("date is not midnight UTC: %v", day.Now())
	}
}

func TestParseRejectsTheRest(t *testing.T) {
	for _, in := range []string{"", "yesterday", "17/11/2026", "2026-13-01"} {
		if _, err := Parse(in); err == nil {
			t.Fatalf("Parse(%q) succeeded, want an error", in)
		}
	}
}

func TestSystemIsUTC(t *testing.T) {
	if got := (System{}).Now(); got.Location() != time.UTC {
		t.Fatalf("System.Now() is in %v, want UTC", got.Location())
	}
}
