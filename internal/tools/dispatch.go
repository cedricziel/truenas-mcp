// Package tools builds the MCP tool surface over the TrueNAS middleware.
//
// Reads are grouped into concern-level tools that take an `op` enum, because
// read operations share almost all their arguments and 800-odd middleware
// methods cannot each become a tool. Writes are deliberately NOT dispatched
// this way: MCP annotations are per-tool, so bundling a safe operation with a
// destructive one behind one `op` parameter would force a single consent gate
// over both.
package tools

import (
	"fmt"
	"sort"
	"strings"
)

// Args are the arguments supplied to a dispatch tool. The schema is a flat
// superset across a concern's operations rather than a union discriminated on
// `op`, because models handle flat schemas measurably better and reads share
// most of their arguments anyway.
type Args map[string]any

// Op is one operation within a concern.
type Op struct {
	// Name is the value of `op` that selects this operation.
	Name string

	// Summary is what a model reads to choose between operations.
	Summary string

	// Method is the middleware method this maps to.
	Method string

	// Args are the argument names this operation accepts. Anything outside
	// this set is refused rather than ignored.
	Args []string

	// Required are the argument names this operation cannot run without.
	Required []string

	// Project names the fields a list-shaped result is reduced to by default.
	// Unset means the full middleware object is returned, which stays the
	// right default for anything that is not already known to be large: a
	// declared projection is a claim that these particular fields are what
	// answers the question, and that claim should not be guessed.
	Project []string

	// Filters names the arguments this operation passes to the target as
	// query filters rather than as a positional parameter, and how. This
	// replaces a dispatcher-wide hardcoded mapping (pool, dataset) with a
	// per-operation declaration: which argument, which middleware field it
	// narrows, and which comparison operator to apply. An argument's role is
	// a property of the operation that declares it -- the same argument name
	// can be positional on one operation (Args) and a filter on another.
	Filters []Filter

	// Options is the middleware options object this operation always sends,
	// for a method that refuses a call whose object parameter carries no
	// bound at all rather than accepting a bare filter list or no parameters.
	// audit.query is the reason this exists: verified live against TrueNAS
	// 26, it returns -32602 Invalid params for no parameters, for {}, and
	// even for {"query-options":{}} -- query-options must be present and
	// must itself carry a bound such as limit before the target accepts the
	// call at all. describe_method reports zero required parameters for it,
	// so nothing in the middleware's own introspection says this.
	//
	// Because the bound has to be present for the call to succeed, it cannot
	// be applied the way shape() applies limit to every other operation's
	// response -- after the fact, locally. It has to travel to the target
	// instead, which server.middlewareParams does by merging the caller's
	// effective limit into Options["query-options"]["limit"] at dispatch
	// time. Options itself declares only what stays constant across every
	// call, such as ordering; it must never carry a limit of its own; a
	// hardcoded one would silently cap a caller who asked for more.
	//
	// An operation that declares Options is also telling shape() something
	// it cannot learn from the response alone: that the size limit was
	// enforced by the target, not applied locally to a complete answer. See
	// shape()'s boundAtTarget parameter for what that changes.
	Options map[string]any
}

// Filter declares one argument as a middleware query filter: which argument
// supplies the value, which field on the target it narrows, and which
// comparison operator joins them.
//
// The operator is declared rather than inferred from the argument's value
// (e.g. "a list gets membership") because the field's shape lives on the
// target, not in what the caller sent -- categories is list-valued and needs
// "rin", pool is scalar and needs "=", and only the operation author, who has
// actually checked the target, knows which. Inferring it from the value's Go
// type would be right by luck rather than by design.
type Filter struct {
	// Arg is the DispatchInput argument name that supplies the value.
	Arg string

	// Field is the middleware field name the filter narrows on.
	Field string

	// Operator is the middleware comparison operator, such as "=" for
	// equality or "rin" for membership on a list-valued field.
	Operator string
}

// Concern is a user-facing grouping of read operations, exposed as one tool.
type Concern struct {
	Name string

	// Title is shown to a human in a client's consent dialog, where the raw
	// tool name reads poorly.
	Title string

	Description string
	Ops         []Op
}

// OpNames lists the operations this concern exposes, in declaration order.
func (c *Concern) OpNames() []string {
	names := make([]string, 0, len(c.Ops))
	for _, op := range c.Ops {
		names = append(names, op.Name)
	}
	return names
}

// acceptedArgs is the full set of argument names Resolve accepts for this
// operation: positional Args plus the Arg of each declared filter, in that
// order and de-duplicated. Kept as one function so the accept-set used to
// validate a call and the one reported back in a refusal can never drift
// apart.
func (op *Op) acceptedArgs() []string {
	seen := make(map[string]bool, len(op.Args)+len(op.Filters))
	out := make([]string, 0, len(op.Args)+len(op.Filters))
	for _, a := range op.Args {
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	for _, f := range op.Filters {
		if !seen[f.Arg] {
			seen[f.Arg] = true
			out = append(out, f.Arg)
		}
	}
	return out
}

// Resolve selects an operation and validates the supplied arguments against it.
//
// Errors are written so a model that guessed wrong can recover in one turn:
// they name the operation, the offending argument, and what would have been
// valid instead.
func (c *Concern) Resolve(opName string, args Args) (*Op, error) {
	var op *Op
	for i := range c.Ops {
		if c.Ops[i].Name == opName {
			op = &c.Ops[i]
			break
		}
	}
	if op == nil {
		return nil, fmt.Errorf("unknown operation %q for %s; valid operations are: %s",
			opName, c.Name, strings.Join(c.OpNames(), ", "))
	}

	// A filter is another way of reaching the target, not a response-shaping
	// knob, so it belongs in the same accept-or-refuse set as a positional
	// identifier: the allowed set is Args plus each declared filter's Arg.
	accepted := op.acceptedArgs()
	allowed := map[string]bool{}
	for _, a := range accepted {
		allowed[a] = true
	}

	// Refuse rather than ignore: an argument meant for another operation
	// signals a misunderstanding, and silently dropping it would return a
	// confidently wrong answer.
	var unexpected []string
	for name := range args {
		if !allowed[name] {
			unexpected = append(unexpected, name)
		}
	}
	if len(unexpected) > 0 {
		sort.Strings(unexpected)
		valid := "none"
		if len(accepted) > 0 {
			valid = strings.Join(accepted, ", ")
		}
		return nil, fmt.Errorf("operation %q does not accept %s; it accepts: %s",
			opName, strings.Join(unexpected, ", "), valid)
	}

	var missing []string
	for _, name := range op.Required {
		if v, ok := args[name]; !ok || v == nil || v == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("operation %q requires %s",
			opName, strings.Join(missing, ", "))
	}

	return op, nil
}

// ToolDescription renders the concern and its operations as the tool
// description a model selects from.
func (c *Concern) ToolDescription() string {
	var b strings.Builder
	b.WriteString(c.Description)
	b.WriteString("\n\nOperations:\n")
	for _, op := range c.Ops {
		fmt.Fprintf(&b, "  %s — %s", op.Name, op.Summary)
		if len(op.Required) > 0 {
			fmt.Fprintf(&b, " (requires %s)", strings.Join(op.Required, ", "))
		}
		b.WriteString("\n")
	}
	return b.String()
}
