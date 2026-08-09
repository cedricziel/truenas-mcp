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
				Filters: []Filter{{Arg: "pool", Field: "pool", Operator: "="}},
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
				Filters: []Filter{{Arg: "dataset", Field: "dataset", Operator: "="}},
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
			{
				Name:    "version",
				Summary: "the exact TrueNAS release running",
				Method:  "system.version",
			},
			{
				Name:    "audit_log",
				Summary: "recent audited actions, for seeing what changed on the system",
				Method:  "audit.query",
				// audit.query rejects a call whose object parameter carries no
				// query-options bound at all -- verified live against TrueNAS
				// 26: no parameters, {}, {"services":[...]}, and even
				// {"query-options":{}} all return -32602 Invalid params, while
				// {"query-options":{"limit":N}} succeeds. describe_method
				// reports zero required parameters for this method, so nothing
				// in the middleware's own introspection says this -- it was
				// found by probing the live target.
				//
				// Options declares the parts of that object that stay constant
				// across every call. The bound itself is deliberately not one
				// of them: server.middlewareParams merges the caller's
				// effective limit into query-options.limit at dispatch time,
				// because that limit has to travel to the target rather than
				// being applied to the response the way shape() bounds every
				// other operation -- the target refuses the call before there
				// is any response to bound. A limit hardcoded here would
				// silently cap a caller who explicitly asked for more.
				//
				// order_by is unconditional rather than caller-controlled for
				// a different reason: without it, audit.query returns the
				// OLDEST records first, backwards for an op whose whole point
				// is "what changed recently".
				Options: map[string]any{
					"query-options": map[string]any{
						"order_by": []string{"-message_timestamp"},
					},
				},
				// An audit record runs to roughly 1,200 characters, most of
				// it a service_data credentials blob repeated verbatim on
				// every row and, on authentication rows, duplicated from
				// event_data. A default page of 50 is therefore about 60KB.
				// That is the apps.list failure again; it stayed invisible
				// only because this op returned -32602 rather than a payload,
				// so fixing the call without projecting it would have traded
				// one bug for another.
				//
				// What survives is the question the summary promises -- when,
				// what, by whom, from where, and whether it worked.
				// event_data stays because it carries the method name and the
				// human-readable description, which is the "what". full=true
				// returns service_data, which is where a caller goes to learn
				// which API key acted rather than merely which user.
				Project: []string{
					"message_timestamp", "event", "username",
					"address", "service", "success", "event_data",
				},
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
				// These answer the three things this concern claims to cover:
				// what is installed, what state it is in, and what needs
				// updating. The last is not optional -- a summary that cannot
				// answer it sends the caller back for the full object to learn
				// what the first call already knew. Everything else app.query
				// returns is detail that show and the lifecycle ops below
				// cover on demand, and returning it by default is what blew a
				// caller's token budget: one app object is roughly 4KB, almost
				// all of it active_workloads.
				Project: []string{"name", "state", "version", "upgrade_available", "image_updates_available"},
			},
			{
				Name:     "show",
				Summary:  "one app in full",
				Method:   "app.get_instance",
				Args:     []string{"name"},
				Required: []string{"name"},
			},
			{
				Name: "config",
				Summary: "an app's current configuration; app.update expects this same object " +
					"back in full as values, not a partial patch",
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
			// TrueNAS exposes no JSON-RPC method returning container log output;
			// logs are available from the app.container_log_follow event source.
			// This still returns the container identities a caller can select.
			{
				Name:     "containers",
				Summary:  "the containers an app runs, with their service names and IDs",
				Method:   "app.container_ids",
				Args:     []string{"name"},
				Required: []string{"name"},
			},
			// Logs are deliberately the one event-source-backed operation rather
			// than a general declaration mechanism: one source is not enough
			// evidence to design an abstraction for every future source. Its empty
			// Method tells the dispatcher to take this explicit path.
			{
				Name:     "logs",
				Summary:  "the bounded tail of one app container's logs, with timestamps",
				Args:     []string{"name", "container"},
				Required: []string{"name"},
			},
			// A moving tag such as :main says nothing about identity, which is
			// what leaves a caller unable to prove which build is running after
			// a deploy. app.image.query is global rather than per-app and takes
			// no name filter, so this op takes no argument; a caller matches a
			// tag from the containers op against repo_tags, and locally only
			// one image holds a given tag at a time, so that match is exact.
			{
				Name: "images",
				Summary: "every image's digest and tags, plus a per-image update_available boolean; " +
					"match a tag from containers against repo_tags to learn exactly which image " +
					"a running container executes. Prefer update_available here over " +
					"outdated_images, whose empty result cannot distinguish \"nothing outdated\" " +
					"from \"could not tell\"",
				Method: "app.image.query",
				// Identity and staleness are the whole question here. The rest
				// is build metadata -- author, comment, created, dangling --
				// which is noise against a collection that grows with every
				// image ever pulled. full=true still yields it.
				Project: []string{"id", "repo_tags", "repo_digests", "update_available"},
			},
		},
	}
}

