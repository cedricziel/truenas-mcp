package tools

import "fmt"

// denial describes something this server will not do under any configuration.
//
// Entries constrain argument values as well as method names, because some
// methods are safe or unsafe depending on how they are called: deleting an app
// is recoverable by reinstalling it, and deleting it along with its volumes
// destroys data. A method-name denylist would force a choice between banning a
// useful operation outright and permitting data destruction inside it.
type denial struct {
	// Method is the middleware method this entry covers.
	Method string

	// Arg, when set, narrows the denial to calls where that argument is true.
	// When empty, the method is denied in every form.
	Arg string

	// Reason is shown to the caller.
	Reason string
}

// denials is deliberately a constant, not configuration. An operator who could
// switch these off would eventually switch them off.
var denials = []denial{
	{Method: "pool.export", Reason: "exporting a pool removes access to every byte on it"},
	{Method: "pool.dataset.delete", Reason: "deleting a dataset destroys its data and snapshots"},
	{Method: "disk.wipe", Reason: "wiping a disk is unrecoverable"},
	{Method: "boot.detach", Reason: "detaching a boot device can leave the system unbootable"},
	{Method: "pool.dataset.destroy_snapshots", Reason: "destroying snapshots removes the ability to recover"},

	// Argument-level: the method itself stays available in its safe form.
	{
		Method: "app.delete",
		Arg:    "remove_ixvolumes",
		Reason: "removing an app's volumes destroys its data; deleting the app alone is recoverable",
	},
	{
		Method: "pool.dataset.delete",
		Arg:    "recursive",
		Reason: "recursive deletion destroys every child dataset",
	},

	// Setting one path's ACL is the useful thing. Applying it recursively is
	// not: the blast radius is unbounded, undoing it requires knowing what
	// every child ACL was, and getting it wrong locks people out of terabytes.
	{
		Method: "filesystem.setacl",
		Arg:    "recursive",
		Reason: "applying an ACL recursively rewrites permissions on every child and cannot be undone without knowing the previous ones",
	},
	{
		Method: "filesystem.setacl",
		Arg:    "traverse",
		Reason: "traversing filesystem boundaries extends an ACL change beyond the dataset it was aimed at",
	},
	{
		Method: "filesystem.setacl",
		Arg:    "stripacl",
		Reason: "stripping an ACL discards the existing permissions rather than editing them",
	},
}

// DeniedError is a refusal originating from this server rather than the target.
type DeniedError struct {
	Method string
	Arg    string
	Reason string
}

func (e *DeniedError) Error() string {
	if e.Arg != "" {
		return fmt.Sprintf(
			"refusing %s with %s=true: %s. This cannot be enabled by configuration; "+
				"use the TrueNAS web interface if you need it.",
			e.Method, e.Arg, e.Reason)
	}
	return fmt.Sprintf(
		"refusing %s: %s. This cannot be enabled by configuration; "+
			"use the TrueNAS web interface if you need it.",
		e.Method, e.Reason)
}

// CheckDenied reports whether a call is permanently refused.
//
// It applies to every path that can reach a middleware method -- tools and the
// discovery escape hatch alike -- so there is no route around it.
func CheckDenied(method string, args map[string]any) error {
	for _, d := range denials {
		if d.Method != method {
			continue
		}
		if d.Arg == "" {
			return &DeniedError{Method: d.Method, Reason: d.Reason}
		}
		if isTrue(args[d.Arg]) {
			return &DeniedError{Method: d.Method, Arg: d.Arg, Reason: d.Reason}
		}
	}
	return nil
}

// DeniedMethods lists methods denied in every form, for startup reporting.
func DeniedMethods() []string {
	var out []string
	for _, d := range denials {
		if d.Arg == "" {
			out = append(out, d.Method)
		}
	}
	return out
}

func isTrue(v any) bool {
	b, ok := v.(bool)
	return ok && b
}
