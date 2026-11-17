package fetch

import "net/http"

// Profile is the browser header set a source is asked with.
//
// This is not cosmetic. Eventim and Billetlugen complete the TLS handshake and then kill the stream in
// ~100 ms when a request carries only a User-Agent — which reads as "blocked from this network" and was
// recorded as a block for a week. The same URL with the headers below answers 200 and the full page. A
// source is therefore never recorded as blocked on User-Agent-only evidence.
type Profile struct {
	UserAgent       string
	AcceptLanguage  string
	Accept          string
	SecCHUA         string
	SecCHUAMobile   string
	SecCHUAPlatform string
}

// ChromeProfile is the default: a recent Chrome on macOS. A profile per platform is not needed yet, and
// inventing one per source would be a lie about who is asking.
func ChromeProfile() Profile {
	return Profile{
		UserAgent:       "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36",
		AcceptLanguage:  "en-GB,en;q=0.9",
		Accept:          "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
		SecCHUA:         `"Chromium";v="141", "Not?A_Brand";v="24", "Google Chrome";v="141"`,
		SecCHUAMobile:   "?0",
		SecCHUAPlatform: `"macOS"`,
	}
}

// WithLanguage returns a copy of the profile asking for one locale first. Eventim serves a differently
// priced, differently dated page per language, so the registry's `lang` is carried here rather than
// guessed.
func (p Profile) WithLanguage(tag string) Profile {
	if tag == "" {
		return p
	}
	p.AcceptLanguage = tag + "," + tag[:2] + ";q=0.9,en;q=0.7"
	return p
}

// headers is the whole profile as it goes on the wire, including Accept-Encoding: gzip. Compression is
// requested explicitly and decoded explicitly (see client.go): a client that sets the header and skips
// the gunzip parses binary, reports "0 events", and looks exactly like a blocked host.
func (p Profile) headers() http.Header {
	h := http.Header{}
	set := func(key, value string) {
		if value != "" {
			h.Set(key, value)
		}
	}
	set("User-Agent", p.UserAgent)
	set("Accept", p.Accept)
	set("Accept-Language", p.AcceptLanguage)
	set("Accept-Encoding", "gzip")
	set("sec-ch-ua", p.SecCHUA)
	set("sec-ch-ua-mobile", p.SecCHUAMobile)
	set("sec-ch-ua-platform", p.SecCHUAPlatform)
	set("Sec-Fetch-Dest", "document")
	set("Sec-Fetch-Mode", "navigate")
	set("Sec-Fetch-Site", "none")
	set("Sec-Fetch-User", "?1")
	set("Upgrade-Insecure-Requests", "1")
	return h
}
