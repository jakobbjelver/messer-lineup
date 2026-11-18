package state

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newState(t *testing.T) *State {
	t.Helper()
	opened, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return opened
}

func TestTheSameTargetIsQueuedOnce(t *testing.T) {
	s := newState(t)
	now := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	job := Job{Source: "tickster", Kind: JobDetail, Target: "https://t.test/a", Town: "copenhagen-dk", NotBefore: now}

	if !s.EnqueueJob(job) {
		t.Fatal("first enqueue was refused")
	}
	if s.EnqueueJob(job) {
		t.Fatal("the same target was queued twice")
	}
	if _, _, total := s.JobStats(now); total != 1 {
		t.Fatalf("total = %d, want 1", total)
	}
	if job.ID != "" && JobID("tickster", JobDetail, "https://t.test/a") == "" {
		t.Fatal("JobID is empty")
	}
}

func TestDueJobsAreOrderedAndRespectTheBackoff(t *testing.T) {
	s := newState(t)
	now := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	s.EnqueueJob(Job{Source: "tickster", Target: "late", Town: "copenhagen-dk", NotBefore: now.Add(time.Hour)})
	s.EnqueueJob(Job{Source: "tickster", Target: "early", Town: "copenhagen-dk", NotBefore: now.Add(-time.Minute)})
	s.EnqueueJob(Job{Source: "tickster", Target: "also-early", Town: "stockholm-se", NotBefore: now.Add(-5 * time.Minute)})

	due := s.DueJobs(now, 10)
	if len(due) != 2 {
		t.Fatalf("due = %d, want 2 (the future job is not due)", len(due))
	}
	if due[0].Target != "also-early" || due[1].Target != "early" {
		t.Fatalf("due order = %q, %q", due[0].Target, due[1].Target)
	}
	if limited := s.DueJobs(now, 1); len(limited) != 1 {
		t.Fatalf("limit was not applied: %d", len(limited))
	}
	if d, w, total := s.JobStats(now); d != 2 || w != 1 || total != 3 {
		t.Fatalf("stats = %d due, %d waiting, %d total", d, w, total)
	}
}

func TestAFailureBacksOffAndThenGivesUp(t *testing.T) {
	s := newState(t)
	now := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	target := "https://t.test/flaky"
	id := JobID("tickster", JobDetail, target)
	s.EnqueueJob(Job{Source: "tickster", Target: target, Town: "copenhagen-dk", NotBefore: now})

	var delays []time.Duration
	for attempt := 1; attempt <= MaxJobAttempts; attempt++ {
		due := s.DueJobs(now, 10)
		if len(due) != 1 {
			t.Fatalf("attempt %d: due = %d, want 1", attempt, len(due))
		}
		givenUp := s.FailJob(id, errors.New("answered 429"), now)
		if attempt < MaxJobAttempts {
			if givenUp {
				t.Fatalf("attempt %d gave up early", attempt)
			}
			next := s.DueJobs(now, 10)
			if len(next) != 0 {
				t.Fatalf("attempt %d: the job is still due immediately", attempt)
			}
			after := s.DueJobs(now.Add(24*time.Hour), 10)
			if len(after) != 1 {
				t.Fatalf("attempt %d: the job never became due again", attempt)
			}
			delays = append(delays, after[0].NotBefore.Sub(now))
			now = after[0].NotBefore
			continue
		}
		if !givenUp {
			t.Fatalf("attempt %d did not give up", attempt)
		}
	}
	if _, _, total := s.JobStats(now); total != 0 {
		t.Fatalf("the queue still holds %d jobs after giving up", total)
	}
	want := []time.Duration{30 * time.Minute, time.Hour, 2 * time.Hour, 4 * time.Hour}
	for i := range want {
		if delays[i] != want[i] {
			t.Fatalf("backoff %d = %s, want %s (all: %v)", i+1, delays[i], want[i], delays)
		}
	}
}

func TestSuccessRemovesTheJob(t *testing.T) {
	s := newState(t)
	now := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	target := "https://t.test/later"
	id := JobID("tickster", JobDetail, target)
	s.EnqueueJob(Job{Source: "tickster", Target: target, Town: "copenhagen-dk", NotBefore: now})

	if !s.SuccessJob(id) {
		t.Fatal("success did not remove the job")
	}
	if s.SuccessJob(id) {
		t.Fatal("a second success claimed the job again")
	}
	if _, _, total := s.JobStats(now); total != 0 {
		t.Fatalf("total = %d after success", total)
	}
}

func TestTheQueueSurvivesAReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	now := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	first, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	first.EnqueueJob(Job{Source: "tickster", Target: "https://t.test/x", Town: "copenhagen-dk", NotBefore: now})

	second, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	due := second.DueJobs(now, 10)
	if len(due) != 1 || due[0].Target != "https://t.test/x" {
		t.Fatalf("the queue did not survive the file: %+v", due)
	}
}
