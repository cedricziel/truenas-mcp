package tools

// The concerns below are task-shaped rather than namespace-shaped. TrueNAS 26
// exposes 815 methods across 74 top-level namespaces, and that hierarchy is an
// implementation artifact: "my containers" is spread across app, docker,
// container, lxc, and vm. Grouping by what a person is trying to find out
// discloses better than mirroring the API.
//
// Every method listed here grants READONLY_ADMIN on the target, which is the
// middleware's own signal that it does not mutate. That is asserted in tests
// against a live box rather than trusted.

// Storage answers questions about pools, datasets, and snapshots.
func Storage() *Concern {
	return &Concern{
		Name:        "storage",
		Title:       "Storage: pools, datasets, snapshots",
		Description: "Read pools, datasets, and snapshots on the TrueNAS target.",
		Ops: []Op{
			{
				Name:    "list_pools",
				Summary: "every pool with its capacity and health",
				Method:  "pool.query",
			},
			{
				Name:     "show_pool",
				Summary:  "one pool in full",
				Method:   "pool.get_instance",
				Args:     []string{"id"},
				Required: []string{"id"},
			},
			{
				Name:    "list_datasets",
				Summary: "datasets with usage; narrow with pool",
				Method:  "pool.dataset.query",
				Args:    []string{"pool", "limit"},
			},
			{
				Name:     "show_dataset",
				Summary:  "one dataset in full, by name such as tank/media",
				Method:   "pool.dataset.get_instance",
				Args:     []string{"id"},
				Required: []string{"id"},
			},
			{
				Name:    "list_snapshots",
				Summary: "snapshots; narrow with dataset",
				Method:  "pool.snapshot.query",
				Args:    []string{"dataset", "limit"},
			},
		},
	}
}

// System answers "what is this box and is it healthy".
func System() *Concern {
	return &Concern{
		Name:        "system",
		Title:       "System health and alerts",
		Description: "Read the TrueNAS target's identity, health, and alerts.",
		Ops: []Op{
			{
				Name:    "info",
				Summary: "version, hostname, uptime, and hardware",
				Method:  "system.info",
			},
			{
				Name:    "alerts",
				Summary: "current alerts, the first thing to check when something is wrong",
				Method:  "alert.list",
			},
			{
				Name:    "list_services",
				Summary: "services and whether they are running",
				Method:  "service.query",
			},
			{
				Name:    "update_status",
				Summary: "whether a TrueNAS update is available",
				Method:  "update.status",
			},
		},
	}
}

// Apps answers questions about installed applications, including the reads
// that inform a lifecycle decision: what is out of date, what an upgrade would
// change, and what can be rolled back to. Without these a caller must act
// blind, which is why they ship alongside the write tier rather than after it.
func Apps() *Concern {
	return &Concern{
		Name:        "apps",
		Title:       "Installed applications",
		Description: "Read installed applications, their state, and what needs updating.",
		Ops: []Op{
			{
				Name:    "list",
				Summary: "installed apps with their state",
				Method:  "app.query",
			},
			{
				Name:     "show",
				Summary:  "one app in full",
				Method:   "app.get_instance",
				Args:     []string{"name"},
				Required: []string{"name"},
			},
			{
				Name:     "config",
				Summary:  "an app's current configuration",
				Method:   "app.config",
				Args:     []string{"name"},
				Required: []string{"name"},
			},
			{
				Name:     "outdated_images",
				Summary:  "whether one app is running images that are out of date",
				Method:   "app.outdated_docker_images",
				Args:     []string{"name"},
				Required: []string{"name"},
			},
			{
				Name:     "upgrade_summary",
				Summary:  "what upgrading an app would change, before doing it",
				Method:   "app.upgrade_summary",
				Args:     []string{"name"},
				Required: []string{"name"},
			},
			{
				Name:     "rollback_versions",
				Summary:  "which versions an app can be rolled back to",
				Method:   "app.rollback_versions",
				Args:     []string{"name"},
				Required: []string{"name"},
			},
			{
				Name:    "used_ports",
				Summary: "host ports already taken, useful before exposing a new app",
				Method:  "app.used_ports",
			},
			// The closest the middleware gets to logs. TrueNAS 26 exposes no
			// JSON-RPC method returning container log output -- the web UI
			// streams it over a separate channel -- so this returns the
			// container identities a log transport would need, and is useful
			// on its own for "which containers does this app actually run".
			{
				Name:     "containers",
				Summary:  "the containers an app runs, with their service names and IDs",
				Method:   "app.container_ids",
				Args:     []string{"name"},
				Required: []string{"name"},
			},
		},
	}
}

// ReadConcerns is the default read surface.
func ReadConcerns() []*Concern {
	return []*Concern{Storage(), System(), Apps()}
}
