package server

import (
	"context"
	"time"
)

// logEntry preserves the timestamp and text supplied by a log event source.
type logEntry struct {
	Timestamp string
	Text      string
}

type logCollectionBound string

const (
	logCollectionBoundCount    logCollectionBound = "count"
	logCollectionBoundBytes    logCollectionBound = "bytes"
	logCollectionBoundQuiet    logCollectionBound = "quiet"
	logCollectionBoundDeadline logCollectionBound = "deadline"
	logCollectionBoundCanceled logCollectionBound = "canceled"
)

// logCollectionResult describes the bounded portion of an event stream.
type logCollectionResult struct {
	Entries         []logEntry
	Bound           logCollectionBound
	ContentWithheld bool
}

type logCollectorClock interface {
	After(time.Duration) <-chan time.Time
}

type logCollectionOptions struct {
	Count         int
	ByteLimit     int
	QuietInterval time.Duration
	Deadline      time.Duration
	Clock         logCollectorClock
}

type realLogCollectorClock struct{}

func (realLogCollectorClock) After(duration time.Duration) <-chan time.Time {
	return time.After(duration)
}

// collectLogEntries takes a finite, bounded snapshot from an otherwise live
// event stream. Clock injection keeps collection independent of middleware and
// lets callers test time-based bounds without waiting on wall-clock time.
func collectLogEntries(ctx context.Context, entries <-chan logEntry, options logCollectionOptions) logCollectionResult {
	if options.Count <= 0 {
		return logCollectionResult{Bound: logCollectionBoundCount}
	}

	clock := options.Clock
	if clock == nil {
		clock = realLogCollectorClock{}
	}

	// The quiet interval distinguishes "the backlog is drained" from "the
	// container is idle" -- a distinction that only makes sense once
	// streaming has actually begun. Arming it here, before the first entry
	// arrives, would mistake ordinary subscribe latency (the target hasn't
	// replayed the backlog yet) for silence and end the collection empty.
	// The deadline alone bounds the wait for that first entry.
	var quiet <-chan time.Time
	var deadline <-chan time.Time
	if options.Deadline > 0 {
		deadline = clock.After(options.Deadline)
	}

	result := logCollectionResult{}
	bytes := 0
	for {
		select {
		case <-ctx.Done():
			result.Bound = logCollectionBoundCanceled
			return result
		case <-deadline:
			result.Bound = logCollectionBoundDeadline
			return result
		case <-quiet:
			result.Bound = logCollectionBoundQuiet
			return result
		case entry, ok := <-entries:
			if !ok {
				result.Bound = logCollectionBoundQuiet
				return result
			}

			entryBytes := len(entry.Text)
			if entryBytes > options.ByteLimit-bytes {
				result.Bound = logCollectionBoundBytes
				result.ContentWithheld = true
				return result
			}

			result.Entries = append(result.Entries, entry)
			bytes += entryBytes
			if len(result.Entries) == options.Count {
				result.Bound = logCollectionBoundCount
				return result
			}
			if bytes == options.ByteLimit {
				result.Bound = logCollectionBoundBytes
				return result
			}
			if options.QuietInterval > 0 {
				quiet = clock.After(options.QuietInterval)
			}
		}
	}
}
