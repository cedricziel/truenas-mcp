package tools

// Configuration writes: shares and access control.
//
// This is the work that turns people off TrueNAS — SMB share options, NFS
// export networks, and ACL entries are genuinely hard to get right, and getting
// them wrong is usually a support thread rather than a data loss. That is
// exactly the shape of task worth handing to an assistant.
//
// None of it destroys data. A misconfigured share is disruptive and fixable; a
// deleted share can be recreated. The one genuine hazard lives in an argument
// rather than a method: filesystem.setacl applied recursively rewrites
// permissions across a whole dataset and cannot be undone without knowing what
// every child ACL was. That is denied at the argument level, so the useful form
// of the method stays available.

// ConfigWriteOp is a mutating operation whose payload is a configuration
// object rather than a simple target. Share creation takes a dozen fields; a
// flat argument list would misrepresent it.
type ConfigWriteOp struct {
	Name        string
	Title       string
	Description string
	Method      string

	// Destructive marks operations that interrupt access to data.
	Destructive bool

	// NeedsID is set for updates and deletes, which act on an existing record.
	NeedsID bool

	// NeedsConfig is set when the operation carries a configuration object.
	NeedsConfig bool
}

// ConfigWrites is the share and permission configuration surface.
func ConfigWrites() []ConfigWriteOp {
	return []ConfigWriteOp{
		{
			Name:  "create_smb_share",
			Title: "Create an SMB share",
			Description: "Create an SMB (Windows file sharing) share. Supply the config object " +
				"with at least `path` and `name`. Use describe_method on sharing.smb.create " +
				"to see the available options, and storage(op=\"list_datasets\") to find a path.",
			Method:      "sharing.smb.create",
			NeedsConfig: true,
		},
		{
			Name:  "update_smb_share",
			Title: "Change an SMB share",
			Description: "Change an existing SMB share's configuration. Supply the share id and " +
				"only the fields to change. Clients may briefly lose access while it reloads.",
			Method:      "sharing.smb.update",
			Destructive: true,
			NeedsID:     true,
			NeedsConfig: true,
		},
		{
			Name:  "delete_smb_share",
			Title: "Remove an SMB share",
			Description: "Stop sharing a path over SMB. The data is untouched and the share can " +
				"be recreated, but anyone using it loses access immediately.",
			Method:      "sharing.smb.delete",
			Destructive: true,
			NeedsID:     true,
		},
		{
			Name:  "create_nfs_export",
			Title: "Create an NFS export",
			Description: "Export a path over NFS. Supply the config object with at least `path`. " +
				"Restrict access with `networks` or `hosts`; without them the export is open to " +
				"anyone who can reach the server.",
			Method:      "sharing.nfs.create",
			NeedsConfig: true,
		},
		{
			Name:  "update_nfs_export",
			Title: "Change an NFS export",
			Description: "Change an existing NFS export. Supply the export id and only the fields " +
				"to change. Clients may briefly lose access while it reloads.",
			Method:      "sharing.nfs.update",
			Destructive: true,
			NeedsID:     true,
			NeedsConfig: true,
		},
		{
			Name:  "delete_nfs_export",
			Title: "Remove an NFS export",
			Description: "Stop exporting a path over NFS. The data is untouched, but anyone " +
				"using the export loses access immediately.",
			Method:      "sharing.nfs.delete",
			Destructive: true,
			NeedsID:     true,
		},
		{
			Name:  "set_smb_share_acl",
			Title: "Set who may use an SMB share",
			Description: "Set the share-level ACL, which controls who may connect to an SMB share. " +
				"This is separate from filesystem permissions: both apply, and the more " +
				"restrictive one wins. Supply `share_name` and `share_acl`.",
			Method:      "sharing.smb.setacl",
			Destructive: true,
			NeedsConfig: true,
		},
		{
			Name:  "set_path_acl",
			Title: "Set filesystem permissions on a path",
			Description: "Set the ACL on one path, controlling who may read or write the files " +
				"themselves. Use filesystem(op=\"acl\") to see the current one first. " +
				"Applies to the given path only: recursive application is refused, because it " +
				"rewrites permissions on every child and cannot be undone without knowing what " +
				"they were.",
			Method:      "filesystem.setacl",
			Destructive: true,
			NeedsConfig: true,
		},
	}
}
