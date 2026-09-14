// Package config loads and validates the server's configuration.
//
// All configuration arrives as environment variables so the container needs
// no mounted file and no persistent dataset. Loading is strict: an invalid
// configuration refuses to start rather than degrading, because the deployment
// target is an appliance whose app UI reports failures poorly.
//
// Note what is absent. The server holds no TrueNAS credential of its own —
// each MCP session supplies its own username and API key, and that credential's
// privilege level is the outer bound on what the session can reach.
package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cedricziel/truenas-mcp/internal/oauth"
)

const (
	// DefaultListen is the bind address used when none is configured.
	DefaultListen = ":8080"

	// APIPath is the middleware's versioned JSON-RPC endpoint. "current"
	// resolves to whatever version the target runs.
	APIPath = "/api/current"

	// DefaultOAuthAccessTokenTTL and DefaultOAuthRefreshTokenTTL are used
	// when the operator does not configure a TTL explicitly.
	DefaultOAuthAccessTokenTTL  = time.Hour
	DefaultOAuthRefreshTokenTTL = 30 * 24 * time.Hour
)

// Mode selects which transport the server serves on. Validation differs by
// mode: the HTTP transport enforces TLS-or-explicit-plaintext and a bind
// address, neither of which mean anything over stdio, and the credential
// rules invert -- see validate.
type Mode int

const (
	// ModeHTTP serves MCP over Streamable HTTP. The server holds no TrueNAS
	// credential of its own; each caller supplies its own key per request.
	ModeHTTP Mode = iota

	// ModeStdio serves MCP over stdin/stdout. The process is spawned by a
	// single client for a single user, so a server-wide credential is scoped
	// to that user rather than to the server, and is therefore required.
	ModeStdio
)

// Config is the validated server configuration.
type Config struct {
	// Target is the TrueNAS host, optionally with a port.
	Target string

	// Listen is the address the MCP transport binds to.
	Listen string

	// TLSCertFile and TLSKeyFile enable TLS on the MCP boundary.
	TLSCertFile string
	TLSKeyFile  string

	// AllowPlaintext permits serving MCP without TLS. Caller API keys travel
	// on this boundary, so this is an explicit opt-out rather than a default.
	AllowPlaintext bool

	// TargetInsecureSkipVerify accepts an unverified certificate from the
	// target. Deliberately independent of TargetAllowPlaintext: a self-signed
	// certificate must never push an operator onto plaintext, where TrueNAS
	// revokes the API key it observes.
	TargetInsecureSkipVerify bool

	// TargetAllowPlaintext connects to the target without TLS.
	TargetAllowPlaintext bool

	// EnableWrites exposes the mutating tool tier. Off by default.
	EnableWrites bool

	// APIKey is the TrueNAS API key the process authenticates with in stdio
	// mode. It has no meaning in HTTP mode -- see validate -- and is never
	// included in Summary.
	APIKey string

	// OAuthIssuer is the externally-reachable base URL clients see as this
	// server's OAuth issuer. Its presence is what turns the OAuth
	// authorization server on -- see OAuthEnabled -- the same way
	// TLSCertFile/TLSKeyFile's presence, not a separate flag, turns on TLS.
	OAuthIssuer string

	// OAuthEncryptionKey seals every OAuth client registration, authorization
	// code, access token, and refresh token this process issues. Left unset,
	// a random key is generated for the process instead -- see
	// oauthKeyGenerated -- which works, but invalidates every outstanding
	// OAuth value on the next restart.
	OAuthEncryptionKey string

	// OAuthAccessTokenTTL and OAuthRefreshTokenTTL bound how long an issued
	// OAuth token is valid. A zero OAuthRefreshTokenTTL disables refresh
	// token issuance entirely.
	OAuthAccessTokenTTL  time.Duration
	OAuthRefreshTokenTTL time.Duration

	// oauthKey is the resolved 32-byte key backing every value the oauth
	// package seals, decoded from OAuthEncryptionKey or generated fresh when
	// that was unset. Only set when OAuthEnabled and mode is ModeHTTP.
	oauthKey          [32]byte
	oauthKeyGenerated bool

	mode Mode

	// listenSet records whether TRUENAS_MCP_LISTEN was explicitly set, before
	// Load defaults it to DefaultListen. Only used to decide whether it is
	// worth warning about in stdio mode, where it has no effect.
	listenSet bool

	warnings []string
}

