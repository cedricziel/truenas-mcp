package truenas

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSupportedVersionAccepted(t *testing.T) {
	f := newFakeMiddleware(t)
	f.handle("system.version", func(json.RawMessage) (any, *rpcError) {
		return "TrueNAS-26.0.0-MASTER+20260614-020151", nil
	})

	c := dial(t, f)
	v, err := c.CheckVersion(context.Background())
	if err != nil {
		t.Fatalf("a supported release should be accepted: %v", err)
	}
	if v.Major != 26 {
		t.Errorf("Major = %d, want 26", v.Major)
	}
}

func TestVersionAtTheFloorAccepted(t *testing.T) {
	f := newFakeMiddleware(t)
	f.handle("system.version", func(json.RawMessage) (any, *rpcError) {
		return "TrueNAS-25.04.0", nil
	})

	c := dial(t, f)
	if _, err := c.CheckVersion(context.Background()); err != nil {
		t.Fatalf("25.04 is the minimum and must be accepted: %v", err)
	}
}

// Before 25.04 the versioned JSON-RPC API is not the supported path, so this
// server cannot work correctly and should say so rather than fail obscurely
// later.
func TestUnsupportedVersionRefused(t *testing.T) {
	f := newFakeMiddleware(t)
	f.handle("system.version", func(json.RawMessage) (any, *rpcError) {
		return "TrueNAS-24.10.2", nil
	})

	c := dial(t, f)
	_, err := c.CheckVersion(context.Background())
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("want ErrUnsupportedVersion, got %v", err)
	}
	// The operator has to know both numbers to act on this.
	if !strings.Contains(err.Error(), "24.10") || !strings.Contains(err.Error(), "25.04") {
		t.Errorf("error must report the detected and minimum versions: %v", err)
	}
}

func TestUnparseableVersionIsNotFatal(t *testing.T) {
	f := newFakeMiddleware(t)
	f.handle("system.version", func(json.RawMessage) (any, *rpcError) {
		return "something-unexpected", nil
	})

	c := dial(t, f)
	// An unrecognised string is more likely a format change than an ancient
	// release; refusing to start would be worse than proceeding.
	if _, err := c.CheckVersion(context.Background()); err != nil {
		t.Fatalf("an unparseable version should not block startup: %v", err)
	}
}
