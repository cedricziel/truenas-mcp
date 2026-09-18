package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/cedricziel/truenas-mcp/internal/apps"
	"github.com/cedricziel/truenas-mcp/internal/oauth"
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

	// OAuthKeys unseals an OAuth access token presented as a bearer
	// credential -- see CredentialFromRequest. Nil when OAuth is disabled,
	// in which case only a raw TrueNAS API key is accepted, exactly as
	// before this capability existed.
	OAuthKeys *oauth.Keys

	// ProtectedResourceMetadataURL, when non-empty, is named in the
	// WWW-Authenticate challenge on a 401 so an OAuth-capable client can
	// discover how to obtain a credential instead of guessing a header name.
	ProtectedResourceMetadataURL string
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

// serverInstructions is returned at initialize, and is the only thing a client
// reads before it starts choosing tools.
//
// It exists because of a specific failure. An agent working on a target needed
// to create a directory, pull a container image, and read a directory's
// contents; it concluded the middleware modelled none of them and shelled into
// the box over SSH instead. All three are middleware methods -- filesystem.mkdir,
// app.image.pull, filesystem.listdir -- and the last is already a dedicated
// tool. The agent was not being careless: it used the tools it could see, and
// nothing told it that what it could see was a curated fraction of roughly 800
// methods.
//
// So this leads with how to look rather than with an inventory of what exists.
// An inventory would duplicate the tool list, drift from it, and still not
// cover the tail -- and the tail was the whole problem. The paragraph on
// preferring the API over a shell is here for the same reason: an agent that
// reaches for SSH is not choosing it over this surface, it has concluded this
// surface ended.
const serverInstructions = `This server manages one TrueNAS instance through its middleware API.

Call server_info first. It reports which instance you are connected to, which
build is running, and whether mutating tools are exposed at all.

Reads are grouped into concern tools that take an ` + "`op`" + ` argument. Mutations are
separate tools, one per operation, registered only when the write tier is
enabled -- a mutating tool you cannot see is switched off, not hidden.

If no tool covers what you need, search before concluding the capability is
missing. These tools are a curated fraction of roughly 800 middleware methods.
search_methods finds one by name, describe_method shows what it takes, and
call_method runs it. Creating a directory, pulling a container image, reading
an ACL and much else are middleware methods whether or not a tool names them.

Prefer this API over a shell on the target. A middleware call is validated
against the method's schema, bounded by the privileges of the caller's own API
key, and recorded in the target's audit log with that key's identity attached.
A change made over SSH has none of that: it does not appear in the box's own
record of what happened to it.

Operations that take time return a job id immediately rather than blocking.
Follow one with jobs(op="show", job_id=...).

Resources under truenas:// carry documentation the tool descriptions do not
repeat, including the middleware's query filter syntax.`

