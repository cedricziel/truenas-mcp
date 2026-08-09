package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// serverInstructionsFor returns what a connecting client is told at initialize.
func serverInstructionsFor(t *testing.T, srv *mcp.Server) string {
	t.Helper()

	httpSrv := httptest.NewServer(mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return srv }, nil))
	t.Cleanup(httpSrv.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(context.Background(),
		&mcp.StreamableClientTransport{Endpoint: httpSrv.URL}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })

	res := sess.InitializeResult()
	if res == nil {
		t.Fatal("no initialize result")
	}
	return res.Instructions
}

// A client that is told nothing at initialize has only the tool list to reason
// from, and the tool list does not say it is partial. That is what sent an
// agent to SSH for filesystem.mkdir and app.image.pull -- both reachable here.
func TestServerReturnsInstructionsAtInitialize(t *testing.T) {
	got := serverInstructionsFor(t, NewMCPServer(MCPConfig{Version: "test"}, stubSession))
	if strings.TrimSpace(got) == "" {
		t.Fatal("server must return instructions at initialize: without them a client's " +
			"only map of this server is a tool list that never says it is a curated subset")
	}
}

// The instructions earn their place by covering what the tool list cannot say
// about itself. Each of these is a specific thing an agent got wrong, not a
// stylistic preference, so each is pinned rather than left to drift.
func TestInstructionsCoverWhatTheToolListCannotSay(t *testing.T) {
	got := serverInstructionsFor(t, NewMCPServer(MCPConfig{Version: "test"}, stubSession))

	for _, want := range []struct {
		needle string
		why    string
	}{
		{"search_methods", "an agent that cannot find a tool must be told how to look for a method"},
		{"describe_method", "finding a method is useless without learning what it takes"},
		{"call_method", "and knowing what it takes is useless without being able to run it"},
		{"server_info", "which box, which build, and whether writes exist decides everything after"},
		{"job", "a mutation returns a job id rather than a result, and a caller must expect that"},
	} {
		if !strings.Contains(got, want.needle) {
			t.Errorf("instructions must mention %q: %s", want.needle, want.why)
		}
	}
}

// Describing the surface differently depending on how the server was started
// would make the text something a reader has to distrust: the same sentence
// would mean different things on two boxes. The text says mutating tools exist
// only when the tier is enabled, which is true either way and is what a caller
// needs in order to read an empty write surface correctly.
func TestInstructionsDoNotVaryWithTheWriteTier(t *testing.T) {
	off := serverInstructionsFor(t, NewMCPServer(MCPConfig{Version: "test"}, stubSession))
	on := serverInstructionsFor(t, NewMCPServer(MCPConfig{Version: "test", EnableWrites: true}, stubSession))

	if off != on {
		t.Error("instructions must not depend on the configured tier: a description that shifts " +
			"with configuration is one a reader cannot take at face value")
	}
}

// The shell paragraph is the whole point of the change, and it is the sentence
// most likely to be trimmed by someone shortening the text later.
func TestInstructionsSteerAwayFromShellingIntoTheTarget(t *testing.T) {
	got := strings.ToLower(serverInstructionsFor(t, NewMCPServer(MCPConfig{Version: "test"}, stubSession)))

	if !strings.Contains(got, "ssh") && !strings.Contains(got, "shell") {
		t.Error("instructions must address shelling into the target: an agent reaches for SSH " +
			"when it believes this surface ended, and nothing else here corrects that belief")
	}
	if !strings.Contains(got, "audit") {
		t.Error("instructions must say middleware calls are audited: it is the concrete reason " +
			"to prefer this API over a shell, rather than a matter of taste")
	}
}
