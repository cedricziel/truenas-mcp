package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cedricziel/truenas-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ConfigWriteInput carries a configuration object rather than a flat argument
// list. Share creation takes a dozen fields and ACL entries are structured, so
// flattening them would misrepresent the shape and lose nesting the middleware
// requires.
type ConfigWriteInput struct {
	ID int `json:"id,omitempty" jsonschema:"the record to change; required for update and delete"`

	Config map[string]any `json:"config,omitempty" jsonschema:"the configuration object; use describe_method on the underlying method to see its fields"`
}

// ConfigWriteOutput reports what changed.
type ConfigWriteOutput struct {
	Method string `json:"method" jsonschema:"the middleware method invoked"`
	Result any    `json:"result,omitempty" jsonschema:"the target's response, whatever shape it has"`
	Note   string `json:"note,omitempty"`
}

func registerConfigWrites(srv *mcp.Server, session sessionFor) {
	for _, w := range tools.ConfigWrites() {
		registerConfigWrite(srv, w, session)
	}
}

func registerConfigWrite(srv *mcp.Server, w tools.ConfigWriteOp, session sessionFor) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        w.Name,
		Description: w.Description,
		// Never idempotent: creating a share twice makes two shares, and an ACL
		// change depends on what was there before.
		Annotations: writeAnnotations(w.Title, w.Destructive, false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ConfigWriteInput) (*mcp.CallToolResult, ConfigWriteOutput, error) {
		if w.NeedsID && in.ID == 0 {
			return toolError(fmt.Sprintf("%s requires the id of the record to change", w.Name)),
				ConfigWriteOutput{}, nil
		}
		if w.NeedsConfig && len(in.Config) == 0 {
			return toolError(fmt.Sprintf(
				"%s requires a config object; call describe_method(method=%q) to see its fields",
				w.Name, w.Method)), ConfigWriteOutput{}, nil
		}

		// The argument-level denylist is what keeps this surface safe: the
		// methods themselves are permitted, and the dangerous shapes are not.
		if err := tools.CheckDenied(w.Method, in.Config); err != nil {
			return toolError(err.Error()), ConfigWriteOutput{}, nil
		}
		// Options nest one level on some methods, so check inside too.
		if opts, ok := in.Config["options"].(map[string]any); ok {
			if err := tools.CheckDenied(w.Method, opts); err != nil {
				return toolError(err.Error()), ConfigWriteOutput{}, nil
			}
		}

		s, err := session(ctx)
		if err != nil {
			return nil, ConfigWriteOutput{}, err
		}

		params := configParams(w, in)

		if isJobMethod(ctx, s, w.Method) {
			id, err := s.Client().CallJob(ctx, w.Method, params...)
			if err != nil {
				return nil, ConfigWriteOutput{}, err
			}
			return nil, ConfigWriteOutput{
				Method: w.Method,
				Result: map[string]any{"job_id": id},
				Note:   "Started; follow it with jobs(op=\"show\", job_id=...).",
			}, nil
		}

		raw, err := s.Client().Call(ctx, w.Method, params...)
		if err != nil {
			return nil, ConfigWriteOutput{}, err
		}
		var result any
		if err := json.Unmarshal(raw, &result); err != nil {
			result = string(raw)
		}
		return nil, ConfigWriteOutput{Method: w.Method, Result: result}, nil
	})
}

func configParams(w tools.ConfigWriteOp, in ConfigWriteInput) []any {
	switch {
	case w.NeedsID && w.NeedsConfig:
		return []any{in.ID, in.Config}
	case w.NeedsID:
		return []any{in.ID}
	default:
		return []any{in.Config}
	}
}

// isJobMethod asks the target rather than assuming: whether a method runs as a
// job is its own property and changes between releases.
func isJobMethod(ctx context.Context, s *Session, method string) bool {
	meta, err := methodMetadata(ctx, s, method)
	if err != nil {
		return false
	}
	return meta.Job
}
