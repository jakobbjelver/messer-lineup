package state

// The job queue: what the miner will try again.
//
// A sweep fetches one detail page per candidate, and a few of those fail for reasons that say nothing about the show —
// a 429, a timeout, a gateway hiccup. Counting those as failures and moving on means a show is simply missing from a
// published window until someone happens to sweep the town again. The queue keeps those targets and retries them with
// backoff, at the start of the next run for that town.
//
// Three decisions worth stating:
//
//   - **Only transient failures are queued.** A 404 is a fact (the show was taken down), a 403 is a fact (we are
//     blocked), a page with no event block is a fact (the platform changed or it never was a show). Retrying a fact
//     spends the rate budget to learn it again.
//   - **The queue lives in the state file, not in SQLite.** Plan §1.2 said SQLite, and this package's own header
//     reserved it for "when claims actually need transactions". Nothing here needs a transaction: one process mines at
//     a time, the queue is a handful of entries, and a crash mid-run costs at most a repeated fetch. A database
//     dependency would buy contention safety nobody is contending for, so the plan's shape is kept and the storage is
//     the honest one.
//   - **The id is `source|kind|target`.** The same failing URL cannot be queued twice, so a run that fails the same
//     page on three towns still leaves one job.

import (
	"sort"
	"time"
)

// JobDetail is the only kind the miner queues today: one event page it could not read.
const JobDetail = "detail"

const (
	// MaxJobAttempts is where a job stops being retried and becomes a counted loss. Five attempts over a day and a
	// half is enough for a bad minute to clear without keeping a dead target forever.
	MaxJobAttempts = 5
	// GiveUpTTL is how long a target that ran out of attempts stays out of the queue. Without it the next sweep
	// re-queues the same dead page immediately — the target is also an ordinary candidate, so "five attempts" would
	// mean "five attempts, then five more, forever".
	GiveUpTTL = 7 * 24 * time.Hour
	// jobBackoffBase is the first gap: half an hour, doubling, capped at six.
	jobBackoffBase = 30 * time.Minute
	jobBackoffCap  = 6 * time.Hour
)

// FirstRetry is how long a caller should wait before the first attempt. Exported so the miner schedules with the same
// number the backoff starts from, instead of inventing a second one.
const FirstRetry = jobBackoffBase

// Job is one target to try again.
type Job struct {
	ID        string    `json:"id"` // source|kind|target
	Source    string    `json:"source"`
	Kind      string    `json:"kind"`
	Target    string    `json:"target"` // the detail page to fetch again
	Town      string    `json:"town"`   // the town whose run will retry it
	NotBefore time.Time `json:"not_before"`
	Attempts  int       `json:"attempts"`
	LastError string    `json:"last_error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// JobID is the identity of a queued target.
func JobID(source, kind, target string) string { return source + "|" + kind + "|" + target }

// EnqueueJob adds a job, or returns false when that target is already queued (an id is never duplicated).
func (s *State) EnqueueJob(job Job) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The kind is defaulted *before* the id is derived from it, or a caller that omits it queues an id the caller
	// cannot look up again.
	if job.Kind == "" {
		job.Kind = JobDetail
	}
	if job.ID == "" {
		job.ID = JobID(job.Source, job.Kind, job.Target)
	}
	for _, existing := range s.data.Jobs {
		if existing.ID == job.ID {
			return false
		}
	}
	if until, dead := s.data.GaveUp[job.ID]; dead {
		if job.NotBefore.Before(until) {
			return false
		}
		delete(s.data.GaveUp, job.ID) // the tombstone has expired: the target may be tried again
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = job.NotBefore
	}
	s.data.Jobs = append(s.data.Jobs, job)
	_ = s.save()
	return true
}

// DueJobs returns the jobs whose backoff has elapsed, oldest first, up to limit. It does not remove them: a run that
// dies mid-sweep must not lose the work, and a claimed-and-lost job is worse than a retried one.
func (s *State) DueJobs(now time.Time, limit int) []Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	due := make([]Job, 0, len(s.data.Jobs))
	for _, job := range s.data.Jobs {
		if !job.NotBefore.After(now) {
			due = append(due, job)
		}
	}
	sort.SliceStable(due, func(i, j int) bool {
		if !due[i].NotBefore.Equal(due[j].NotBefore) {
			return due[i].NotBefore.Before(due[j].NotBefore)
		}
		return due[i].ID < due[j].ID
	})
	if limit > 0 && len(due) > limit {
		due = due[:limit]
	}
	return due
}

// SuccessJob removes a job by id and reports whether it was there.
func (s *State) SuccessJob(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, job := range s.data.Jobs {
		if job.ID != id {
			continue
		}
		s.data.Jobs = append(s.data.Jobs[:i], s.data.Jobs[i+1:]...)
		_ = s.save()
		return true
	}
	return false
}

// FailJob records another failed attempt: it counts the attempt, stores why, and either schedules the next try with
// backoff or gives up and removes the job (returning true, so the caller can count a real loss).
func (s *State) FailJob(id string, cause error, now time.Time) (givenUp bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, job := range s.data.Jobs {
		if job.ID != id {
			continue
		}
		job.Attempts++
		if cause != nil {
			job.LastError = cause.Error()
		}
		if job.Attempts >= MaxJobAttempts {
			s.data.Jobs = append(s.data.Jobs[:i], s.data.Jobs[i+1:]...)
			s.markGivenUp(job.ID, now)
			_ = s.save()
			return true
		}
		job.NotBefore = now.Add(backoffFor(job.Attempts))
		s.data.Jobs[i] = job
		_ = s.save()
		return false
	}
	return false
}

// JobStats is what a run reports: how many are due now, how many are waiting, and how many targets are queued at all.
func (s *State) JobStats(now time.Time) (due, waiting, total int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, job := range s.data.Jobs {
		total++
		if job.NotBefore.After(now) {
			waiting++
		} else {
			due++
		}
	}
	return due, waiting, total
}

// markGivenUp remembers a target that ran out of attempts, and prunes tombstones that have expired so the file does
// not grow a row per dead link forever.
func (s *State) markGivenUp(id string, now time.Time) {
	if s.data.GaveUp == nil {
		s.data.GaveUp = map[string]time.Time{}
	}
	for key, until := range s.data.GaveUp {
		if !now.Before(until) {
			delete(s.data.GaveUp, key)
		}
	}
	s.data.GaveUp[id] = now.Add(GiveUpTTL)
}

// backoffFor is exponential with a ceiling: 30m, 1h, 2h, 4h, then 6h. Attempt 1 means the first retry.
func backoffFor(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	delay := jobBackoffBase
	for i := 1; i < attempts; i++ {
		delay *= 2
		if delay >= jobBackoffCap {
			return jobBackoffCap
		}
	}
	return delay
}
