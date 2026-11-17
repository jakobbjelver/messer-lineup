package fetch

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrDisallowed is returned instead of making a request that robots.txt forbids. A crawler that asks
// anyway is the reason sources close sitemaps.
var ErrDisallowed = errors.New("fetch: disallowed by robots.txt")

// ErrRobotsUnavailable is returned when robots.txt could not be read (a 5xx or a transport error). The
// conservative reading of RFC 9309 is "do not crawl", and it is also the honest one: an unknown policy is
// not permission.
var ErrRobotsUnavailable = errors.New("fetch: robots.txt unavailable")

// Robots is one host's parsed policy.
type Robots struct {
	groups []robotsGroup
}

type robotsGroup struct {
	agents []string
	rules  []robotsRule
	delay  time.Duration
}

type robotsRule struct {
	allow   bool
	pattern string
}

// ParseRobots reads a robots.txt. A file with no groups allows everything, which is what an empty or
// minimal robots.txt means.
func ParseRobots(r io.Reader) (*Robots, error) {
	out := &Robots{}
	var current *robotsGroup
	inRules := false

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)

		switch key {
		case "user-agent":
			if current == nil || inRules {
				out.groups = append(out.groups, robotsGroup{})
				current = &out.groups[len(out.groups)-1]
				inRules = false
			}
			current.agents = append(current.agents, strings.ToLower(value))
		case "disallow", "allow":
			if current == nil {
				continue // a rule before any user-agent applies to nobody
			}
			inRules = true
			if value == "" && key == "disallow" {
				continue // "Disallow:" with no path allows everything; it is not a rule
			}
			current.rules = append(current.rules, robotsRule{allow: key == "allow", pattern: value})
		case "crawl-delay":
			if current == nil {
				continue
			}
			if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds > 0 {
				current.delay = time.Duration(seconds * float64(time.Second))
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Allowed reports whether path (with any query) may be fetched. Rules match by longest pattern, and a
// tie goes to Allow; `*` matches any run and a trailing `$` anchors the end.
func (r *Robots) Allowed(path, token string) bool {
	if r == nil {
		return true
	}
	best := robotsRule{allow: true, pattern: ""} // no rule at all means allowed
	for _, rule := range r.rules(path, token) {
		if !matchPattern(rule.pattern, path) {
			continue
		}
		switch {
		case len(rule.pattern) > len(best.pattern),
			len(rule.pattern) == len(best.pattern) && rule.allow && !best.allow:
			best = rule
		}
	}
	return best.allow
}

// CrawlDelay is the host's stated delay, or zero.
func (r *Robots) CrawlDelay(path, token string) time.Duration {
	if r == nil {
		return 0
	}
	var delay time.Duration
	for _, g := range r.groupsFor(path, token) {
		if g.delay > delay {
			delay = g.delay
		}
	}
	return delay
}

// rules returns every rule that applies to token: the groups naming it (case-insensitively) if any,
// otherwise the wildcard groups. Merging the wildcard group with a named one would apply rules nobody
// wrote for us.
func (r *Robots) rules(_ string, token string) []robotsRule {
	var out []robotsRule
	for _, g := range r.groupsFor("", token) {
		out = append(out, g.rules...)
	}
	return out
}

func (r *Robots) groupsFor(_ string, token string) []robotsGroup {
	token = strings.ToLower(token)
	var named, star []robotsGroup
	for _, g := range r.groups {
		for _, agent := range g.agents {
			if agent == "" {
				continue
			}
			if agent == token || (agent != "*" && strings.HasPrefix(token, agent)) {
				named = append(named, g)
				break
			}
			if agent == "*" {
				star = append(star, g)
				break
			}
		}
	}
	if len(named) > 0 {
		return named
	}
	return star
}

// matchPattern implements robots matching: `*` matches any run of characters, a trailing `$` anchors the
// end of the path, everything else is literal.
func matchPattern(pattern, path string) bool {
	if pattern == "" {
		return true
	}
	anchored := strings.HasSuffix(pattern, "$")
	if anchored {
		pattern = strings.TrimSuffix(pattern, "$")
	}
	if !strings.Contains(pattern, "*") {
		if anchored {
			return path == pattern
		}
		return strings.HasPrefix(path, pattern)
	}
	parts := strings.Split(pattern, "*")
	if !strings.HasPrefix(path, parts[0]) {
		return false
	}
	rest := path[len(parts[0]):]
	for i, part := range parts[1:] {
		last := i == len(parts)-2
		if part == "" {
			if last && anchored {
				return true
			}
			continue
		}
		if last && anchored {
			return strings.HasSuffix(rest, part)
		}
		idx := strings.Index(rest, part)
		if idx < 0 {
			return false
		}
		rest = rest[idx+len(part):]
	}
	return !anchored
}

// RobotsCache holds one policy per host, refetched after ttl. A policy that could not be read is cached
// for the same period: retrying a 503 host on every URL is how a crawler turns a bad minute into a ban.
type RobotsCache struct {
	ttl     time.Duration
	token   string
	now     func() time.Time
	fetch   func(ctx context.Context, origin string) (status int, body []byte, err error)
	mu      sync.Mutex
	entries map[string]robotsEntry
}

type robotsEntry struct {
	robots  *Robots
	err     error
	expires time.Time
}

// NewRobotsCache wires the cache to a fetcher that reads one host's /robots.txt.
func NewRobotsCache(ttl time.Duration, token string, now func() time.Time,
	fetch func(ctx context.Context, origin string) (int, []byte, error)) *RobotsCache {
	if now == nil {
		now = time.Now
	}
	return &RobotsCache{ttl: ttl, token: token, now: now, fetch: fetch, entries: map[string]robotsEntry{}}
}

// Allow returns nil when u may be fetched. It fetches and caches the host's policy on first use.
func (c *RobotsCache) Allow(ctx context.Context, u *url.URL) error {
	robots, err := c.policy(ctx, u)
	if err != nil {
		return err
	}
	if robots.Allowed(requestPath(u), c.token) {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrDisallowed, u.String())
}

func (c *RobotsCache) policy(ctx context.Context, u *url.URL) (*Robots, error) {
	key := u.Scheme + "://" + u.Host
	c.mu.Lock()
	entry, ok := c.entries[key]
	c.mu.Unlock()
	if ok && c.now().Before(entry.expires) {
		return entry.robots, entry.err
	}

	status, body, err := c.fetch(ctx, key)
	var robots *Robots
	switch {
	case err != nil:
		err = fmt.Errorf("%w: %s: %v", ErrRobotsUnavailable, key, err)
	case status >= 500:
		err = fmt.Errorf("%w: %s answered %d", ErrRobotsUnavailable, key, status)
	case status >= 400:
		// 4xx means "no policy published", which allows everything (RFC 9309 §2.3.1.3).
		robots = &Robots{}
	default:
		robots, err = ParseRobots(strings.NewReader(string(body)))
		if err != nil {
			err = fmt.Errorf("%w: %s: %v", ErrRobotsUnavailable, key, err)
		}
	}

	c.mu.Lock()
	c.entries[key] = robotsEntry{robots: robots, err: err, expires: c.now().Add(c.ttl)}
	c.mu.Unlock()
	return robots, err
}

// requestPath is what robots rules are matched against: path plus query, as published.
func requestPath(u *url.URL) string {
	if u.RawQuery == "" {
		return u.Path
	}
	return u.Path + "?" + u.RawQuery
}
