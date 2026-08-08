package tools

import (
	"fmt"
	"slices"
	"strings"
)

// The long tail of the middleware — ups, ipmi, smart, kerberos, and eighty
// other namespaces — will never justify hand-written tools, but occasionally
// somebody needs it. Discovery covers that tail without a code change per
// release.
//
// It is bounded by an ALLOWLIST, not a denylist. An allowlist fails closed: a
// method invented in a future TrueNAS release is unreachable until someone
// decides otherwise. A denylist maintained against an API that grows every six
// months fails open, which is the wrong default for a box holding the only
// copy of someone's data.
//
// Entries are patterns over method shapes rather than an enumeration of names,
// so renames and additions inside an allowed shape keep working. Enumerating
// names would be the transcription problem this design exists to avoid.

// MethodInfo is the introspection this server needs to decide whether a method
// may be reached through discovery.
type MethodInfo struct {
	Name  string
	Roles []string
	Job   bool
}

// readOnlyRole is the middleware's own marker for a method that does not
// mutate. Using it beats guessing from a method's name: it is the target's
// answer, it is exact, and it tracks API versions without a change here.
const readOnlyRole = "READONLY_ADMIN"

// discoveryExclusions are namespaces and methods withheld from discovery
// regardless of what their roles claim.
//
// These are not unrecoverable in the way the denylist's entries are -- they are
// withheld because reaching them would undermine the gating itself.
var discoveryExclusions = []struct {
	prefix string
	reason string
}{
	{
		// core.bulk invokes arbitrary methods. Reachable, it would bypass the
		// denylist, the write tier, and every other gate in this server.
		prefix: "core.bulk",
		reason: "it invokes arbitrary methods and would bypass this server's gating",
	},
	{
		// Authentication is the server's to manage. A caller driving it could
		// mint a token that outlives the session and is invisible in the API
		// keys UI, which is a credential the operator never issued.
		prefix: "auth.",
		reason: "authentication is managed by this server, not by callers",
	},
	{
		prefix: "core.subscribe",
		reason: "event subscription is managed by this server",
	},
	{
		prefix: "core.unsubscribe",
		reason: "event subscription is managed by this server",
	},
}

// DiscoveryError is a refusal from this server's gating rather than the target.
type DiscoveryError struct {
	Method string
	Reason string
}

func (e *DiscoveryError) Error() string {
	return fmt.Sprintf("%s is not exposed by this server: %s", e.Method, e.Reason)
}

// CheckDiscoverable reports whether a method may be described or invoked
// through the discovery tier.
//
// Refusals deliberately do not say whether the method exists on the target.
// Confirming existence would turn discovery into an enumeration oracle for
// exactly the methods that are being withheld.
func CheckDiscoverable(m MethodInfo, writesEnabled bool) error {
	// The permanent denylist outranks everything, including writes being on.
	if err := CheckDenied(m.Name, nil); err != nil {
		return err
	}

	for _, x := range discoveryExclusions {
		if strings.HasPrefix(m.Name, x.prefix) {
			return &DiscoveryError{Method: m.Name, Reason: x.reason}
		}
	}

	// A method declaring no roles has no privilege check at all. On this target
	// those are mostly session and protocol plumbing rather than harmless
	// reads, so "no roles" is treated as unknown risk rather than no risk --
	// except for the few individually established to be side-effect free.
	if len(m.Roles) == 0 {
		if IsSafeUnroledRead(m.Name) {
			return nil
		}
		return &DiscoveryError{
			Method: m.Name,
			Reason: "it declares no privilege requirement, so its effect cannot be established",
		}
	}

	if slices.Contains(m.Roles, readOnlyRole) {
		return nil
	}

	if !writesEnabled {
		return &DiscoveryError{
			Method: m.Name,
			Reason: "it mutates state and the write tier is disabled",
		}
	}
	return nil
}

// SummarizeSchema reduces a middleware argument schema to what a caller needs.
//
// Returning the raw schema is the lazy option and the worst one: measured on a
// live target, sharing.smb.create's schema is ~31,000 characters and
// directoryservices.update's ~53,000. Models also fill large sparse schemas
// less accurately than small dense ones, so a faithful dump costs more and
// works worse.
func SummarizeSchema(params []SchemaParam, full bool) SchemaSummary {
	// Middleware methods overwhelmingly take one object parameter wrapping the
	// real arguments. Summarising only the top level would keep that object
	// whole and reduce nothing, so flatten into it first.
	if len(params) == 1 && len(params[0].Properties) > 0 {
		params = params[0].Properties
	}

	kept := []FlatParam{}
	omitted := 0
	for _, p := range params {
		// Required parameters and those without a default are what a caller
		// actually has to decide about; everything else has a working value
		// already and only adds tokens.
		if full || p.Required || p.Default == nil {
			kept = append(kept, FlatParam{
				Name:        p.Name,
				Type:        p.Type,
				Description: p.Description,
				Required:    p.Required,
				Default:     p.Default,
			})
			continue
		}
		omitted++
	}

	return SchemaSummary{Params: kept, Omitted: omitted}
}

// FlatParam is a summarised argument. It deliberately carries no nested
// properties: the summary is always one level deep, which is what keeps the
// output schema expressible and the result readable.
type FlatParam struct {
	Name        string `json:"name"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
	Default     any    `json:"default,omitempty" jsonschema:"the value used when this argument is omitted"`
}

// SchemaParam is one argument of a middleware method.
type SchemaParam struct {
	Name        string `json:"name"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
	Default     any    `json:"default,omitempty" jsonschema:"the value used when this argument is omitted"`

	// Properties are the nested fields when this parameter is an object,
	// which is the usual middleware shape.
	Properties []SchemaParam `json:"properties,omitempty" jsonschema:"nested fields when this parameter is an object"`
}

// SchemaSummary is a reduced argument schema plus what was left out.
type SchemaSummary struct {
	Params  []FlatParam `json:"params"`
	Omitted int         `json:"omitted_optional_params,omitempty"`
}
