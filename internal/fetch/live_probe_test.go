//go:build livesmoke

// Probes that answer "what can this egress actually reach, and in what form". They are the measurement
// the coverage record is written from, and the reason a source is never recorded as blocked on
// User-Agent-only evidence.
package fetch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestLiveReachability asks the real hosts, with the real profile, and prints what came back.
//
// It is the measurement the coverage record is written from, and the only honest way to answer "is this
// source blocked from here". A 403 with a WAF page and a TLS reset are different facts: the first says the
// software is known to the edge, the second says the connection never became a request.
func TestLiveReachability(t *testing.T) {
	c := New(Options{Interval: 2 * time.Second, Timeout: 45 * time.Second})
	targets := []struct{ name, url string }{
		{"eventim-detail", "https://www.eventim.de/event/teuterekordz-hol-dir-n-gelben-tour-2027-kulturzentrum-faust-22410950/"},
		{"eventim-home", "https://www.eventim.de/"},
		{"eventim-sitemap-index", "https://www.eventim.de/staticsite/sitemap/EVE/sitemapindex_de.xml"},
		{"axs-detail", "https://www.axs.com/uk/events/1328707/london-symphony-orchestra-tickets"},
		{"axs-sitemap", "https://www.static.discovery-prod.axs.com/uploads/sitemaps/sitemap_event_uk.xml"},
		{"billetlugen-home", "https://www.billetlugen.dk/"},
	}
	for _, target := range targets {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		resp, err := c.Get(ctx, target.url)
		cancel()
		switch {
		case err == nil:
			t.Logf("%-22s %d  %7d bytes  %s", target.name, resp.Status, len(resp.Body), resp.Header.Get("Content-Type"))
		case errors.Is(err, ErrDisallowed):
			t.Logf("%-22s ROBOTS %v", target.name, err)
		case StatusOf(err) > 0:
			t.Logf("%-22s %d  %7d bytes (refused)", target.name, StatusOf(err), len(resp.Body))
		default:
			t.Logf("%-22s ERROR %v", target.name, err)
		}
	}
}

// TestLiveEventimDetail asks five different Eventim event pages, so "403 on one page" can be told apart
// from "403 on every event page" — the first is a dead link, the second is a block.
func TestLiveEventimDetail(t *testing.T) {
	urls := []string{
		"https://www.eventim.de/event/disneys-der-koenig-der-loewen-das-musical-in-hamburg-stage-theater-im-hafen-hamburg-22421404/",
		"https://www.eventim.de/event/echte-maenner-weyher-theater-weyher-theater-21637127/",
		"https://www.eventim.de/event/30-years-euphonic-weekender-kuehlhaus-goerlitz-22409903/",
		"https://www.eventim.de/event/hilfe-weihnachten-geloescht-weihnachtsgeschichte-fuer-kinder-carlowitz-congresscenter-carlowitz-saal-21518214/",
		"https://www.eventim.de/event/highlightfuehrung-in-der-semperoper-semperoper-dresden-22393305/",
	}
	c := New(Options{Interval: 2 * time.Second, Timeout: 45 * time.Second})
	for _, u := range urls {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		resp, err := c.Get(ctx, u)
		cancel()
		if err != nil {
			body := ""
			if resp != nil {
				body = firstLine(string(resp.Body))
			}
			t.Logf("%d %6d b  %s | %s", StatusOf(err), bodyLen(resp), body, tail(u))
			continue
		}
		t.Logf("%d %6d b  MusicEvent=%v | %s", resp.Status, len(resp.Body),
			strings.Contains(string(resp.Body), "MusicEvent"), tail(u))
	}
}

func bodyLen(r *Response) int {
	if r == nil {
		return 0
	}
	return len(r.Body)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i > 0 {
		s = s[:i]
	}
	if len(s) > 90 {
		s = s[:90]
	}
	return strings.Join(strings.Fields(s), " ")
}

func tail(u string) string {
	if i := strings.LastIndex(u, "/event/"); i >= 0 {
		u = u[i+len("/event/"):]
	}
	if len(u) > 60 {
		u = u[:60]
	}
	return u
}

// TestLivePlatformSurvey is a one-question survey of keyless platforms that might cover Berlin or London:
// does this egress get their HTML at all, and does that HTML carry structured event data or event links?
func TestLivePlatformSurvey(t *testing.T) {
	c := New(Options{Interval: 3 * time.Second, Timeout: 45 * time.Second})
	targets := []struct{ name, url string }{
		{"eventfrog-berlin", "https://www.eventfrog.de/berlin"},
		{"billetto-london", "https://billetto.co.uk/en/cities/london"},
		{"tickster-events", "https://www.tickster.com/sv/evenemang"},
		{"ra-london", "https://ra.co/events/uk/london"},
		{"dice-london", "https://dice.fm/browse/london"},
	}
	for _, target := range targets {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		resp, err := c.Get(ctx, target.url)
		cancel()
		status := 0
		body := ""
		if resp != nil {
			status = resp.Status
			body = string(resp.Body)
		}
		if err != nil && status == 0 {
			t.Logf("%-18s ERROR %v", target.name, err)
			continue
		}
		ld := strings.Count(body, "application/ld+json")
		music := strings.Contains(body, `"MusicEvent"`) || strings.Contains(body, `"Event"`)
		links := strings.Count(body, "/event")
		t.Logf("%-18s %d %7d b  ld+json=%d  event-json=%v  /event-links=%d  %s",
			target.name, status, len(body), ld, music, links, orError(err))
	}
}

func orError(err error) string {
	if err == nil {
		return ""
	}
	return "(" + err.Error() + ")"
}
