package server

import (
	"context"
	"testing"
	"time"
)

func TestCollectLogEntriesStopsAtRequestedCount(t *testing.T) {
	entries := make(chan logEntry, 3)
	entries <- logEntry{Timestamp: "2026-08-09T10:00:00", Text: "first"}
	entries <- logEntry{Timestamp: "2026-08-09T10:00:01", Text: "second"}
	entries <- logEntry{Timestamp: "2026-08-09T10:00:02", Text: "third"}

	result := collectLogEntries(context.Background(), entries, logCollectionOptions{
		Count:     2,
		ByteLimit: 100,
	})

	if result.Bound != logCollectionBoundCount {
		t.Errorf("bound = %q, want count", result.Bound)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("collected %d entries, want 2", len(result.Entries))
	}
	if result.Entries[1].Text != "second" {
		t.Errorf("second entry = %q, want second", result.Entries[1].Text)
	}
}

func TestCollectLogEntriesStopsAtByteBound(t *testing.T) {
	entries := make(chan logEntry, 2)
	entries <- logEntry{Timestamp: "2026-08-09T10:00:00", Text: "one"}
	entries <- logEntry{Timestamp: "2026-08-09T10:00:01", Text: "two"}

	result := collectLogEntries(context.Background(), entries, logCollectionOptions{
		Count:     10,
		ByteLimit: 3,
	})

	if result.Bound != logCollectionBoundBytes {
		t.Errorf("bound = %q, want bytes", result.Bound)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("collected %d entries, want 1", len(result.Entries))
	}
}

func TestCollectLogEntriesStopsAfterQuietInterval(t *testing.T) {
	clock := newManualLogCollectorClock()
	entries := make(chan logEntry, 1)
	entries <- logEntry{Timestamp: "2026-08-09T10:00:00", Text: "first"}
	resultCh := make(chan logCollectionResult, 1)

	go func() {
		resultCh <- collectLogEntries(context.Background(), entries, logCollectionOptions{
			Count:         10,
			ByteLimit:     100,
			QuietInterval: time.Second,
			Clock:         clock,
		})
	}()

	clock.fire(t, time.Second)
	result := <-resultCh

	if result.Bound != logCollectionBoundQuiet {
		t.Errorf("bound = %q, want quiet", result.Bound)
	}
	if len(result.Entries) != 1 {
		t.Errorf("collected %d entries, want 1", len(result.Entries))
	}
}

// The quiet interval must not arm until the first entry arrives -- otherwise
// ordinary subscribe latency (the target hasn't replayed the backlog yet)
// looks identical to "the stream is empty" and every tail comes back empty.
func TestCollectLogEntriesAwaitsFirstEntryPastQuietInterval(t *testing.T) {
	clock := newManualLogCollectorClock()
	entries := make(chan logEntry, 1)
	resultCh := make(chan logCollectionResult, 1)

	go func() {
		resultCh <- collectLogEntries(context.Background(), entries, logCollectionOptions{
			Count:         10,
			ByteLimit:     100,
			QuietInterval: 100 * time.Millisecond,
			Deadline:      time.Minute,
			Clock:         clock,
		})
	}()

	// Only the deadline timer should be outstanding until an entry arrives --
	// the quiet timer arming here (before any entry) is the bug under test.
	deadlineTimer := clock.take(t)
	if deadlineTimer.duration != time.Minute {
		t.Fatalf("first timer requested = %v, want the deadline (%v); quiet must not arm before the first entry", deadlineTimer.duration, time.Minute)
	}

	select {
	case timer := <-clock.requests:
		t.Fatalf("quiet timer armed before any entry arrived: duration %v", timer.duration)
	case <-time.After(50 * time.Millisecond):
	}

	entries <- logEntry{Timestamp: "2026-08-09T10:00:00", Text: "late but real"}

	quietTimer := clock.take(t)
	if quietTimer.duration != 100*time.Millisecond {
		t.Fatalf("timer requested after first entry = %v, want the quiet interval (%v)", quietTimer.duration, 100*time.Millisecond)
	}
	quietTimer.ch <- time.Time{}

	result := <-resultCh

	if result.Bound != logCollectionBoundQuiet {
		t.Errorf("bound = %q, want quiet", result.Bound)
	}
	if len(result.Entries) != 1 || result.Entries[0].Text != "late but real" {
		t.Errorf("entries = %+v, want one entry with text %q", result.Entries, "late but real")
	}
}

func TestCollectLogEntriesStopsAtDeadline(t *testing.T) {
	clock := newManualLogCollectorClock()
	entries := make(chan logEntry)
	resultCh := make(chan logCollectionResult, 1)

	go func() {
		resultCh <- collectLogEntries(context.Background(), entries, logCollectionOptions{
			Count:     10,
			ByteLimit: 100,
			Deadline:  time.Minute,
			Clock:     clock,
		})
	}()

	clock.fire(t, time.Minute)
	result := <-resultCh

	if result.Bound != logCollectionBoundDeadline {
		t.Errorf("bound = %q, want deadline", result.Bound)
	}
}

func TestCollectLogEntriesWithholdsEntryLargerThanByteBound(t *testing.T) {
	entries := make(chan logEntry, 1)
	entries <- logEntry{Timestamp: "2026-08-09T10:00:00", Text: "too large"}

	result := collectLogEntries(context.Background(), entries, logCollectionOptions{
		Count:     1,
		ByteLimit: 3,
	})

	if result.Bound != logCollectionBoundBytes {
		t.Errorf("bound = %q, want bytes", result.Bound)
	}
	if len(result.Entries) != 0 {
		t.Errorf("collected %d entries, want none", len(result.Entries))
	}
	if !result.ContentWithheld {
		t.Error("ContentWithheld = false, want true for an entry that exceeds the whole byte bound")
	}
}

type manualLogCollectorClock struct {
	requests chan manualLogCollectorTimer
}

type manualLogCollectorTimer struct {
	duration time.Duration
	ch       chan time.Time
}

func newManualLogCollectorClock() *manualLogCollectorClock {
	return &manualLogCollectorClock{requests: make(chan manualLogCollectorTimer, 2)}
}

func (c *manualLogCollectorClock) After(duration time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	c.requests <- manualLogCollectorTimer{duration: duration, ch: ch}
	return ch
}

func (c *manualLogCollectorClock) fire(t *testing.T, duration time.Duration) {
	t.Helper()
	timer := c.take(t)
	if timer.duration != duration {
		t.Fatalf("timer duration = %v, want %v", timer.duration, duration)
	}
	timer.ch <- time.Time{}
}

// take returns the next requested timer without firing it, so a test can
// assert on which duration was requested before deciding whether and when to
// fire it.
func (c *manualLogCollectorClock) take(t *testing.T) manualLogCollectorTimer {
	t.Helper()
	select {
	case timer := <-c.requests:
		return timer
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for a clock.After call")
		return manualLogCollectorTimer{}
	}
}
