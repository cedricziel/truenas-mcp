package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/cedricziel/truenas-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DescribeInput asks about one middleware method.
type DescribeInput struct {
	Method string `json:"method" jsonschema:"the middleware method, e.g. smart.test.results"`
	Full   bool   `json:"full,omitempty" jsonschema:"return the complete argument schema instead of a summary"`
}

// DescribeOutput summarises a method rather than dumping its schema.
type DescribeOutput struct {
	Method      string            `json:"method"`
	Description string            `json:"description,omitempty"`
	Job         bool              `json:"job,omitempty" jsonschema:"whether this starts a long-running job"`
	ReadOnly    bool              `json:"read_only" jsonschema:"whether the method only reads state"`
	Params      []tools.FlatParam `json:"params"`
	Omitted     int               `json:"omitted_optional_params,omitempty" jsonschema:"optional params left out; pass full=true for all"`
	Example     string            `json:"example,omitempty"`
}

// SearchInput finds methods by substring.
type SearchInput struct {
	Query string `json:"query" jsonschema:"substring to match against method names"`
	Limit int    `json:"limit,omitempty"`
}

// SearchOutput lists matching methods a caller may actually use.
type SearchOutput struct {
	Query   string   `json:"query"`
	Methods []string `json:"methods"`
	Count   int      `json:"count"`
	Note    string   `json:"note,omitempty"`
}

// CallInput invokes a method through the escape hatch.
type CallInput struct {
	Method string `json:"method" jsonschema:"the middleware method to call"`
	Params []any  `json:"params,omitempty" jsonschema:"positional parameters, in order"`
}

// CallOutput is a raw middleware result, or a job identity for job methods.
type CallOutput struct {
	Method string `json:"method"`
	JobID  int64  `json:"job_id,omitempty" jsonschema:"set when the method started a job rather than returning a result"`
	Result any    `json:"result,omitempty" jsonschema:"the method's return value, whatever shape it has"`
	Note   string `json:"note,omitempty"`
}

// registerDiscovery exposes the long-tail escape hatch.
//
// Everything here is bounded by the same allowlist, so discovery widens what
// is convenient without widening what is reachable.
func registerDiscovery(srv *mcp.Server, session sessionFor, writesEnabled bool) {
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}

	mcp.AddTool(srv, &mcp.Tool{
		Name: "search_methods",
		Description: "Find TrueNAS middleware methods by name when no dedicated tool covers " +
			"what you need. Only methods this server will actually run are listed.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, SearchOutput, error) {
		if strings.TrimSpace(in.Query) == "" {
			return toolError("search_methods requires a query"), SearchOutput{}, nil
		}

		s, err := session(ctx)
		if err != nil {
			return nil, SearchOutput{}, err
		}

		all, err := methodNames(ctx, s)
		if err != nil {
			return nil, SearchOutput{}, err
		}

		limit := in.Limit
		if limit <= 0 {
			limit = 40
		}

		var matched []string
		hidden := 0
		for _, name := range all {
			if !strings.Contains(name, in.Query) {
				continue
			}
			// Methods this server would refuse are not listed: offering them
			// would just produce a refusal one turn later.
			if tools.CheckDiscoverable(name, writesEnabled) != nil {
				hidden++
				continue
			}
			matched = append(matched, name)
		}
		sort.Strings(matched)
		if matched == nil {
			matched = []string{}
		}

		note := ""
		if len(matched) > limit {
			note = fmt.Sprintf("%d matches; showing %d", len(matched), limit)
			matched = matched[:limit]
		}
		if hidden > 0 {
			if note != "" {
				note += ". "
			}
			note += fmt.Sprintf("%d further matches are not exposed by this server", hidden)
		}

		return nil, SearchOutput{Query: in.Query, Methods: matched, Count: len(matched), Note: note}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "describe_method",
		Description: "Show what a middleware method takes and whether it starts a job. " +
			"Returns the arguments that matter plus a count of omitted optional ones; " +
			"pass full=true for the complete schema.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in DescribeInput) (*mcp.CallToolResult, DescribeOutput, error) {
		if err := tools.CheckDiscoverable(in.Method, writesEnabled); err != nil {
			return toolError(err.Error()), DescribeOutput{}, nil
		}

		s, err := session(ctx)
		if err != nil {
			return nil, DescribeOutput{}, err
		}

		meta, err := methodMetadata(ctx, s, in.Method)
		if err != nil {
			return toolError(err.Error()), DescribeOutput{}, nil
		}

		summary := tools.SummarizeSchema(meta.params(), in.Full)
		return nil, DescribeOutput{
			Method:      in.Method,
			Description: meta.Description,
			Job:         meta.Job,
			ReadOnly:    meta.readOnly(),
			Params:      summary.Params,
			Omitted:     summary.Omitted,
			Example:     meta.example(in.Method),
		}, nil
	})

	destructive := true
	mcp.AddTool(srv, &mcp.Tool{
		Name: "call_method",
		Description: "Invoke a TrueNAS middleware method directly. Use describe_method first " +
			"to learn its parameters. Only methods this server allows are callable.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &destructive},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in CallInput) (*mcp.CallToolResult, CallOutput, error) {
		if err := tools.CheckDiscoverable(in.Method, writesEnabled); err != nil {
			return toolError(err.Error()), CallOutput{}, nil
		}
		// Argument-level denials apply here too: the parameters are positional,
		// so check any object among them.
		for _, p := range in.Params {
			if m, ok := p.(map[string]any); ok {
				if err := tools.CheckDenied(in.Method, m); err != nil {
					return toolError(err.Error()), CallOutput{}, nil
				}
			}
		}

		s, err := session(ctx)
		if err != nil {
			return nil, CallOutput{}, err
		}

		meta, err := methodMetadata(ctx, s, in.Method)
		if err != nil {
			return toolError(err.Error()), CallOutput{}, nil
		}

		// A job method must not block the tool call.
		if meta.Job {
			id, err := s.Client().CallJob(ctx, in.Method, in.Params...)
			if err != nil {
				return nil, CallOutput{}, err
			}
			return nil, CallOutput{
				Method: in.Method,
				JobID:  id,
				Note:   "Started; follow it with jobs(op=\"show\", job_id=...).",
			}, nil
		}

		raw, err := s.Client().Call(ctx, in.Method, in.Params...)
		if err != nil {
			return nil, CallOutput{}, err
		}
		var result any
		if err := json.Unmarshal(raw, &result); err != nil {
			result = string(raw)
		}
		return nil, CallOutput{Method: in.Method, Result: result}, nil
	})
}

