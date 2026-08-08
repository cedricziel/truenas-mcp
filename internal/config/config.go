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
)

const (
	// DefaultListen is the bind address used when none is configured.
	DefaultListen = ":8080"

	// APIPath is the middleware's versioned JSON-RPC endpoint. "current"
	// resolves to whatever version the target runs.
	APIPath = "/api/current"
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

	warnings []string
}

// Load reads configuration through getenv and validates it. Passing getenv
// rather than reading the process environment keeps tests free of global state.
func Load(getenv func(string) string) (*Config, error) {
	c := &Config{
		Target:                   strings.TrimSpace(getenv("TRUENAS_MCP_TARGET")),
		Listen:                   strings.TrimSpace(getenv("TRUENAS_MCP_LISTEN")),
		TLSCertFile:              strings.TrimSpace(getenv("TRUENAS_MCP_TLS_CERT")),
		TLSKeyFile:               strings.TrimSpace(getenv("TRUENAS_MCP_TLS_KEY")),
		AllowPlaintext:           boolVar(getenv, "TRUENAS_MCP_ALLOW_PLAINTEXT"),
		TargetInsecureSkipVerify: boolVar(getenv, "TRUENAS_MCP_TARGET_INSECURE"),
		TargetAllowPlaintext:     boolVar(getenv, "TRUENAS_MCP_TARGET_ALLOW_PLAINTEXT"),
		EnableWrites:             boolVar(getenv, "TRUENAS_MCP_ENABLE_WRITES"),
	}

	if c.Listen == "" {
		c.Listen = DefaultListen
	}

	if err := c.validate(); err != nil {
		return nil, err
	}

	c.collectWarnings()
	return c, nil
}

func (c *Config) validate() error {
	if c.Target == "" {
		return fmt.Errorf("TRUENAS_MCP_TARGET is required: set it to the TrueNAS host, for example nas.local or nas.local:8443")
	}
	if err := validateHost(c.Target); err != nil {
		return fmt.Errorf("TRUENAS_MCP_TARGET is not a usable host: %w", err)
	}

	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		return fmt.Errorf("TRUENAS_MCP_TLS_CERT and TRUENAS_MCP_TLS_KEY must be set together")
	}

	// Caller credentials travel on the MCP boundary, so refuse to serve them
	// in the clear unless the operator says so explicitly.
	if !c.TLSEnabled() && !c.AllowPlaintext {
		return fmt.Errorf(
			"refusing to serve MCP without TLS: caller API keys are transmitted on this connection. " +
				"Set TRUENAS_MCP_TLS_CERT and TRUENAS_MCP_TLS_KEY, or set TRUENAS_MCP_ALLOW_PLAINTEXT=true to override")
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
	if !c.TLSEnabled() {
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
// serves, and whether mutation is possible.
func (c *Config) Summary() string {
	tls := "off"
	if c.TLSEnabled() {
		tls = "on"
	}
	writes := "disabled"
	if c.EnableWrites {
		writes = "enabled"
	}
	return fmt.Sprintf("target=%s listen=%s transport=http tls=%s writes=%s",
		c.Target, c.Listen, tls, writes)
}

func boolVar(getenv func(string) string, key string) bool {
	v, err := strconv.ParseBool(strings.TrimSpace(getenv(key)))
	return err == nil && v
}
