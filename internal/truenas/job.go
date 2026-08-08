package truenas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrJobNotFound means the target does not recognise the job id. It is kept
// distinct from a failed job: "never existed" and "ran and failed" call for
// different responses.
var ErrJobNotFound = errors.New("job not found")

// Job is the state of a long-running middleware operation.
type Job struct {
	ID          int64   `json:"id"`
	Method      string  `json:"method,omitempty"`
	State       string  `json:"state"`
	Percent     float64 `json:"percent,omitempty"`
	Description string  `json:"description,omitempty"`
	Error       string  `json:"error,omitempty"`
	Result      any     `json:"result,omitempty" jsonschema:"the job's return value, present once it succeeds"`
}

// Done reports whether the job has reached a terminal state.
func (j Job) Done() bool {
	switch j.State {
	case "SUCCESS", "FAILED", "ABORTED":
		return true
	}
	return false
}

// Failed reports whether the job finished unsuccessfully.
func (j Job) Failed() bool {
	return j.State == "FAILED" || j.State == "ABORTED"
}

// CallJob invokes a job-typed method and returns its job id as soon as the
// target accepts it, without waiting for completion.
func (c *Client) CallJob(ctx context.Context, method string, params ...any) (int64, error) {
	raw, err := c.Call(ctx, method, params...)
	if err != nil {
		return 0, err
	}

	// Job methods answer with a bare id or, for some methods, an array
	// containing one.
	var id int64
	if err := json.Unmarshal(raw, &id); err == nil {
		return id, nil
	}

	var ids []int64
	if err := json.Unmarshal(raw, &ids); err == nil && len(ids) > 0 {
		return ids[0], nil
	}

	return 0, fmt.Errorf("%s: expected a job id, got %s", method, raw)
}

// rawJob mirrors the middleware's job representation, whose progress is nested
// and whose error field is absent on success.
type rawJob struct {
	ID       int64  `json:"id"`
	Method   string `json:"method"`
	State    string `json:"state"`
	Error    string `json:"error"`
	Result   any    `json:"result"`
	Progress struct {
		Percent     float64 `json:"percent"`
		Description string  `json:"description"`
	} `json:"progress"`
}

func (r rawJob) toJob() Job {
	return Job{
		ID:          r.ID,
		Method:      r.Method,
		State:       r.State,
		Percent:     r.Progress.Percent,
		Description: r.Progress.Description,
		Error:       r.Error,
		Result:      r.Result,
	}
}

// Job returns the current state of one job.
func (c *Client) Job(ctx context.Context, id int64) (Job, error) {
	filters := []any{[]any{"id", "=", id}}

	raw, err := c.Call(ctx, "core.get_jobs", filters)
	if err != nil {
		return Job{}, err
	}

	var jobs []rawJob
	if err := json.Unmarshal(raw, &jobs); err != nil {
		return Job{}, fmt.Errorf("decoding jobs: %w", err)
	}
	if len(jobs) == 0 {
		return Job{}, fmt.Errorf("%w: %d", ErrJobNotFound, id)
	}
	return jobs[0].toJob(), nil
}

// RecentJobs lists recent jobs so a caller can ask what the system has been
// doing without already knowing a job id.
func (c *Client) RecentJobs(ctx context.Context, limit int) ([]Job, error) {
	raw, err := c.Call(ctx, "core.get_jobs")
	if err != nil {
		return nil, err
	}

	var jobs []rawJob
	if err := json.Unmarshal(raw, &jobs); err != nil {
		return nil, fmt.Errorf("decoding jobs: %w", err)
	}

	// The middleware returns oldest-first; recent activity is what a caller
	// asked for.
	out := make([]Job, 0, len(jobs))
	for i := len(jobs) - 1; i >= 0; i-- {
		out = append(out, jobs[i].toJob())
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}
