package truenas

import (
	"context"
	"encoding/json"
	"testing"
)

// A client whose connection has died reports that it is no longer usable, so
// a caller can re-establish rather than retrying against a dead socket.
func TestClientReportsDeadConnection(t *testing.T) {
	f := newFakeMiddleware(t)
	f.handle("system.version", func(p json.RawMessage) (any, *rpcError) { return "x", nil })

	c := dial(t, f)
	if !c.Alive() {
		t.Fatal("a fresh connection should be alive")
	}

	_ = c.Close()
	if c.Alive() {
		t.Fatal("a closed connection must not report alive")
	}
}

func TestClientDeadAfterPeerHangsUp(t *testing.T) {
	f := newFakeMiddleware(t)
	f.handle("slow", func(p json.RawMessage) (any, *rpcError) { return nil, nil })
	f.setDropAfter(1)

	c := dial(t, f)
	// The peer closes without replying; the read loop records why.
	_, _ = c.Call(context.Background(), "slow")

	if c.Alive() {
		t.Fatal("a connection whose peer hung up must not report alive")
	}
}
