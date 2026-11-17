package model

// Candidate is a show a source has nominated but not described.
//
// A sitemap gives a URL and nothing else — no date, no town — so every candidate costs a detail fetch.
// That cost is why mining is country-first and why a candidate carries no expectations beyond "this page
// might be an event".
type Candidate struct {
	URL    string // the detail page a source would fetch
	Source string // the adapter that nominated it, e.g. "dice"
}