// Load reads configuration through getenv and validates it against mode.
// Passing getenv rather than reading the process environment keeps tests
// free of global state.
func Load(getenv func(string) string, mode Mode) (*Config, error) {
	listen := strings.TrimSpace(getenv("TRUENAS_MCP_LISTEN"))

	accessTTL, err := durationVar(getenv, "TRUENAS_MCP_OAUTH_ACCESS_TOKEN_TTL", DefaultOAuthAccessTokenTTL)
	if err != nil {
		return nil, err
	}
	// Unlike OAuthRefreshTokenTTL, zero has no meaning here -- there is no
	// "issue access tokens that don't work" mode -- so it is rejected here
	// rather than accepted and left to silently mint unusable tokens.
	if accessTTL == 0 {
		return nil, fmt.Errorf("TRUENAS_MCP_OAUTH_ACCESS_TOKEN_TTL must be greater than zero")
	}
	refreshTTL, err := durationVar(getenv, "TRUENAS_MCP_OAUTH_REFRESH_TOKEN_TTL", DefaultOAuthRefreshTokenTTL)
	if err != nil {
		return nil, err
	}

	c := &Config{
		Target:                   strings.TrimSpace(getenv("TRUENAS_MCP_TARGET")),
		Listen:                   listen,
		listenSet:                listen != "",
		TLSCertFile:              strings.TrimSpace(getenv("TRUENAS_MCP_TLS_CERT")),
		TLSKeyFile:               strings.TrimSpace(getenv("TRUENAS_MCP_TLS_KEY")),
		AllowPlaintext:           boolVar(getenv, "TRUENAS_MCP_ALLOW_PLAINTEXT"),
		TargetInsecureSkipVerify: boolVar(getenv, "TRUENAS_MCP_TARGET_INSECURE"),
		TargetAllowPlaintext:     boolVar(getenv, "TRUENAS_MCP_TARGET_ALLOW_PLAINTEXT"),
		EnableWrites:             boolVar(getenv, "TRUENAS_MCP_ENABLE_WRITES"),
		APIKey:                   strings.TrimSpace(getenv("TRUENAS_MCP_API_KEY")),
		OAuthIssuer:              strings.TrimSpace(getenv("TRUENAS_MCP_OAUTH_ISSUER")),
		OAuthEncryptionKey:       strings.TrimSpace(getenv("TRUENAS_MCP_OAUTH_ENCRYPTION_KEY")),
		OAuthAccessTokenTTL:      accessTTL,
		OAuthRefreshTokenTTL:     refreshTTL,
		mode:                     mode,
	}

	if c.Listen == "" {
		c.Listen = DefaultListen
	}

	if err := c.validate(); err != nil {
		return nil, err
	}

	// Resolved once per process, not per validation: a fresh key must not be
	// generated more than once, or every value sealed under the first one
	// would stop verifying against the second.
	if c.mode == ModeHTTP && c.OAuthEnabled() {
		key, generated, err := oauth.ResolveMasterKey(c.OAuthEncryptionKey)
		if err != nil {
			return nil, fmt.Errorf("TRUENAS_MCP_OAUTH_ENCRYPTION_KEY is invalid: %w", err)
		}
		c.oauthKey = key
		c.oauthKeyGenerated = generated
	}

	c.collectWarnings()
	return c, nil
}

// OAuthEnabled reports whether the OAuth authorization server is configured.
// The issuer URL's presence, not a separate flag, is the switch -- see
// OAuthIssuer -- but it only ever means anything on the HTTP transport:
// stdio mode never opens the listener OAuth's endpoints would be served on.
func (c *Config) OAuthEnabled() bool {
	return c.mode == ModeHTTP && c.OAuthIssuer != ""
}

// OAuthKey is the resolved 32-byte key backing every OAuth client
// registration, authorization code, access token, and refresh token this
// process issues. Only meaningful when OAuthEnabled and mode is ModeHTTP.
func (c *Config) OAuthKey() [32]byte {
	return c.oauthKey
}

