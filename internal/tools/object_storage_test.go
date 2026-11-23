package tools

import "testing"

func TestObjectStorageRequiresBucketID(t *testing.T) {
	if _, err := ObjectStorage().Resolve("show_bucket", Args{}); err == nil {
		t.Fatal("show_bucket accepted a missing bucket id")
	}
}

func TestObjectStorageRejectsUnrelatedFilter(t *testing.T) {
	if _, err := ObjectStorage().Resolve("list_buckets", Args{"pool": "tank"}); err == nil {
		t.Fatal("list_buckets silently ignored a pool filter")
	}
}
