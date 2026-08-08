package truenas

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// A job-starting method returns a job id immediately. Blocking a tool call for
// the duration of a scrub or an image pull would make it unusable.
func TestCallJobReturnsIdentityWithoutWaiting(t *testing.T) {
	f := newFakeMiddleware(t)
	f.handle("app.pull_images", func(json.RawMessage) (any, *rpcError) {
		return 4242, nil
	})

	c := dial(t, f)
	id, err := c.CallJob(context.Background(), "app.pull_images", "signaldb")
	if err != nil {
		t.Fatalf("CallJob: %v", err)
	}
	if id != 4242 {
		t.Fatalf("job id = %d, want 4242", id)
	}
}

// Some middleware methods answer with the id wrapped in an array.
func TestCallJobAcceptsArrayWrappedID(t *testing.T) {
	f := newFakeMiddleware(t)
	f.handle("app.redeploy", func(json.RawMessage) (any, *rpcError) {
		return []int{77}, nil
	})

	c := dial(t, f)
	id, err := c.CallJob(context.Background(), "app.redeploy", "signaldb")
	if err != nil {
		t.Fatalf("CallJob: %v", err)
	}
	if id != 77 {
		t.Fatalf("job id = %d, want 77", id)
	}
}

func TestCallJobRejectedByTarget(t *testing.T) {
	f := newFakeMiddleware(t)
	f.handle("app.upgrade", func(json.RawMessage) (any, *rpcError) {
		return nil, &rpcError{Code: -32602, Message: "app not found"}
	})

	c := dial(t, f)
	if _, err := c.CallJob(context.Background(), "app.upgrade", "nope"); err == nil {
		t.Fatal("a rejected job must return an error")
	}
}

func TestJobStatusRunning(t *testing.T) {
	f := newFakeMiddleware(t)
	f.handle("core.get_jobs", func(json.RawMessage) (any, *rpcError) {
		return []map[string]any{{
			"id": 1, "state": "RUNNING", "method": "app.pull_images",
			"progress": map[string]any{"percent": 40.0, "description": "pulling"},
		}}, nil
	})

	c := dial(t, f)
	job, err := c.Job(context.Background(), 1)
	if err != nil {
		t.Fatalf("Job: %v", err)
	}
	if job.State != "RUNNING" {
		t.Errorf("state = %q", job.State)
	}
	if job.Percent != 40 {
		t.Errorf("percent = %v", job.Percent)
	}
	if job.Done() {
		t.Error("a running job is not done")
	}
}

func TestJobStatusFailed(t *testing.T) {
	f := newFakeMiddleware(t)
	f.handle("core.get_jobs", func(json.RawMessage) (any, *rpcError) {
		return []map[string]any{{
			"id": 2, "state": "FAILED", "error": "image not found",
		}}, nil
	})

	c := dial(t, f)
	job, err := c.Job(context.Background(), 2)
	if err != nil {
		t.Fatalf("Job: %v", err)
	}
	if !job.Done() {
		t.Error("a failed job is done")
	}
	if job.Failed() != true {
		t.Error("Failed() should report true")
	}
	if !strings.Contains(job.Error, "image not found") {
		t.Errorf("error = %q", job.Error)
	}
}

// An unknown id must be distinguishable from a job that ran and failed.
func TestUnknownJobIsDistinct(t *testing.T) {
	f := newFakeMiddleware(t)
	f.handle("core.get_jobs", func(json.RawMessage) (any, *rpcError) {
		return []map[string]any{}, nil
	})

	c := dial(t, f)
	_, err := c.Job(context.Background(), 999)
	if !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("want ErrJobNotFound, got %v", err)
	}
}

func TestRecentJobsListed(t *testing.T) {
	f := newFakeMiddleware(t)
	f.handle("core.get_jobs", func(json.RawMessage) (any, *rpcError) {
		return []map[string]any{
			{"id": 1, "state": "SUCCESS", "method": "app.start"},
			{"id": 2, "state": "RUNNING", "method": "app.pull_images"},
		}, nil
	})

	c := dial(t, f)
	jobs, err := c.RecentJobs(context.Background(), 10)
	if err != nil {
		t.Fatalf("RecentJobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("got %d jobs", len(jobs))
	}
}
