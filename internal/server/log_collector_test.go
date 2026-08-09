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
	entries := make(chan logEntry)
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
	if len(result.Entries) != 0 {
		t.Errorf("collected %d entries, want none", len(result.Entries))
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
	timer := <-c.requests
	if timer.duration != duration {
		t.Fatalf("timer duration = %v, want %v", timer.duration, duration)
	}
	timer.ch <- time.Time{}
}
