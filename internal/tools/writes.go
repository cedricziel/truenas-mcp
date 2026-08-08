package tools

// Writes are individual operations, never dispatched behind an `op` enum.
//
// MCP annotations are per-tool, so bundling `start` with `delete` under one
// tool would force a single consent gate over both — and a user who tires of
// confirming a restart will allowlist the tool, silently granting whatever
// else it can do. Each write is therefore its own tool with its own
// annotation and its own consent decision.
//
// Every operation here is either self-reversing or reversed by Rollback. That
// is what makes this set defensible: the worst realistic outcome is a service
// interrupted for as long as it takes to notice and roll back, which is
// categorically different from destroying data.

// WriteOp is one mutating operation, exposed as its own tool.
type WriteOp struct {
	// Name is the tool name.
	Name string

	// Title is shown to a human in a client's consent dialog. A mutating tool
	// is exactly where that matters: "Stop an app" is a decision someone can
	// make, "app_stop" is an identifier.
	Title string

	// Description is what the model reads.
	Description string

	// Method is the middleware method invoked.
	Method string

	// Destructive marks operations that interrupt a service or discard state.
	// It maps to MCP's destructiveHint, which clients use to decide whether
	// to prompt.
	Destructive bool

	// Idempotent marks operations where repeating the call is harmless.
	Idempotent bool

	// TargetArg is the argument naming the object acted upon. It is always
	// required: a mutation must resolve to one concrete object before it runs.
	TargetArg string

	// Options are additional boolean arguments the operation accepts.
	Options []string
}

// AppWrites is the v1 mutation surface.
func AppWrites() []WriteOp {
	return []WriteOp{
		{
			Name:  "app_pull_images",
			Title: "Pull new images and redeploy an app",
			Description: "Pull the latest container images for an installed app and redeploy it. " +
				"This is how you update an app that tracks a moving image tag. " +
				"Redeploys by default; the app is briefly unavailable while it restarts. " +
				"Reversible with app_rollback.",
			Method:      "app.pull_images",
			Destructive: true,
			TargetArg:   "app_name",
			Options:     []string{"redeploy"},
		},
		{
			Name:  "app_redeploy",
			Title: "Redeploy an app",
			Description: "Redeploy an installed app without pulling new images. " +
				"Use this to apply a configuration change or restart a stuck app. " +
				"The app is briefly unavailable.",
			Method:      "app.redeploy",
			Destructive: true,
			TargetArg:   "app_name",
		},
		{
			Name:        "app_start",
			Title:       "Start an app",
			Description: "Start a stopped app.",
			Method:      "app.start",
			Idempotent:  true,
			TargetArg:   "app_name",
		},
		{
			Name:  "app_stop",
			Title: "Stop an app",
			Description: "Stop a running app. The app becomes unavailable until started again. " +
				"Reversible with app_start.",
			Method:      "app.stop",
			Destructive: true,
			Idempotent:  true,
			TargetArg:   "app_name",
		},
		{
			Name:  "app_upgrade",
			Title: "Upgrade an app",
			Description: "Upgrade an app to a newer catalog version. " +
				"Check apps/upgrade_summary first to see what would change. " +
				"Reversible with app_rollback.",
			Method:      "app.upgrade",
			Destructive: true,
			TargetArg:   "app_name",
		},
		{
			Name:  "app_rollback",
			Title: "Roll an app back to a previous version",
			Description: "Roll an app back to a previous version. " +
				"This is the recovery path for a bad upgrade or image pull. " +
				"Use apps/rollback_versions to see what is available.",
			Method:      "app.rollback",
			Destructive: true,
			TargetArg:   "app_name",
		},
	}
}

// SnapshotWrite is the additive companion to any risky operation.
func SnapshotWrite() WriteOp {
	return WriteOp{
		Name:  "create_snapshot",
		Title: "Create a snapshot",
		Description: "Create a ZFS snapshot of a dataset. Additive and cheap; " +
			"take one before any risky change.",
		Method:    "pool.snapshot.create",
		TargetArg: "dataset",
	}
}

// WriteOps is the full v1 mutation surface.
func WriteOps() []WriteOp {
	return append(AppWrites(), SnapshotWrite())
}

// RollbackToolName is the recovery path that must be present whenever any
// reversible app mutation is exposed.
const RollbackToolName = "app_rollback"
