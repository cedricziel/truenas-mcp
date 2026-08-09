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
	Path    string `json:"path,omitempty" jsonschema:"a filesystem path, such as /mnt/tank/media"`

	// Category narrows a browse to entries carrying it. It is a query filter
	// like Pool and Dataset above, not a response-shaping argument: an
	// operation that does not declare it is refused for supplying it, per the
	// same contract those two already keep.
	Category string `json:"category,omitempty" jsonschema:"restrict results to this category; see catalog(op=\"categories\") for the vocabulary"`

	// Limit bounds the response, the same way on every op: shape() applies it
	// to any list-shaped result regardless of which op produced it, and
	// middlewareParams never reads it. It is deliberately absent from args()
	// below for the same reason Full is -- see that field's comment.
	Limit int `json:"limit,omitempty" jsonschema:"maximum number of results to return"`

	// Full is the escape hatch for ops that declare a default field
	// projection: it has no effect on ops that do not, and it never widens a
	// projection, only bypasses it entirely.
	Full bool `json:"full,omitempty" jsonschema:"return every field instead of the default subset, for operations that normally project down"`
}

// DispatchOutput carries the middleware's answer plus enough context for a
// caller to know what it is looking at and whether anything was withheld.
type DispatchOutput struct {
	Op        string `json:"op" jsonschema:"the operation that ran"`
	Count     int    `json:"count,omitempty" jsonschema:"number of items returned"`
	Total     int    `json:"total,omitempty" jsonschema:"total available, when more exist than were returned"`
	Truncated bool   `json:"truncated,omitempty" jsonschema:"whether the result was cut short"`
	// Projected mirrors Truncated's role but for fields rather than items: a
	// result with dropped fields must not read as complete any more than a
	// truncated list does. Pass full=true on the next call to get everything.
	Projected bool `json:"projected,omitempty" jsonschema:"whether each item was reduced to a default subset of fields; pass full=true to get everything"`
	Result    any  `json:"result" jsonschema:"the operation's result"`
}

// defaultLimit bounds collection results. Middleware collections can be large
// enough to swamp a context window, and a truncated answer that says so is
// more useful than a complete one that cannot be read.
const defaultLimit = 50

// args builds the set Concern.Resolve validates an op against. Limit and Full
// are both deliberately absent: Resolve exists to catch an argument that
// selects or narrows the wrong thing -- pool on an op with no pool filter --
// and neither of these does that. Both only ever reach shape(), which applies
// them uniformly to whatever the op returned, so gating either per-op could
// only produce a refusal for a response the server was about to shape
// correctly anyway. That refusal is exactly the defect this comment guards
// against: limit used to be threaded through here too, which meant an op had
// to separately declare it in Args to accept it at all, and 40 of 43 ops
// never did -- despite limit sitting right there on every concern's schema.
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
	if in.Path != "" {
		a["path"] = in.Path
	}
	if in.Category != "" {
		a["category"] = in.Category
	}
	return a
}

// registerConcern exposes one concern as a dispatch tool.
func registerConcern(srv *mcp.Server, c *tools.Concern, session sessionFor) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        c.Name,
		Description: c.ToolDescription(),
		Annotations: readAnnotations(c.Title),
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

		return nil, shape(in.Op, raw, in.Limit, op.Project, in.Full), nil
	})
}

// middlewareParams turns the resolved operation and arguments into the
// positional parameters the middleware method expects.
func middlewareParams(op *tools.Op, in DispatchInput) []any {
	// get_instance-style methods take the identifier positionally. This wins
	// over any declared filter when an operation has both: an operation that
	// takes an identifier positionally is fetching one record, and a filter
	// alongside it would be meaningless, so there is no merge to define --
	// resolving positionally first and returning is what this already did.
	if in.ID != "" && contains(op.Args, "id") {
		return []any{in.ID}
	}
	if in.Name != "" && contains(op.Args, "name") {
		return []any{in.Name}
	}
	if in.Path != "" && contains(op.Args, "path") {
		return []any{in.Path}
	}

	// Query methods take a filter list. Each op declares which of its
	// arguments becomes a filter, against which middleware field and under
	// which comparison operator -- there is no dispatcher-wide notion of "a
	// filterable argument" any more, because the same argument name can play
	// a different role on a different op (see design.md's "Declared filters
	// widen what Resolve accepts"). in.args() is the single place that maps
	// an argument name to its value, so it is reused here rather than
	// re-deriving that mapping.
	argVals := in.args()
	var filters []any
	for _, f := range op.Filters {
		v, ok := argVals[f.Arg]
		if !ok {
			continue
		}
		filters = append(filters, []any{f.Field, f.Operator, v})
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
// truncated answer never reads as a complete one. project, when the op
// declares one and full is not set, additionally reduces each item to a
// named subset of fields -- the same principle applied to fields that limit
// already applies to item count.
func shape(op string, raw json.RawMessage, limit int, project []string, full bool) DispatchOutput {
	if limit <= 0 {
		limit = defaultLimit
	}

	var list []any
	if err := json.Unmarshal(raw, &list); err != nil {
		// Not a collection; pass the object through untouched. Projection is
		// a list-shaped concern -- a single object is already the answer the
		// caller asked for, not a page of them. limit is the same kind of
		// no-op here: since it is no longer gated per-op (see args()), a
		// caller can pass it to a single-object op such as apps show, and it
		// is silently inert rather than refused. That is the same trade full
		// already makes, and it is accepted rather than worked around --
		// there is no reliable declaration of which ops are list-shaped to
		// build such a check on, short of hand-maintained data of exactly the
		// kind this fix removes.
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

	// Projected reports what happened, not what was configured -- the same
	// contract Truncated keeps. An empty page, or one whose items already
	// carry nothing but the declared fields, has lost nothing and must not
	// claim to have.
	var projected bool
	if len(project) > 0 && !full {
		for i, item := range list {
			reduced, dropped := projectFields(item, project)
			list[i] = reduced
			projected = projected || dropped
		}
	}

	return DispatchOutput{
		Op:        op,
		Count:     len(list),
		Total:     total,
		Truncated: truncated,
		Projected: projected,
		Result:    list,
	}
}

// projectFields reduces one item to a named subset of its fields, reporting
// whether that actually removed anything. An item that is not an object --
// unexpected, but not this function's place to diagnose -- passes through
// unchanged rather than being discarded.
func projectFields(item any, fields []string) (any, bool) {
	obj, ok := item.(map[string]any)
	if !ok {
		return item, false
	}
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		if v, ok := obj[f]; ok {
			out[f] = v
		}
	}
	return out, len(out) < len(obj)
}
