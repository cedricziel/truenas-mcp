package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cedricziel/truenas-mcp/internal/tools"
	"github.com/cedricziel/truenas-mcp/internal/truenas"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// WriteInput is the argument shape shared by the mutating tools. Unlike the
// read side there is no `op`: each mutation is its own tool, so the target and
// its options are all that remain.
type WriteInput struct {
	Target string `json:"target" jsonschema:"the app or dataset to act on"`

	// Redeploy applies to image pulls. Pointer so an omitted value is
	// distinguishable from an explicit false, since the middleware default
	// is true.
	Redeploy *bool `json:"redeploy,omitempty" jsonschema:"redeploy after pulling images; defaults to true"`

	// SnapshotName applies to snapshot creation.
	SnapshotName string `json:"snapshot_name,omitempty" jsonschema:"name for the new snapshot"`
}

// JobStartedOutput is what a mutation returns. It never carries a result,
// because the operation has not finished: it carries the identity needed to
// watch it.
type JobStartedOutput struct {
	JobID    int64  `json:"job_id" jsonschema:"the job started on the target"`
	Method   string `json:"method" jsonschema:"the middleware method invoked"`
	Target   string `json:"target" jsonschema:"the object acted upon"`
	Resource string `json:"resource" jsonschema:"resource URI tracking this job"`
	Note     string `json:"note" jsonschema:"how to follow the job to completion"`
}

// registerWrites exposes the mutating tier. Each operation becomes its own
// tool so it carries its own annotation and its own consent decision; nothing
// here is reachable when the tier is disabled, because the tool simply is not
// registered.
func registerWrites(srv *mcp.Server, session sessionFor) {
	for _, w := range tools.WriteOps() {
		registerWrite(srv, w, session)
	}
}

func registerWrite(srv *mcp.Server, w tools.WriteOp, session sessionFor) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        w.Name,
		Description: w.Description,
		Annotations: writeAnnotations(w.Title, w.Destructive, w.Idempotent),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in WriteInput) (*mcp.CallToolResult, JobStartedOutput, error) {
		if in.Target == "" {
			return toolError(fmt.Sprintf("%s requires a target", w.Name)), JobStartedOutput{}, nil
		}

		args := map[string]any{}
		if in.Redeploy != nil {
			args["redeploy"] = *in.Redeploy
		}

		// Defence in depth: the denylist applies to every path that can reach
		// a middleware method, not only the discovery escape hatch.
		if err := tools.CheckDenied(w.Method, args); err != nil {
			return toolError(err.Error()), JobStartedOutput{}, nil
		}

		s, err := session(ctx)
		if err != nil {
			return nil, JobStartedOutput{}, err
		}

		// Resolve the target before mutating, so consent was given against
		// something that exists rather than a name that may be a typo.
		if err := resolveTarget(ctx, s.Client(), w, in.Target); err != nil {
			return toolError(err.Error()), JobStartedOutput{}, nil
		}

		params := writeParams(w, in)
		jobID, err := s.Client().CallJob(ctx, w.Method, params...)
		if err != nil {
			return nil, JobStartedOutput{}, err
		}

		return nil, JobStartedOutput{
			JobID:    jobID,
			Method:   w.Method,
			Target:   in.Target,
			Resource: fmt.Sprintf("truenas://job/%d", jobID),
			Note:     "Started; not yet finished. Follow it with jobs(op=\"show\", job_id=...).",
		}, nil
	})
}

func writeParams(w tools.WriteOp, in WriteInput) []any {
	params := []any{in.Target}

	if w.Method == "app.pull_images" && in.Redeploy != nil {
		params = append(params, map[string]any{"redeploy": *in.Redeploy})
	}
	if w.Method == "pool.snapshot.create" {
		name := in.SnapshotName
		if name == "" {
			// The middleware requires a name; a caller who omitted one gets a
			// deterministic error rather than a surprise snapshot name.
			return []any{map[string]any{"dataset": in.Target}}
		}
		return []any{map[string]any{"dataset": in.Target, "name": name}}
	}
	return params
}

// resolveTarget confirms the object exists before a mutation runs.
func resolveTarget(ctx context.Context, c *truenas.Client, w tools.WriteOp, target string) error {
	if w.TargetArg != "app_name" {
		return nil // only app targets are resolvable this cheaply
	}

	raw, err := c.Call(ctx, "app.query", []any{[]any{"name", "=", target}})
	if err != nil {
		return err
	}
	var apps []any
	if err := json.Unmarshal(raw, &apps); err != nil {
		return fmt.Errorf("checking whether %q exists: %w", target, err)
	}
	switch len(apps) {
	case 1:
		return nil
	case 0:
		return fmt.Errorf("no app named %q is installed; use apps(op=\"list\") to see what is", target)
	default:
		return fmt.Errorf("%q matched %d apps; refusing to act on an ambiguous target", target, len(apps))
	}
}

func toolError(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}
}
