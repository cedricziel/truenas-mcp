package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/cedricziel/truenas-mcp/internal/truenas"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// JobsInput selects between listing recent jobs and showing one.
type JobsInput struct {
	Op    string `json:"op" jsonschema:"list to see recent jobs, or show to check one"`
	JobID int64  `json:"job_id,omitempty" jsonschema:"the job to show; required for show"`
	Limit int    `json:"limit,omitempty" jsonschema:"how many recent jobs to list"`
}

// JobsOutput reports job state. Polling this is always sufficient to follow a
// job to completion; resource subscription, where a client supports it, only
// makes it nicer.
type JobsOutput struct {
	Op   string        `json:"op"`
	Job  *truenas.Job  `json:"job,omitempty" jsonschema:"the requested job"`
	Jobs []truenas.Job `json:"jobs,omitempty" jsonschema:"recent jobs, most recent first"`
}

func registerJobs(srv *mcp.Server, session sessionFor) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "jobs",
		Description: "Follow long-running TrueNAS operations.\n\nOperations:\n" +
			"  list — recent jobs and their state\n" +
			"  show — one job's state, progress, and outcome (requires job_id)\n\n" +
			"Mutating tools return a job_id rather than waiting; use show to follow it.",
		Annotations: readAnnotations("Long-running operations"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in JobsInput) (*mcp.CallToolResult, JobsOutput, error) {
		s, err := session(ctx)
		if err != nil {
			return nil, JobsOutput{}, err
		}

		switch in.Op {
		case "list":
			limit := in.Limit
			if limit <= 0 {
				limit = 20
			}
			jobs, err := s.Client().RecentJobs(ctx, limit)
			if err != nil {
				return nil, JobsOutput{}, err
			}
			return nil, JobsOutput{Op: in.Op, Jobs: jobs}, nil

		case "show":
			if in.JobID == 0 {
				return toolError(`operation "show" requires job_id`), JobsOutput{}, nil
			}
			job, err := s.Client().Job(ctx, in.JobID)
			if err != nil {
				// An id the target never knew about is a different problem
				// from a job that ran and failed.
				if errors.Is(err, truenas.ErrJobNotFound) {
					return toolError(fmt.Sprintf(
						"no job %d on the target; it may have been pruned", in.JobID)), JobsOutput{}, nil
				}
				return nil, JobsOutput{}, err
			}
			return nil, JobsOutput{Op: in.Op, Job: &job}, nil

		default:
			return toolError(fmt.Sprintf(
				"unknown operation %q for jobs; valid operations are: list, show", in.Op)), JobsOutput{}, nil
		}
	})
}
