package server

import (
	"context"
	"encoding/json"

	"github.com/cedricziel/truenas-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DispatchInput is the flat argument superset shared by the read tools.
//
// It is flat and mostly optional rather than a union discriminated on `op`,
// because models handle flat schemas measurably better than conditional ones
// and read operations genuinely share these arguments. The narrowness of the
// superset is what keeps that trade acceptable — it works for reads precisely
// because it would not work for writes.
type DispatchInput struct {
	Op string `json:"op" jsonschema:"which operation to run; see the tool description"`

	ID      string `json:"id,omitempty" jsonschema:"identifier, for operations that act on one object"`
	Name    string `json:"name,omitempty" jsonschema:"name, for operations that act on a named object such as an app"`
	Pool    string `json:"pool,omitempty" jsonschema:"restrict results to this pool"`
	Dataset string `json:"dataset,omitempty" jsonschema:"restrict results to this dataset"`
	Limit   int    `json:"limit,omitempty" jsonschema:"maximum number of results to return"`
}

// DispatchOutput carries the middleware's answer plus enough context for a
// caller to know what it is looking at and whether anything was withheld.
type DispatchOutput struct {
	Op        string `json:"op" jsonschema:"the operation that ran"`
	Count     int    `json:"count,omitempty" jsonschema:"number of items returned"`
	Total     int    `json:"total,omitempty" jsonschema:"total available, when more exist than were returned"`
	Truncated bool   `json:"truncated,omitempty" jsonschema:"whether the result was cut short"`
	Result    any    `json:"result" jsonschema:"the operation's result"`
}

// defaultLimit bounds collection results. Middleware collections can be large
// enough to swamp a context window, and a truncated answer that says so is
// more useful than a complete one that cannot be read.
const defaultLimit = 50

func (in DispatchInput) args() tools.Args {
	a := tools.Args{}
	if in.ID != "" {
		a["id"] = in.ID
	}
	if in.Name != "" {
		a["name"] = in.Name
	}
	if in.Pool != "" {
		a["pool"] = in.Pool
	}
	if in.Dataset != "" {
		a["dataset"] = in.Dataset
	}
	if in.Limit != 0 {
		a["limit"] = in.Limit
	}
	return a
}

// registerConcern exposes one concern as a dispatch tool.
func registerConcern(srv *mcp.Server, c *tools.Concern, session sessionFor, annotations *mcp.ToolAnnotations) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        c.Name,
		Description: c.ToolDescription(),
		Annotations: annotations,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in DispatchInput) (*mcp.CallToolResult, DispatchOutput, error) {
		op, err := c.Resolve(in.Op, in.args())
		if err != nil {
			// A resolution failure is the caller's mistake, not the target's.
			// Returning it as a tool error rather than a protocol error lets
			// the model read it and retry.
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			}, DispatchOutput{}, nil
		}

		s, err := session(ctx)
		if err != nil {
			return nil, DispatchOutput{}, err
		}

		params := middlewareParams(op, in)
		raw, err := s.Client().Call(ctx, op.Method, params...)
		if err != nil {
			return nil, DispatchOutput{}, err
		}

		return nil, shape(in.Op, raw, in.Limit), nil
	})
}

// middlewareParams turns the resolved operation and arguments into the
// positional parameters the middleware method expects.
func middlewareParams(op *tools.Op, in DispatchInput) []any {
	// get_instance-style methods take the identifier positionally.
	if in.ID != "" && contains(op.Args, "id") {
		return []any{in.ID}
	}
	if in.Name != "" && contains(op.Args, "name") {
		return []any{in.Name}
	}

	// Query methods take a filter list. Narrowing arguments become filters so
	// the target does the work rather than this server post-filtering.
	var filters []any
	if in.Pool != "" && contains(op.Args, "pool") {
		filters = append(filters, []any{"pool", "=", in.Pool})
	}
	if in.Dataset != "" && contains(op.Args, "dataset") {
		filters = append(filters, []any{"dataset", "=", in.Dataset})
	}
	if len(filters) > 0 {
		return []any{filters}
	}
	return nil
}

func contains(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

// shape bounds a collection result and reports what was withheld, so a
// truncated answer never reads as a complete one.
func shape(op string, raw json.RawMessage, limit int) DispatchOutput {
	if limit <= 0 {
		limit = defaultLimit
	}

	var list []any
	if err := json.Unmarshal(raw, &list); err != nil {
		// Not a collection; pass the object through.
		var single any
		if err := json.Unmarshal(raw, &single); err != nil {
			return DispatchOutput{Op: op, Result: string(raw)}
		}
		return DispatchOutput{Op: op, Result: single}
	}

	total := len(list)
	truncated := total > limit
	if truncated {
		list = list[:limit]
	}

	return DispatchOutput{
		Op:        op,
		Count:     len(list),
		Total:     total,
		Truncated: truncated,
		Result:    list,
	}
}
