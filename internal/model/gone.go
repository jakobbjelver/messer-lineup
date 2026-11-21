package model

import "errors"

// ErrGone is a URL a platform's sitemap still lists but no longer serves as an event: the page answers 200 with an
// organizer's listing instead of a date (Nortic does this for a removed event), or a redirect lands somewhere that is
// not an event.
//
// It is distinct from ErrNotMusic (the page answered, and the answer was "not a gig") and from a parse failure (the
// page changed shape). The miner counts it as a fact, never as a failure, so a report cannot read a stale link as a
// broken parser — and a retry queue cannot spend attempts on a page that is never coming back.
var ErrGone = errors.New("no longer listed")