// Catalog answers "what could I install" -- a separate concern from Apps,
// which answers only "what is installed". The two are genuinely different
// questions with different defaults (installed state matters for one and not
// the other), and a model choosing between two well-named tools does better
// than one choosing between operations on a tool that answers both.
//
// list must project by default, for the same reason apps.list does but an
// order of magnitude more so: app.available returns roughly 400 entries on a
// real target, and a full entry carries an HTML readme, its JSON config
// schema, and its version history. Returning that unprojected is not merely
// wasteful the way an unprojected app.query was -- it is the difference
// between a usable browse and one that cannot be called at all. full=true
// remains the escape hatch, as everywhere else this pattern is used.
//
// list and show both reach app.available, which is query-shaped rather than
// name-addressable: it takes a filter list, not a positional identifier. show
// filters by name under equality; list optionally filters by category under
// membership, because categories is a list-valued field on the target and
// equality would never match it. Both were verified against a live TrueNAS 26
// target: [["categories", "rin", "media"]] returns the media apps, and
// [["name", "=", "plex"]] returns exactly the one entry. This is also why
// show is a filter on app.available rather than catalog.get_app_details,
// which would answer the same question but needs a second object argument
// the flat dispatch shape cannot carry.
func Catalog() *Concern {
	return &Concern{
		Name:  "catalog",
		Title: "Applications available to install",
		Description: "Browse the catalog of applications that can be installed. Distinct " +
			"from apps, which covers only what is already installed on this system.",
		Ops: []Op{
			{
				Name:    "list",
				Summary: "browse the catalog; narrow with category",
				Method:  "app.available",
				Filters: []Filter{{Arg: "category", Field: "categories", Operator: "rin"}},
				// installed earns its place here specifically: without it a
				// caller must cross-reference every candidate against
				// apps(op="list") to avoid proposing something already
				// running. Everything else -- readme, schema, maintainers,
				// icon, full version history -- is what full=true is for.
				Project: []string{
					"name", "title", "categories", "train",
					"latest_version", "latest_app_version", "installed",
				},
			},
			{
				Name:    "categories",
				Summary: "the category vocabulary, so list can be narrowed without guessing a name",
				Method:  "app.categories",
			},
			{
				Name:     "show",
				Summary:  "one catalog entry in full, by name",
				Method:   "app.available",
				Required: []string{"name"},
				Filters:  []Filter{{Arg: "name", Field: "name", Operator: "="}},
			},
		},
	}
}

// Virtualization answers "what else is running on this box" — TrueNAS spreads
// that across vm, container, and lxc as separate namespaces, but to a person
// they are one question.
func Virtualization() *Concern {
	return &Concern{
		Name:        "virtualization",
		Title:       "Virtual machines and system containers",
		Description: "Read virtual machines and system containers on the TrueNAS target.",
		Ops: []Op{
			{Name: "list_vms", Summary: "virtual machines and their state", Method: "vm.query"},
			{
				Name: "show_vm", Summary: "one virtual machine in full",
				Method: "vm.get_instance", Args: []string{"id"}, Required: []string{"id"},
			},
			{Name: "vm_devices", Summary: "devices attached to virtual machines", Method: "vm.device.query"},
			{Name: "list_containers", Summary: "system containers and their state", Method: "container.query"},
			{
				Name: "show_container", Summary: "one system container in full",
				Method: "container.get_instance", Args: []string{"id"}, Required: []string{"id"},
			},
			{Name: "container_devices", Summary: "devices attached to containers", Method: "container.device.query"},
		},
	}
}

