package fetch

import (
	"strings"
	"testing"
	"time"
)

func TestRobotsWildcardGroup(t *testing.T) {
	r, err := ParseRobots(strings.NewReader(`
# a comment
User-agent: *
Disallow: /search
Disallow: /admin/
Allow: /admin/public
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cases := map[string]bool{
		"/":               true,
		"/events/123":     true,
		"/search":         false,
		"/search?q=opeth": false,
		"/admin/panel":    false,
		"/admin/public":   true, // the longer Allow wins
		"/administrate":   true, // a prefix match is on the whole segment, not the word
	}
	for path, want := range cases {
		if got := r.Allowed(path, "lineup"); got != want {
			t.Errorf("Allowed(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestRobotsNamedAgentBeatsWildcard(t *testing.T) {
	r, err := ParseRobots(strings.NewReader(`
User-agent: *
Disallow: /

User-agent: lineup
Allow: /
Disallow: /private
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !r.Allowed("/events", "lineup") {
		t.Fatal("the named group says allow, but the wildcard group was used")
	}
	if r.Allowed("/private/x", "lineup") {
		t.Fatal("the named group's Disallow was ignored")
	}
	// A crawler the file does not name still gets the wildcard rule.
	if r.Allowed("/events", "someone-else") {
		t.Fatal("an unnamed agent ignored the wildcard Disallow")
	}
}

func TestRobotsPatterns(t *testing.T) {
	r, err := ParseRobots(strings.NewReader("User-agent: *\nDisallow: /*.json$\nDisallow: /tmp*\nDisallow: /cal/*/ics\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cases := map[string]bool{
		"/data/feed.json":  false,
		"/data/feed.jsonx": true,
		"/tmpfile":         false,
		"/tmp/a/b":         false,
		"/cal/2026/ics":    false,
		"/cal/2026/icsx":   false, // a rule is a prefix match; only `$` ends it
		"/calendar":        true,
	}
	for path, want := range cases {
		if got := r.Allowed(path, "lineup"); got != want {
			t.Errorf("Allowed(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestRobotsAnchoredRule(t *testing.T) {
	r, err := ParseRobots(strings.NewReader("User-agent: *\nDisallow: /cal/*/ics$\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if r.Allowed("/cal/2026/ics", "lineup") {
		t.Fatal("an anchored rule did not match its exact path")
	}
	if !r.Allowed("/cal/2026/icsx", "lineup") {
		t.Fatal("an anchored rule matched a longer path")
	}
}

func TestRobotsCrawlDelayAndEmptyRules(t *testing.T) {
	r, err := ParseRobots(strings.NewReader("User-agent: *\nCrawl-delay: 1.5\nDisallow:\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := r.CrawlDelay("/", "lineup"); got != 1500*time.Millisecond {
		t.Fatalf("CrawlDelay = %v", got)
	}
	// "Disallow:" with no path is not a rule: it means everything is allowed.
	if !r.Allowed("/anything", "lineup") {
		t.Fatal("an empty Disallow was treated as a rule")
	}
}

func TestRobotsEmptyFileAllows(t *testing.T) {
	r, err := ParseRobots(strings.NewReader(""))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !r.Allowed("/anything", "lineup") {
		t.Fatal("an empty robots.txt disallowed something")
	}
}

func TestMatchPattern(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"", "/x", true},
		{"/x", "/x/y", true},
		{"/x", "/y", false},
		{"/*.json$", "/a/b.json", true},
		{"/*.json$", "/a/b.json?x=1", false},
		{"/a*c", "/abc", true},
		{"/a*c", "/abd", false},
		{"/a*b*c", "/aXbYc", true},
		{"/a*b*c", "/aXc", false},
	}
	for _, c := range cases {
		if got := matchPattern(c.pattern, c.path); got != c.want {
			t.Errorf("matchPattern(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestProfileLanguage(t *testing.T) {
	p := ChromeProfile().WithLanguage("sv")
	if !strings.HasPrefix(p.AcceptLanguage, "sv,sv;q=0.9") {
		t.Fatalf("Accept-Language = %q", p.AcceptLanguage)
	}
	if got := ChromeProfile().WithLanguage(""); got.AcceptLanguage != ChromeProfile().AcceptLanguage {
		t.Fatalf("an empty language changed the profile: %q", got.AcceptLanguage)
	}
}
