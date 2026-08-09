package tools

// describe_method and call_method both surface the middleware's own
// description of a method, verbatim -- inventing a better one here would
// drift from whatever the next release actually does. For most methods that
// description is enough to call the thing correctly.
//
// A few are not. app.update's description reads "Update `app_name` app with
// new configuration", and its schema takes an optional `values` object -- a
// shape that, to anyone who has used a PATCH-style update elsewhere, reads as
// "send what you want changed". It is not: the middleware replaces the app's
// configuration outright, and a caller who sends only the fields they meant
// to change silences every field they didn't. That is not a missing detail in
// the schema; it is a property of the call that the schema cannot express and
// the description does not mention, and getting it wrong is a real
// misconfiguration rather than a style complaint.
//
// This table exists for exactly that gap: a method whose own description
// omits a material property of the call that a caller cannot infer from the
// argument schema. It is deliberately short, and every entry is added because
// someone established the caveat -- read the middleware's behaviour, hit the
// misconfiguration, confirmed it -- never because it seemed prudent to guess.
// A speculative caveat is worse than none: it trains a caller to distrust the
// ones that are actually true.
var methodCaveats = map[string]string{
	"app.update": "Replaces the app's configuration; it is not a patch. Read the current " +
		"config with apps(op=\"config\", name=...), apply your changes to that object, " +
		"and pass the whole thing back as `values` -- fields you leave out are not preserved.",
}

// MethodCaveat returns the caveat attached to a middleware method, or "" when
// none is established.
func MethodCaveat(method string) string {
	return methodCaveats[method]
}