// Sharing answers "who can reach my files". SMB, NFS, and web shares have
// almost nothing in common structurally, but they answer one user concern.
func Sharing() *Concern {
	return &Concern{
		Name:        "sharing",
		Title:       "File shares: SMB, NFS, web",
		Description: "Read the shares exposing this system's data, and who can reach them.",
		Ops: []Op{
			{Name: "list_smb", Summary: "SMB shares", Method: "sharing.smb.query"},
			{
				Name: "show_smb", Summary: "one SMB share in full",
				Method: "sharing.smb.get_instance", Args: []string{"id"}, Required: []string{"id"},
			},
			{
				Name: "smb_acl", Summary: "who may access an SMB share",
				Method: "sharing.smb.getacl", Args: []string{"id"}, Required: []string{"id"},
			},
			{Name: "list_nfs", Summary: "NFS exports", Method: "sharing.nfs.query"},
			{
				Name: "show_nfs", Summary: "one NFS export in full",
				Method: "sharing.nfs.get_instance", Args: []string{"id"}, Required: []string{"id"},
			},
			{Name: "list_web", Summary: "web shares", Method: "sharing.webshare.query"},
		},
	}
}

// Backup answers "is my data actually going somewhere else" — one question
// spread across cloud sync, replication, rsync, and periodic snapshot tasks.
func Backup() *Concern {
	return &Concern{
		Name:        "backup",
		Title:       "Backups: cloud sync, replication, rsync, snapshot tasks",
		Description: "Read the tasks that copy this system's data elsewhere.",
		Ops: []Op{
			{Name: "list_cloud_syncs", Summary: "cloud sync tasks and their last result", Method: "cloudsync.query"},
			{
				Name: "show_cloud_sync", Summary: "one cloud sync task in full",
				Method: "cloudsync.get_instance", Args: []string{"id"}, Required: []string{"id"},
			},
			{Name: "cloud_credentials", Summary: "configured cloud credentials, without secrets", Method: "cloudsync.credentials.query"},
			{Name: "list_replications", Summary: "replication tasks and their state", Method: "replication.query"},
			{
				Name: "show_replication", Summary: "one replication task in full",
				Method: "replication.get_instance", Args: []string{"id"}, Required: []string{"id"},
			},
			{Name: "list_rsync_tasks", Summary: "rsync tasks", Method: "rsynctask.query"},
			{Name: "list_snapshot_tasks", Summary: "periodic snapshot tasks and retention", Method: "pool.snapshottask.query"},
		},
	}
}

// Filesystem answers questions about paths themselves rather than the datasets
// containing them: what is here, how big, and who may read it.
func Filesystem() *Concern {
	return &Concern{
		Name:        "filesystem",
		Title:       "Files and permissions",
		Description: "Inspect paths on the TrueNAS target: contents, size, and access control.",
		Ops: []Op{
			{
				Name: "list_directory", Summary: "what is in a path",
				Method: "filesystem.listdir", Args: []string{"path"}, Required: []string{"path"},
			},
			{
				Name: "stat", Summary: "size, timestamps, and ownership of one path",
				Method: "filesystem.stat", Args: []string{"path"}, Required: []string{"path"},
			},
			{
				Name: "space", Summary: "space used and available at a path",
				Method: "filesystem.statfs", Args: []string{"path"}, Required: []string{"path"},
			},
			{
				Name: "acl", Summary: "who may access a path",
				Method: "filesystem.getacl", Args: []string{"path"}, Required: []string{"path"},
			},
		},
	}
}

// ReadConcerns is the default read surface.
//
// Eight concerns rather than the four the design first budgeted. The budget
// existed to protect tool-selection accuracy, and what actually protects that
// is names mapping to distinct domains -- "is my backup running" and "what VMs
// exist" are not questions a model confuses. Folding them into a single
// tool with a thirty-value op enum would have been worse on exactly the axis
// the budget was meant to defend. Catalog is the eighth: "what could I
// install" and "what is installed" (Apps) are exactly that kind of distinct
// domain, not two operations on the same one.
func ReadConcerns() []*Concern {
	return []*Concern{
		Storage(), System(), Apps(), Sharing(),
		Virtualization(), Backup(), Filesystem(), Catalog(),
	}
}
