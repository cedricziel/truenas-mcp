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

	allowed := map[string]bool{}
	for _, a := range op.Args {
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
		if len(op.Args) > 0 {
			valid = strings.Join(op.Args, ", ")
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
