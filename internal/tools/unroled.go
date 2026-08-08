package tools

// A handful of middleware methods declare no roles at all. The general rule
// treats that as unknown risk rather than no risk, because on this target most
// unroled methods are session and protocol plumbing — login, subscribe, abort,
// bulk execution — not harmless reads.
//
// A few are genuinely harmless, and refusing them costs real capability:
// `system.version` is a pure read that this server itself calls on every
// session, and `core.get_jobs` is how a caller sees what the system is doing.
//
// So they are allowed individually, each because someone established it reads
// and nothing more. This list is deliberately short and deliberately explicit:
// the alternative is trusting "no declared roles" to mean "safe", which is what
// would let `core.bulk` through.

// safeUnroledReads are unroled methods established to be side-effect free.
var safeUnroledReads = map[string]string{
	"system.version":       "returns the release string",
	"system.version_short": "returns the release string",
	"system.boot_id":       "returns an identifier for the current boot",
	"system.product_type":  "returns the edition name",
	"core.get_jobs":        "lists jobs; how a caller sees what the system is doing",
	"core.ping":            "liveness check, returns pong",
	"core.get_services":    "lists middleware services",

	"directoryservices.status":            "reports whether a directory service is connected",
	"failover.licensed":                   "reports whether failover is licensed",
	"privilege.roles":                     "lists the roles this system defines",
	"user.has_local_administrator_set_up": "reports whether an admin account exists",

	// Deliberately absent, though also unroled: everything under auth.*
	// (session control and token minting), core.bulk (arbitrary method
	// execution), core.subscribe / core.unsubscribe (event plumbing this
	// server owns), core.job_abort and core.set_options (both mutate),
	// core.job_wait (blocks), core.download and core.resize_shell.
}

// IsSafeUnroledRead reports whether a method with no declared roles has been
// individually established as a harmless read.
func IsSafeUnroledRead(method string) bool {
	_, ok := safeUnroledReads[method]
	return ok
}
