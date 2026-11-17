// Package fetch is the only way this program talks to a source.
//
// Three things are not negotiable here, and all three were learned the hard way (see the project skill):
// a request carries the full browser header profile, because UA-only requests are killed mid-stream by
// two of the platforms this product depends on; compression is requested explicitly and decoded
// explicitly, because a 200 carrying gzip read as binary parses to "0 events"; and robots.txt is honoured
// per host and cached, because a sitemap is an invitation, not permission.
package fetch

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Defaults. The interval is one request per host per half second: a sitemap sweep is a few hundred
// requests and this keeps it a polite guest. The robots policy is refetched weekly, which is what the
// source policy assumes.
const (
	DefaultTimeout   = 30 * time.Second
	DefaultInterval  = 500 * time.Millisecond
	DefaultRobotsTTL = 7 * 24 * time.Hour
	DefaultMaxBody   = 64 << 20
	DefaultToken     = "lineup"
)

// Options configures a Client. Zero values take the defaults above; the clock and the sleep are
// injectable so the limiter and the cache can be asserted instead of waited out.
type Options struct {
	Profile   Profile
	Timeout   time.Duration
	Interval  time.Duration
	RobotsTTL time.Duration
	MaxBody   int64
	Token     string
	Now       func() time.Time
	Sleep     func(time.Duration)
	Transport http.RoundTripper
}

// Client fetches URLs politely. It is safe for concurrent use.
type Client struct {
	opts    Options
	http    *http.Client
	limiter *Limiter
	robots  *RobotsCache
}

// Response is what came back. Body is already decompressed.
type Response struct {
	URL    string
	Status int
	Header http.Header
	Body   []byte
}

// StatusError is returned for any HTTP status of 400 or above, with the response still attached: a
// sitemap walk ends at a 404 and needs to see it.
type StatusError struct {
	URL    string
	Status int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s answered %d", e.URL, e.Status)
}

// StatusOf returns the HTTP status behind err, or 0.
func StatusOf(err error) int {
	var se *StatusError
	if errors.As(err, &se) {
		return se.Status
	}
	return 0
}

// New returns a client with the defaults filled in.
func New(opts Options) *Client {
	if opts.Profile.UserAgent == "" {
		opts.Profile = ChromeProfile()
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.Interval == 0 {
		opts.Interval = DefaultInterval
	}
	if opts.RobotsTTL <= 0 {
		opts.RobotsTTL = DefaultRobotsTTL
	}
	if opts.MaxBody <= 0 {
		opts.MaxBody = DefaultMaxBody
	}
	if opts.Token == "" {
		opts.Token = DefaultToken
	}
	transport := opts.Transport
	if transport == nil {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.DisableCompression = true // we ask for gzip ourselves and decode it ourselves
		t.ForceAttemptHTTP2 = true
		transport = t
	}
	c := &Client{
		opts:    opts,
		http:    &http.Client{Timeout: opts.Timeout, Transport: transport},
		limiter: NewLimiter(opts.Interval, opts.Now, opts.Sleep),
	}
	c.robots = NewRobotsCache(opts.RobotsTTL, opts.Token, opts.Now, c.readRobots)
	return c
}

// Allow answers whether a URL may be fetched, without fetching it. A source checks this before it
// schedules a sweep, so the coverage line can say "off: robots" instead of failing row by row.
func (c *Client) Allow(ctx context.Context, rawURL string) error {
	u, err := parseTarget(rawURL)
	if err != nil {
		return err
	}
	return c.robots.Allow(ctx, u)
}

// Get performs a robots-checked, rate-limited fetch with the browser header profile.
func (c *Client) Get(ctx context.Context, rawURL string) (*Response, error) {
	return c.get(ctx, rawURL, false)
}

func (c *Client) get(ctx context.Context, rawURL string, skipRobots bool) (*Response, error) {
	u, err := parseTarget(rawURL)
	if err != nil {
		return nil, err
	}
	if !skipRobots {
		if err := c.robots.Allow(ctx, u); err != nil {
			return nil, err
		}
	}
	c.limiter.Wait(u.Host)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("fetch: %s: %w", rawURL, err)
	}
	req.Header = c.opts.Profile.headers()

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, c.opts.MaxBody))
	if err != nil {
		return nil, fmt.Errorf("fetch: %s: reading body: %w", rawURL, err)
	}
	body, err = decodeBody(body, resp.Header.Get("Content-Encoding"), c.opts.MaxBody)
	if err != nil {
		return nil, fmt.Errorf("fetch: %s: %w", rawURL, err)
	}

	out := &Response{URL: u.String(), Status: resp.StatusCode, Header: resp.Header, Body: body}
	if resp.StatusCode >= 400 {
		return out, &StatusError{URL: u.String(), Status: resp.StatusCode}
	}
	return out, nil
}

// readRobots fetches one host's policy. It deliberately bypasses the robots check — a policy fetch that
// asked itself for permission would deadlock — and the rate limiter, because it is one request per host
// per week.
func (c *Client) readRobots(ctx context.Context, origin string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/robots.txt", nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header = c.opts.Profile.headers()
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	body, err = decodeBody(body, resp.Header.Get("Content-Encoding"), 1<<20)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

// decodeBody gunzips what was asked for. Anything else is passed through: a source that answers with an
// encoding we did not request is a bug worth seeing in the parser rather than silently mangling.
func decodeBody(body []byte, encoding string, max int64) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", "identity":
		return body, nil
	case "gzip", "x-gzip":
		zr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("body claims gzip but is not: %w", err)
		}
		defer zr.Close()
		out, err := io.ReadAll(io.LimitReader(zr, max))
		if err != nil {
			return nil, fmt.Errorf("gunzip: %w", err)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported content-encoding %q", encoding)
	}
}

func parseTarget(rawURL string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("fetch: %q is not a URL: %w", rawURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("fetch: %q is not http(s)", rawURL)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("fetch: %q has no host", rawURL)
	}
	return u, nil
}
