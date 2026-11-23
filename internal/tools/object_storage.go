package tools

// ObjectStorage exposes native S3 management reads on targets with the experimental S3 APIs.
func ObjectStorage() *Concern {
	return &Concern{
		Name: "objectstorage", Title: "S3 object storage",
		Description: "Inspect native S3 buckets, access keys, and service configuration. Requires the experimental native S3 APIs, verified on TrueNAS 27.0.0-RC.1.",
		Ops: []Op{
			{Name: "config", Summary: "S3 service configuration", Method: "s3.config"},
			{Name: "bindip_choices", Summary: "available listener addresses", Method: "s3.bindip_choices"},
			{Name: "list_buckets", Summary: "registered S3 buckets", Method: "sharing.s3.query"},
			{Name: "show_bucket", Summary: "one bucket by id", Method: "sharing.s3.get_instance", Args: []string{"id"}, Required: []string{"id"}},
			{Name: "list_access_keys", Summary: "S3 access key metadata without secrets", Method: "s3.accesskey.query"},
			{Name: "show_access_key", Summary: "one access key by id without its secret", Method: "s3.accesskey.get_instance", Args: []string{"id"}, Required: []string{"id"}},
		},
	}
}

func objectStorageWrites() []ConfigWriteOp {
	return []ConfigWriteOp{
		{Name: "update_s3_config", Title: "Configure S3", Description: "Update native S3 service configuration. Listener and region changes restart the service. Use describe_method on s3.update for config fields.", Method: "s3.update", NeedsConfig: true, Destructive: true},
		{Name: "create_s3_bucket", Title: "Create an S3 bucket", Description: "Create a native S3 bucket. Supply name and use describe_method on sharing.s3.create for layout, dataset, ownership and grants.", Method: "sharing.s3.create", NeedsConfig: true},
		{Name: "update_s3_bucket", Title: "Update an S3 bucket", Description: "Update a bucket by id. Grants replace the entire list; changes can restart S3. Use describe_method on sharing.s3.update for config fields.", Method: "sharing.s3.update", NeedsID: true, NeedsConfig: true, Destructive: true},
		{Name: "delete_s3_bucket", Title: "Deregister an S3 bucket", Description: "Deregister a bucket by id. Preserves its dataset and objects but immediately removes S3 access.", Method: "sharing.s3.delete", NeedsID: true, Destructive: true},
		{Name: "create_s3_access_key", Title: "Create an S3 access key", Description: "Create an S3 credential with name and username. Returns the secret; treat the result as sensitive. Use describe_method on s3.accesskey.create for options.", Method: "s3.accesskey.create", NeedsConfig: true},
		{Name: "update_s3_access_key", Title: "Update an S3 access key", Description: "Update an S3 key by id. Set rotate=true to replace its secret, invalidating the old one. The returned new secret is sensitive.", Method: "s3.accesskey.update", NeedsID: true, NeedsConfig: true, Destructive: true},
		{Name: "delete_s3_access_key", Title: "Revoke an S3 access key", Description: "Delete an S3 access key by id, immediately revoking clients using it.", Method: "s3.accesskey.delete", NeedsID: true, Destructive: true},
	}
}
