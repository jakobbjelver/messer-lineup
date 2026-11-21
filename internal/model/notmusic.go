package model

import "errors"

// ErrNotMusic is a detail that parsed into an event the platform itself labels as something other than a gig.
//
// It is a fact, not a failure: Nortic prefixes every description with a category (`[Konsert]`, `[Teater]`, `[Humor]`,
// `[Dans]`, ...), and a theatre night at a room that also programmes music is still not a concert. The miner counts
// these separately so a report can say "the category said no" rather than "the page did not parse" — and so the retry
// queue never spends a second attempt on a page that answered correctly.
var ErrNotMusic = errors.New("not music")
