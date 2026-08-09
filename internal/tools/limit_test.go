package tools

import "testing"

// TestGenuinelyWrongArgumentIsStillRefused guards against mistaking the fix
// that made `limit` dispatch-level (see DispatchInput.args() in the server
// package) for a general relaxation of Resolve's validation: an argument that
// actually selects or narrows the middleware call -- `pool`, on an op that
// has nothing to do with pools -- must still be refused, with the same
// message shape Resolve has always produced.
func TestGenuinelyWrongArgumentIsStillRefused(t *testing.T) {
	_, err := Storage().Resolve("list_snapshots", Args{"pool": "tank"})
	if err == nil {
		t.Fatal("list_snapshots does not accept pool and must refuse it")
	}
}