func (c *Config) validate() error {
	if c.Target == "" {
		return fmt.Errorf("TRUENAS_MCP_TARGET is required: set it to the TrueNAS host, for example nas.local or nas.local:8443")
	}
	if err := validateHost(c.Target); err != nil {
		return fmt.Errorf("TRUENAS_MCP_TARGET is not a usable host: %w", err)
	}

	if c.mode == ModeStdio {
		// The process is spawned by one client for one user, so this key is
		// scoped to that user rather than shared across every caller the way
		// a server-wide HTTP credential would be. That is what makes it
		// acceptable here and not over HTTP -- see the check below.
		if c.APIKey == "" {
			return fmt.Errorf(
				"TRUENAS_MCP_API_KEY is required in stdio mode: set it to a TrueNAS API key. " +
					"Create one in the TrueNAS UI under Credentials -> API Keys")
		}
		// TLS-or-plaintext and the bind address govern the HTTP transport's
		// listener, which stdio mode never opens.
		return nil
	}

	// The server holds no TrueNAS credential of its own in HTTP mode: each
	// caller supplies its own key per request, and that credential's
	// privilege level is the outer bound on what the session can reach. A
	// server-wide key here would hollow that design out, so it is refused
	// rather than silently honoured.
	if c.APIKey != "" {
		return fmt.Errorf(
			"TRUENAS_MCP_API_KEY is only meaningful with --stdio: HTTP callers supply their own TrueNAS API key per request")
	}

	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		return fmt.Errorf("TRUENAS_MCP_TLS_CERT and TRUENAS_MCP_TLS_KEY must be set together")
	}

	// The authorization endpoint collects a TrueNAS API key through a
	// browser form on this same boundary, so OAuth refuses the plaintext
	// override outright -- unlike the raw-bearer-key path below, there is no
	// case where an operator already holds the secret and is choosing to
	// transmit it insecurely.
	if c.OAuthEnabled() && c.AllowPlaintext {
		return fmt.Errorf(
			"refusing to enable OAuth with TRUENAS_MCP_ALLOW_PLAINTEXT set: TLS is required whenever " +
				"TRUENAS_MCP_OAUTH_ISSUER is configured, since the authorization endpoint collects a " +
				"TrueNAS API key through a browser form on this same connection")
	}

	// Caller credentials travel on the MCP boundary, so refuse to serve them
	// in the clear unless the operator says so explicitly.
	if !c.TLSEnabled() && !c.AllowPlaintext {
		return fmt.Errorf(
			"refusing to serve MCP without TLS: caller API keys are transmitted on this connection. " +
				"Set TRUENAS_MCP_TLS_CERT and TRUENAS_MCP_TLS_KEY, or set TRUENAS_MCP_ALLOW_PLAINTEXT=true to override")
	}

	if c.OAuthEnabled() {
		// The issuer is what discovery metadata advertises as the
		// authorization/token endpoints' base URL to every OAuth client, so
		// it must itself promise TLS -- independent of, and in addition to,
		// TLSEnabled()/AllowPlaintext above, which only govern what this
		// process actually listens on. A reverse proxy in front of this
		// process could terminate TLS and forward over plaintext, in which
		// case the issuer clients are told about is still the outward-facing
		// https URL; nothing legitimate needs this to be http.
		issuerURL, err := url.ParseRequestURI(c.OAuthIssuer)
		if err != nil || !strings.EqualFold(issuerURL.Scheme, "https") || issuerURL.Host == "" {
			return fmt.Errorf(
				"TRUENAS_MCP_OAUTH_ISSUER is not a valid https URL: got %q", c.OAuthIssuer)
		}
		if c.OAuthEncryptionKey != "" {
			if _, err := oauth.ParseMasterKey(c.OAuthEncryptionKey); err != nil {
				return fmt.Errorf("TRUENAS_MCP_OAUTH_ENCRYPTION_KEY is invalid: %w", err)
			}
		}
	}

	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return fmt.Errorf("TRUENAS_MCP_LISTEN is not a valid bind address: %w", err)
	}

	return nil
}

// validateHost accepts "host" and "host:port" but rejects anything carrying a
// scheme, path, or characters that would not survive URL construction.
func validateHost(target string) error {
	if strings.ContainsAny(target, " \t/\\?#%") {
		return fmt.Errorf("expected a host or host:port, got %q", target)
	}

	host := target
	if h, p, err := net.SplitHostPort(target); err == nil {
		host = h
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("port %q is not in 1-65535", p)
		}
	}
	if host == "" {
		return fmt.Errorf("host is empty")
	}
	if _, err := url.Parse("wss://" + target); err != nil {
		return err
	}
	return nil
}

