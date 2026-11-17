package fetch

import (
	"sync"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/clock"
)

// Limiter keeps the crawler polite: one request at a time to a host, with a minimum gap between them.
// It is per host, so a slow source cannot stall the sweep of another country.
//
// Both the clock and the sleep are injected. A test that had to wait six real seconds to assert a six
// second rule would not be run, and a rule that is not tested is a rule that drifts.
type Limiter struct {
	min   time.Duration
	now   func() time.Time
	sleep func(time.Duration)

	mu    sync.Mutex
	hosts map[string]*hostSlot
}

type hostSlot struct {
	mu   sync.Mutex
	next time.Time
}

// NewLimiter returns a limiter with a minimum gap of min between requests to the same host.
func NewLimiter(min time.Duration, now func() time.Time, sleep func(time.Duration)) *Limiter {
	if now == nil {
		now = clock.Now
	}
	if sleep == nil {
		sleep = time.Sleep
	}
	return &Limiter{min: min, now: now, sleep: sleep, hosts: map[string]*hostSlot{}}
}

// Wait blocks until a request to host is allowed, and books the next slot. Calls for the same host
// serialise, so concurrency cannot exceed the limit by racing.
func (l *Limiter) Wait(host string) {
	if l == nil || l.min <= 0 {
		return
	}
	slot := l.slot(host)
	slot.mu.Lock()
	defer slot.mu.Unlock()

	if wait := slot.next.Sub(l.now()); wait > 0 {
		l.sleep(wait)
	}
	slot.next = l.now().Add(l.min)
}

func (l *Limiter) slot(host string) *hostSlot {
	l.mu.Lock()
	defer l.mu.Unlock()
	slot, ok := l.hosts[host]
	if !ok {
		slot = &hostSlot{}
		l.hosts[host] = slot
	}
	return slot
}
