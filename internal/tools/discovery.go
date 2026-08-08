package tools

import (
	"fmt"
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

// readSuffixes are method shapes that read state. The middleware is consistent
// enough about these that matching on shape is reliable.
var readSuffixes = []string{
	".query", ".get_instance", ".config", ".choices", ".summary",
	".status", ".info", ".list", ".results", ".get_data", ".stats",
	"_choices", "_info", "_status", "_summary", "_versions", "_ids",
	"_get_data", "_results", "_query", "_list", "_images",
}

// writeSuffixes are method shapes that mutate state. Reachable only when the
// write tier is enabled.
var writeSuffixes = []string{
	".create", ".update", ".start", ".stop", ".restart", ".upgrade",
	".rollback", ".redeploy", ".pull_images", ".sync", ".scrub",
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
func CheckDiscoverable(method string, writesEnabled bool) error {
	// The permanent denylist outranks everything, including writes being on.
	if err := CheckDenied(method, nil); err != nil {
		return err
	}

	// Write shapes are checked FIRST. Some mutating methods end in something
	// that also reads as a read suffix — app.pull_images ends in "_images" —
	// and matching reads first would let them through the gate. When the two
	// sets overlap, the safer classification has to win.
	if matchesAny(method, writeSuffixes) {
		if !writesEnabled {
			return &DiscoveryError{
				Method: method,
				Reason: "it mutates state and the write tier is disabled",
			}
		}
		return nil
	}

	if matchesAny(method, readSuffixes) {
		return nil
	}

	return &DiscoveryError{
		Method: method,
		Reason: "it matches no allowed method shape",
	}
}

func matchesAny(method string, suffixes []string) bool {
	for _, s := range suffixes {
		if strings.HasSuffix(method, s) {
			return true
		}
	}
	return false
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
