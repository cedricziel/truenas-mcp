package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/cedricziel/truenas-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPConfig is the posture the MCP surface is built from. It deliberately
// carries no TrueNAS credential: each session supplies its own, and that
// credential's privilege level is the outer bound on what the session reaches.
type MCPConfig struct {
	Version      string
	Target       string
	EnableWrites bool

	// Sessions opens middleware connections under a caller's own credential.
	// Nil in tests that exercise only the tool surface.
	Sessions *SessionManager
}

// ServerInfoOutput reports what this server is and how it is configured, so a
// caller can tell which box it is talking to and whether mutation is possible
// before doing anything else.
type ServerInfoOutput struct {
	Name          string `json:"name" jsonschema:"the server's name"`
	Version       string `json:"version" jsonschema:"the build this server was made from"`
	Target        string `json:"target" jsonschema:"the TrueNAS instance this server manages"`
	WritesEnabled bool   `json:"writes_enabled" jsonschema:"whether any mutating tool is exposed"`
}

// SystemInfoOutput is a summary of the target, shaped for the question
// "what am I connected to and is it healthy" rather than passing through the
// middleware's full payload.
type SystemInfoOutput struct {
	Version  string `json:"version" jsonschema:"the TrueNAS release running on the target"`
	Hostname string `json:"hostname" jsonschema:"the target's hostname"`
	Uptime   string `json:"uptime,omitempty" jsonschema:"how long the target has been up"`
	Cores    int    `json:"physical_cores,omitempty" jsonschema:"physical CPU cores"`
}

type emptyInput struct{}

// sessionFor is how a tool reaches the middleware under the caller's identity.
type sessionFor func(context.Context) (*Session, error)

// NewMCPServer builds the MCP server and registers the tool surface permitted
// by cfg. Tools are registered only when their tier is enabled, so the tool
// list is itself the policy: a disabled tier has no tool to call.
func NewMCPServer(cfg MCPConfig, session sessionFor) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "truenas-mcp",
		Version: cfg.Version,
	}, nil)

	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}

	mcp.AddTool(srv, &mcp.Tool{
		Name: "server_info",
		Description: "Report which TrueNAS instance this server manages, which build " +
			"it is running, and whether mutating tools are enabled.",
		Annotations: readOnly,
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, ServerInfoOutput, error) {
		return nil, ServerInfoOutput{
			Name:          "truenas-mcp",
			Version:       cfg.Version,
			Target:        cfg.Target,
			WritesEnabled: cfg.EnableWrites,
		}, nil
	})

	if session != nil {
		for _, concern := range tools.ReadConcerns() {
			registerConcern(srv, concern, session, readOnly)
		}
		registerJobs(srv, session)
		registerDiscovery(srv, session, cfg.EnableWrites)

		// The tool list is the policy: with the write tier disabled there is
		// no mutating tool to call, not merely one that refuses.
		if cfg.EnableWrites {
			registerWrites(srv, session)
		}

		mcp.AddTool(srv, &mcp.Tool{
			Name: "system_info",
			Description: "Get the target TrueNAS system's version, hostname, and uptime. " +
				"Runs under the calling user's own API key.",
			Annotations: readOnly,
		}, func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, SystemInfoOutput, error) {
			s, err := session(ctx)
			if err != nil {
				return nil, SystemInfoOutput{}, err
			}

			raw, err := s.Client().Call(ctx, "system.info")
			if err != nil {
				return nil, SystemInfoOutput{}, err
			}

			var info struct {
				Version  string `json:"version"`
				Hostname string `json:"hostname"`
				Uptime   string `json:"uptime"`
				Cores    int    `json:"physical_cores"`
			}
			if err := json.Unmarshal(raw, &info); err != nil {
				return nil, SystemInfoOutput{}, fmt.Errorf("decoding system.info: %w", err)
			}

			return nil, SystemInfoOutput{
				Version:  info.Version,
				Hostname: info.Hostname,
				Uptime:   info.Uptime,
				Cores:    info.Cores,
			}, nil
		})
	}

	return srv
}

// mcpHandler serves MCP over Streamable HTTP, binding each caller to a server
// whose tools run under that caller's own credential.
//
// Servers are cached per credential rather than rebuilt per request, and the
// middleware connection behind them is opened lazily on first use: the target
// rate-limits authentication, so dialling on every HTTP request would spend
// that budget on requests that may never call a tool.
type mcpHandler struct {
	cfg MCPConfig

	mu      sync.Mutex
	servers map[string]*mcp.Server
	http    map[string]http.Handler
}

// NewMCPHandler serves the MCP server over the Streamable HTTP transport.
//
// The transport is HTTP rather than stdio because the server is deployed as a
// container on the appliance it manages, so the client cannot spawn it as a
// child process.
func NewMCPHandler(cfg MCPConfig) http.Handler {
	h := &mcpHandler{
		cfg:     cfg,
		servers: map[string]*mcp.Server{},
		http:    map[string]http.Handler{},
	}

	// Without a session manager the server exposes only tools that need no
	// middleware connection. Used by tests of the tool surface itself.
	if cfg.Sessions == nil {
		return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
			return NewMCPServer(cfg, nil)
		}, nil)
	}

	return h
}

func (h *mcpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	apiKey, err := CredentialFromRequest(r)
	if err != nil {
		w.Header().Set("WWW-Authenticate", `Bearer realm="truenas-mcp"`)
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	h.handlerFor(apiKey).ServeHTTP(w, r)
}

// handlerFor returns the transport handler bound to one credential. The key is
// hashed so the credential itself is never used as a map key or logged.
func (h *mcpHandler) handlerFor(apiKey string) http.Handler {
	id := credentialID(apiKey)

	h.mu.Lock()
	defer h.mu.Unlock()

	if existing, ok := h.http[id]; ok {
		return existing
	}

	// One lazily-established middleware connection per credential, reused for
	// every tool call that credential makes.
	var (
		once    sync.Once
		sess    *Session
		sessErr error
	)
	provider := func(ctx context.Context) (*Session, error) {
		once.Do(func() {
			sess, sessErr = h.cfg.Sessions.Open(ctx, apiKey)
			if sessErr == nil {
				h.cfg.Sessions.Track(id, sess)
			}
		})
		return sess, sessErr
	}

	srv := NewMCPServer(h.cfg, provider)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return srv
	}, nil)

	h.servers[id] = srv
	h.http[id] = handler
	return handler
}

func credentialID(apiKey string) string {
	sum := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(sum[:8])
}

// RequireCredential rejects any request that carries no TrueNAS API key.
//
// It sits in front of the MCP handler so an unauthenticated caller is refused
// at the transport rather than reaching a tool. The server holds no credential
// of its own, so there is nothing to fall back to.
func RequireCredential(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := CredentialFromRequest(r); err != nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="truenas-mcp"`)
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