// rawMethodMeta is the introspection payload this server relies on.
type rawMethodMeta struct {
	Description string        `json:"description"`
	Job         bool          `json:"job"`
	Roles       []string      `json:"roles"`
	Accepts     []acceptParam `json:"accepts"`
}

// acceptParam mirrors one middleware parameter, including the nested fields
// that carry the bulk of a schema's size.
type acceptParam struct {
	Name        string                 `json:"_name_"`
	Required    bool                   `json:"_required_"`
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	Default     any                    `json:"default"`
	Properties  map[string]nestedParam `json:"properties"`
}

type nestedParam struct {
	Type        string `json:"type"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Required    bool   `json:"_required_"`
	Default     any    `json:"default"`
}

func (m rawMethodMeta) readOnly() bool {
	for _, r := range m.Roles {
		if r == "READONLY_ADMIN" {
			return true
		}
	}
	return false
}

func (m rawMethodMeta) params() []tools.SchemaParam {
	out := make([]tools.SchemaParam, 0, len(m.Accepts))
	for _, a := range m.Accepts {
		p := tools.SchemaParam{
			Name:        a.Name,
			Type:        a.Type,
			Description: a.Description,
			Required:    a.Required,
			Default:     a.Default,
		}
		for name, n := range a.Properties {
			p.Properties = append(p.Properties, tools.SchemaParam{
				Name:        name,
				Type:        n.Type,
				Description: n.Description,
				Required:    n.Required,
				Default:     n.Default,
			})
		}
		sort.Slice(p.Properties, func(i, j int) bool {
			// Required fields first: they are what a caller must decide about.
			if p.Properties[i].Required != p.Properties[j].Required {
				return p.Properties[i].Required
			}
			return p.Properties[i].Name < p.Properties[j].Name
		})
		out = append(out, p)
	}
	return out
}

// example renders a minimal call from the required parameters. Only 34 of the
// target's 815 methods carry an examples field, so there is nothing to derive
// for the rest and one has to be constructed.
func (m rawMethodMeta) example(method string) string {
	var required []string
	for _, a := range m.Accepts {
		if a.Required {
			required = append(required, fmt.Sprintf("<%s>", a.Name))
		}
	}
	if len(required) == 0 {
		return fmt.Sprintf(`call_method(method="%s")`, method)
	}
	return fmt.Sprintf(`call_method(method="%s", params=[%s])`, method, strings.Join(required, ", "))
}

func methodMetadata(ctx context.Context, s *Session, method string) (rawMethodMeta, error) {
	raw, err := s.Client().Call(ctx, "core.get_methods")
	if err != nil {
		return rawMethodMeta{}, err
	}
	var all map[string]rawMethodMeta
	if err := json.Unmarshal(raw, &all); err != nil {
		return rawMethodMeta{}, fmt.Errorf("decoding method metadata: %w", err)
	}
	meta, ok := all[method]
	if !ok {
		return rawMethodMeta{}, fmt.Errorf("no method named %q on the target", method)
	}
	return meta, nil
}

func methodNames(ctx context.Context, s *Session) ([]string, error) {
	raw, err := s.Client().Call(ctx, "core.get_methods")
	if err != nil {
		return nil, err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, fmt.Errorf("decoding method list: %w", err)
	}
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	return names, nil
}