// NewMCPServer builds the MCP server and registers the tool surface permitted
// by cfg. Tools are registered only when their tier is enabled, so the tool
// list is itself the policy: a disabled tier has no tool to call.
func NewMCPServer(cfg MCPConfig, session sessionFor) *mcp.Server {
	capabilities := &mcp.ServerCapabilities{}
	capabilities.AddExtension(apps.ExtensionID, nil)

	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "truenas-mcp",
		Version: cfg.Version,
	}, &mcp.ServerOptions{
		// Extensions are declared here; everything else -- tools, resources
		// -- is inferred from what gets registered, exactly as before.
		Capabilities: capabilities,
		// Instructions do not vary with the configured tier. Saying "mutations
		// are registered only when enabled" is true and useful in both
		// postures; describing the surface differently depending on how the
		// server was started would make the text something a reader has to
		// distrust.
		Instructions: serverInstructions,
	})

	registerResources(srv, session)
	registerApps(srv, session)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "server_info",
		Description: "Report which TrueNAS instance this server manages, which build " +
			"it is running, and whether mutating tools are enabled.",
		Annotations: readAnnotations("About this server"),
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
			registerConcern(srv, concern, session)
		}
		registerJobs(srv, session)
		registerDiscovery(srv, session, cfg.EnableWrites)

		// The tool list is the policy: with the write tier disabled there is
		// no mutating tool to call, not merely one that refuses.
		if cfg.EnableWrites {
			registerWrites(srv, session)
			registerConfigWrites(srv, session)
		}

		mcp.AddTool(srv, &mcp.Tool{
			Name: "system_info",
			Description: "Get the target TrueNAS system's version, hostname, and uptime. " +
				"Runs under the calling user's own API key.",
			Annotations: readAnnotations("TrueNAS system information"),
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
	apiKey, err := CredentialFromRequest(r, h.cfg.OAuthKeys)
	if err != nil {
		w.Header().Set("WWW-Authenticate", wwwAuthenticateChallenge(h.cfg.ProtectedResourceMetadataURL))
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
	// every tool call that credential makes, and re-established if it dies.
	provider := NewSessionProvider(h.cfg.Sessions, id, apiKey)

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
//
// oauthKeys and resourceMetadataURL are threaded through exactly as they are
// on MCPConfig, and mean the same thing: nil/empty when OAuth is disabled.
func RequireCredential(oauthKeys *oauth.Keys, resourceMetadataURL string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := CredentialFromRequest(r, oauthKeys); err != nil {
			w.Header().Set("WWW-Authenticate", wwwAuthenticateChallenge(resourceMetadataURL))
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// wwwAuthenticateChallenge builds the Bearer challenge for a 401. Naming
// resource_metadata, per RFC 9728, lets an OAuth-capable client discover how
// to obtain a credential instead of guessing a header name; omitted when
// OAuth is disabled, since there is nothing to discover.
func wwwAuthenticateChallenge(resourceMetadataURL string) string {
	if resourceMetadataURL == "" {
		return `Bearer realm="truenas-mcp"`
	}
	return fmt.Sprintf(`Bearer realm="truenas-mcp", resource_metadata=%q`, resourceMetadataURL)
}

// SessionCredentialValidator adapts SessionManager to oauth.CredentialValidator,
// so the authorization endpoint validates a resource owner's submitted
// TrueNAS credential the same way every other credential path in this
// server does -- see SessionManager.Open -- just one step earlier, so a bad
// credential is caught before any client attempts to use it. The probe
// connection is closed immediately: it exists only to prove the credential
// works, not to serve anything.
type SessionCredentialValidator struct {
	Sessions *SessionManager
}

// Validate implements oauth.CredentialValidator.
func (v SessionCredentialValidator) Validate(ctx context.Context, apiKey string) error {
	sess, err := v.Sessions.Open(ctx, apiKey)
	if err != nil {
		return err
	}
	// Login and the version check inside Open already succeeded, so the
	// credential is valid regardless of what happens next -- a failure
	// closing this probe connection (already-severed socket, say) must not
	// be reported as a rejected credential.
	_ = sess.Close()
	return nil
}

// MountOAuth builds the OAuth authorization server and registers its routes
// on mux alongside the MCP handler, or does nothing if issuer is empty
// (OAuth disabled -- see config.Config.OAuthEnabled, whose presence-based
// switch this mirrors).
//
// The two returned values are exactly what MCPConfig.OAuthKeys and
// MCPConfig.ProtectedResourceMetadataURL need: a caller wires them straight
// through so the MCP handler accepts the tokens this authorization server
// issues and its 401 challenge points back at this same server's discovery
// document.
func MountOAuth(
	mux *http.ServeMux,
	sessions *SessionManager,
	issuer string,
	masterKey [32]byte,
	accessTokenTTL, refreshTokenTTL time.Duration,
) (oauthKeys *oauth.Keys, resourceMetadataURL string) {
	if issuer == "" {
		return nil, ""
	}

	keys := oauth.DeriveKeys(masterKey)
	h := oauth.NewHandler(oauth.Config{
		Issuer:          issuer,
		Keys:            keys,
		AccessTokenTTL:  accessTokenTTL,
		RefreshTokenTTL: refreshTokenTTL,
		Validator:       SessionCredentialValidator{Sessions: sessions},
	})
	h.Mount(mux)

	return &keys, h.ProtectedResourceMetadataURL()
}