func (c *Config) collectWarnings() {
	if c.mode == ModeStdio {
		// These variables govern the HTTP transport's listener and MCP-boundary
		// TLS, neither of which stdio mode uses. They are not errors -- unlike
		// TRUENAS_MCP_API_KEY in HTTP mode, setting them does not weaken the
		// credential model -- but an operator who set them likely expects them
		// to matter, so surface it rather than silently ignoring it.
		if c.listenSet {
			c.warnings = append(c.warnings, "TRUENAS_MCP_LISTEN is set but has no effect in stdio mode")
		}
		if c.TLSCertFile != "" || c.TLSKeyFile != "" {
			c.warnings = append(c.warnings, "TRUENAS_MCP_TLS_CERT/TRUENAS_MCP_TLS_KEY are set but have no effect in stdio mode")
		}
		if c.AllowPlaintext {
			c.warnings = append(c.warnings, "TRUENAS_MCP_ALLOW_PLAINTEXT is set but has no effect in stdio mode")
		}
		if c.OAuthIssuer != "" {
			c.warnings = append(c.warnings, "TRUENAS_MCP_OAUTH_ISSUER is set but has no effect in stdio mode")
		}
	} else if !c.TLSEnabled() {
		c.warnings = append(c.warnings,
			"serving MCP over plaintext: caller API keys are transmitted in the clear and TrueNAS may revoke them")
	}
	if c.TargetInsecureSkipVerify {
		c.warnings = append(c.warnings,
			"target certificate verification is disabled: the connection is encrypted but unauthenticated")
	}
	if c.TargetAllowPlaintext {
		c.warnings = append(c.warnings,
			"connecting to the target over plaintext: TrueNAS may revoke API keys it observes in the clear")
	}
	if c.EnableWrites {
		c.warnings = append(c.warnings,
			"write tier is enabled: mutating tools are exposed")
	}
	if c.mode == ModeHTTP && c.OAuthEnabled() && c.oauthKeyGenerated {
		c.warnings = append(c.warnings,
			"TRUENAS_MCP_OAUTH_ENCRYPTION_KEY is not set: a random key was generated for this process. "+
				"Restarting invalidates every outstanding OAuth client registration, authorization code, and "+
				"token (the TrueNAS credentials they wrap are unaffected). Set TRUENAS_MCP_OAUTH_ENCRYPTION_KEY "+
				"to keep OAuth sessions stable across restarts.")
	}
}

// TLSEnabled reports whether the MCP transport serves over TLS.
func (c *Config) TLSEnabled() bool {
	return c.TLSCertFile != "" && c.TLSKeyFile != ""
}

// TargetURL is the middleware WebSocket endpoint to connect to.
func (c *Config) TargetURL() string {
	scheme := "wss"
	if c.TargetAllowPlaintext {
		scheme = "ws"
	}
	return scheme + "://" + c.Target + APIPath
}

// Warnings returns operational warnings worth surfacing at startup. They
// describe a weakened posture the operator opted into, not errors.
func (c *Config) Warnings() []string {
	return c.warnings
}

// Summary is the one-line startup posture report: what it talks to, how it
// serves, and whether mutation is possible. It never includes APIKey: this
// is logged at startup, and the key must not end up in a log line.
func (c *Config) Summary() string {
	writes := "disabled"
	if c.EnableWrites {
		writes = "enabled"
	}
	if c.mode == ModeStdio {
		return fmt.Sprintf("target=%s transport=stdio writes=%s", c.Target, writes)
	}
	tls := "off"
	if c.TLSEnabled() {
		tls = "on"
	}
	oauthStatus := "disabled"
	if c.OAuthEnabled() {
		oauthStatus = "enabled"
	}
	return fmt.Sprintf("target=%s listen=%s transport=http tls=%s writes=%s oauth=%s",
		c.Target, c.Listen, tls, writes, oauthStatus)
}

func boolVar(getenv func(string) string, key string) bool {
	v, err := strconv.ParseBool(strings.TrimSpace(getenv(key)))
	return err == nil && v
}

// durationVar parses key as a Go duration, returning def when unset. An
// empty override behaves like an unset variable rather than an error, so a
// deployment tool that always sets every variable can pass "" for "use the
// default" without this failing to load.
func durationVar(getenv func(string) string, key string, def time.Duration) (time.Duration, error) {
	v := strings.TrimSpace(getenv(key))
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s is not a valid duration: %w", key, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("%s must not be negative", key)
	}
	return d, nil
}
