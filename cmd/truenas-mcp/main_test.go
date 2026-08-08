package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cedricziel/truenas-mcp/internal/server"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// stubSession stands in for a caller's middleware connection, matching the
// pattern in internal/server/testhelp_test.go. Tools that would reach the
// middleware fail when invoked; these tests exercise the transport plumbing
// end to end, not a live TrueNAS.
func stubSession(context.Context) (*server.Session, error) {
	return nil, errors.New("no middleware connection in tests")
}

// serveStdio must serve a real MCP request end to end over whatever
// transport it is given -- an in-memory pair here, stdin/stdout in
// production -- and return cleanly once the client disconnects.
func TestServeStdioServesToolsEndToEnd(t *testing.T) {
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	errc := make(chan error, 1)
	go func() {
		errc <- serveStdio(context.Background(),
			server.MCPConfig{Version: "test", Target: "nas.local"}, stubSession, serverTransport)
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "server_info",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("call server_info: %v", err)
	}
	if res.IsError {
		t.Fatalf("server_info returned an error: %v", res.Content)
	}
	info, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured content, got %T", res.StructuredContent)
	}
	if info["target"] != "nas.local" {
		t.Errorf("target = %v, want nas.local", info["target"])
	}

	if err := sess.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// The client closing its side ends the session; Run must return a clean
	// nil rather than treating that as a failure.
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("serveStdio returned an error after the client closed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serveStdio did not return after the client closed the connection")
	}
}

// A session-dependent tool call has nowhere to go without a live TrueNAS in
// this test, but the plumbing that reaches it -- transport, dispatch, the
// session provider -- must still work end to end. The stub error surfacing
// as a tool error (rather than the call hanging or panicking) is what proves
// that.
func TestServeStdioSessionToolReachesProvider(t *testing.T) {
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	errc := make(chan error, 1)
	go func() {
		errc <- serveStdio(context.Background(),
			server.MCPConfig{Version: "test", Target: "nas.local"}, stubSession, serverTransport)
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = sess.Close() }()

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "system_info",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("call system_info: %v", err)
	}
	if !res.IsError {
		t.Fatal("system_info should fail without a middleware connection")
	}

	select {
	case <-errc:
		t.Fatal("serveStdio returned before the client closed the connection")
	default:
	}
}

// A signal-driven shutdown cancels the run context; srv.Run reports that as
// context.Canceled, which is an intentional stop, not a failure worth a
// non-zero exit.
func TestShutdownErrTreatsCancellationAsClean(t *testing.T) {
	if err := shutdownErr(context.Canceled); err != nil {
		t.Fatalf("context.Canceled must be treated as a clean shutdown, got %v", err)
	}
	if err := shutdownErr(nil); err != nil {
		t.Fatalf("nil should stay nil, got %v", err)
	}

	boom := errors.New("boom")
	if err := shutdownErr(boom); !errors.Is(err, boom) {
		t.Fatalf("non-cancellation errors must propagate, got %v", err)
	}
}

// --healthcheck probes cfg.Listen, a listener stdio mode never starts, so
// the combination can only ever fail -- and with an error that names the
// wrong problem. Rejecting it outright at the flags gives the operator the
// real reason instead.
func TestFlagConflictErrRejectsStdioAndHealthcheckTogether(t *testing.T) {
	err := flagConflictErr(true, true)
	if err == nil {
		t.Fatal("expected an error when --stdio and --healthcheck are combined")
	}
	if !strings.Contains(err.Error(), "--stdio") || !strings.Contains(err.Error(), "--healthcheck") {
		t.Fatalf("error must name both flags, got: %v", err)
	}
}

func TestFlagConflictErrAllowsEitherFlagAlone(t *testing.T) {
	for _, tc := range []struct {
		name               string
		stdio, healthcheck bool
	}{
		{"neither", false, false},
		{"stdio only", true, false},
		{"healthcheck only", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := flagConflictErr(tc.stdio, tc.healthcheck); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
