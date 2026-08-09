package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cedricziel/truenas-mcp/internal/tools"
	"github.com/cedricziel/truenas-mcp/internal/truenas"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// WriteTargetOnlyInput is the shape for a write tool with no options: the
// target is all there is to decide.
type WriteTargetOnlyInput struct {
	Target string `json:"target" jsonschema:"the app or dataset to act on"`
}

func (in WriteTargetOnlyInput) target() string           { return in.Target }
func (in WriteTargetOnlyInput) denyArgs() map[string]any { return nil }
func (in WriteTargetOnlyInput) params(target string) []any {
	return []any{target}
}

// WriteRedeployInput is the shape for a write tool whose WriteOp declares the
// "redeploy" option — currently only app_pull_images.
type WriteRedeployInput struct {
	Target string `json:"target" jsonschema:"the app or dataset to act on"`

	// Pointer so an omitted value is distinguishable from an explicit false,
	// since the middleware default is true.
	Redeploy *bool `json:"redeploy,omitempty" jsonschema:"redeploy after pulling images; defaults to true"`
}

func (in WriteRedeployInput) target() string { return in.Target }
func (in WriteRedeployInput) denyArgs() map[string]any {
	if in.Redeploy == nil {
		return nil
	}
	return map[string]any{"redeploy": *in.Redeploy}
}
func (in WriteRedeployInput) params(target string) []any {
	params := []any{target}
	if in.Redeploy != nil {
		params = append(params, map[string]any{"redeploy": *in.Redeploy})
	}
	return params
}

// WriteSnapshotInput is the shape for a write tool whose WriteOp declares the
// "snapshot_name" option — currently only create_snapshot.
type WriteSnapshotInput struct {
	Target       string `json:"target" jsonschema:"the app or dataset to act on"`
	SnapshotName string `json:"snapshot_name,omitempty" jsonschema:"name for the new snapshot"`
}

func (in WriteSnapshotInput) target() string           { return in.Target }
func (in WriteSnapshotInput) denyArgs() map[string]any { return nil }
func (in WriteSnapshotInput) params(target string) []any {
	if in.SnapshotName == "" {
		// The middleware requires a name; a caller who omitted one gets a
		// deterministic error rather than a surprise snapshot name.
		return []any{map[string]any{"dataset": target}}
	}
	return []any{map[string]any{"dataset": target, "name": in.SnapshotName}}
}

// writeInput is implemented by every concrete input type above. mcp.AddTool
// needs a concrete Go type per tool at compile time, so a per-op input struct
// can't be synthesised at runtime from WriteOp.Options; this interface is
// what lets one registration function still work across all of them.
type writeInput interface {
	// target is the object to act on; every write requires one.
	target() string
	// denyArgs is what CheckDenied inspects: the option values that matter to
	// the denylist, if this shape carries any.
	denyArgs() map[string]any
	// params is what's sent to the middleware method, given the resolved
	// target.
	params(target string) []any
}

// JobStartedOutput is what a mutation returns. It never carries a result,
// because the operation has not finished: it carries the identity needed to
// watch it.
type JobStartedOutput struct {
	JobID    int64  `json:"job_id" jsonschema:"the job just started on the target; the call does not wait for it to finish"`
	Method   string `json:"method" jsonschema:"the middleware method invoked"`
	Target   string `json:"target" jsonschema:"the object acted upon"`
	Resource string `json:"resource" jsonschema:"resource URI tracking this job"`
	Note     string `json:"note" jsonschema:"how to follow the job to completion"`
}

// asyncContractNote is appended to every write tool's description. Composing
// it once here, rather than editing it into each WriteOp.Description by hand,
// is what keeps a dozen descriptions saying the same thing without drifting:
// the alternative is a phrase that a future op author paraphrases slightly
// differently, or forgets. The op descriptions stay focused on the decision
// ("this is how you update an app that tracks a moving tag"); this sentence
// documents the mechanism they all share.
const asyncContractNote = "Starts an asynchronous job and returns its id " +
	"immediately rather than a finished result; follow it with " +
	"jobs(op=\"show\", job_id=...) to see it complete."

func withAsyncContractNote(description string) string {
	return description + " " + asyncContractNote
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

// registerWrite picks the input type matching w's declared options and
// registers the tool with it. Adding a new option to a WriteOp without adding
// a matching case here panics at server construction — every test that builds
// a server catches it immediately — rather than silently registering a schema
// that doesn't advertise the new option, which is the bug this dispatch
// exists to prevent.
func registerWrite(srv *mcp.Server, w tools.WriteOp, session sessionFor) {
	switch {
	case len(w.Options) == 0:
		registerWriteOp[WriteTargetOnlyInput](srv, w, session)
	case len(w.Options) == 1 && w.Options[0] == "redeploy":
		registerWriteOp[WriteRedeployInput](srv, w, session)
	case len(w.Options) == 1 && w.Options[0] == "snapshot_name":
		registerWriteOp[WriteSnapshotInput](srv, w, session)
	default:
		panic(fmt.Sprintf(
			"write tool %q declares options %v with no matching input type; "+
				"add one alongside WriteTargetOnlyInput in write_tool.go", w.Name, w.Options))
	}
}

func registerWriteOp[In writeInput](srv *mcp.Server, w tools.WriteOp, session sessionFor) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        w.Name,
		Description: withAsyncContractNote(w.Description),
		Annotations: writeAnnotations(w.Title, w.Destructive, w.Idempotent),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, JobStartedOutput, error) {
		target := in.target()
		if target == "" {
			return toolError(fmt.Sprintf("%s requires a target", w.Name)), JobStartedOutput{}, nil
		}

		// Defence in depth: the denylist applies to every path that can reach
		// a middleware method, not only the discovery escape hatch.
		if err := tools.CheckDenied(w.Method, in.denyArgs()); err != nil {
			return toolError(err.Error()), JobStartedOutput{}, nil
		}

		s, err := session(ctx)
		if err != nil {
			return nil, JobStartedOutput{}, err
		}

		// Resolve the target before mutating, so consent was given against
		// something that exists rather than a name that may be a typo.
		if err := resolveTarget(ctx, s.Client(), w, target); err != nil {
			return toolError(err.Error()), JobStartedOutput{}, nil
		}

		jobID, err := s.Client().CallJob(ctx, w.Method, in.params(target)...)
		if err != nil {
			return nil, JobStartedOutput{}, err
		}

		return nil, JobStartedOutput{
			JobID:    jobID,
			Method:   w.Method,
			Target:   target,
			Resource: fmt.Sprintf("truenas://job/%d", jobID),
			Note:     "Started; not yet finished. Follow it with jobs(op=\"show\", job_id=...).",
		}, nil
	})
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
