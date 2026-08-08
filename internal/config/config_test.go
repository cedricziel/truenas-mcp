package config

import (
	"strings"
	"testing"
)

// env builds a getenv func from a map, so tests never touch process state.
func env(pairs map[string]string) func(string) string {
	return func(k string) string { return pairs[k] }
}

func validEnv(overrides map[string]string) map[string]string {
	base := map[string]string{
		"TRUENAS_MCP_TARGET":          "nas.local",
		"TRUENAS_MCP_TLS_CERT":        "/tls/cert.pem",
		"TRUENAS_MCP_TLS_KEY":         "/tls/key.pem",
		"TRUENAS_MCP_TARGET_INSECURE": "",
	}
	for k, v := range overrides {
		if v == "" {
			delete(base, k)
			continue
		}
		base[k] = v
	}
	return base
}

func TestLoadRequiresTarget(t *testing.T) {
	_, err := Load(env(validEnv(map[string]string{"TRUENAS_MCP_TARGET": ""})), ModeHTTP)
	if err == nil {
		t.Fatal("expected an error when the target is missing")
	}
	if !strings.Contains(err.Error(), "TRUENAS_MCP_TARGET") {
		t.Fatalf("error must name the missing variable, got: %v", err)
	}
}

func TestLoadRejectsMalformedTarget(t *testing.T) {
	_, err := Load(env(validEnv(map[string]string{"TRUENAS_MCP_TARGET": "http://a b c/%"})), ModeHTTP)
	if err == nil {
		t.Fatal("expected an error for a malformed target")
	}
	if !strings.Contains(err.Error(), "TRUENAS_MCP_TARGET") {
		t.Fatalf("error must name the offending variable, got: %v", err)
	}
}

// The MCP boundary carries caller API keys, so TLS is required unless the
// operator explicitly opts out. See design D11d.
func TestLoadRefusesPlaintextWithoutOverride(t *testing.T) {
	_, err := Load(env(validEnv(map[string]string{
		"TRUENAS_MCP_TLS_CERT": "",
		"TRUENAS_MCP_TLS_KEY":  "",
	})), ModeHTTP)
	if err == nil {
		t.Fatal("expected a refusal to serve without TLS and without the override")
	}
	if !strings.Contains(err.Error(), "TRUENAS_MCP_ALLOW_PLAINTEXT") {
		t.Fatalf("error must name the override that permits this, got: %v", err)
	}
}

func TestLoadAllowsPlaintextWithOverride(t *testing.T) {
	cfg, err := Load(env(validEnv(map[string]string{
		"TRUENAS_MCP_TLS_CERT":        "",
		"TRUENAS_MCP_TLS_KEY":         "",
		"TRUENAS_MCP_ALLOW_PLAINTEXT": "true",
	})), ModeHTTP)
	if err != nil {
		t.Fatalf("override should permit plaintext: %v", err)
	}
	if cfg.TLSEnabled() {
		t.Fatal("TLS must not be reported as enabled")
	}
	if len(cfg.Warnings()) == 0 {
		t.Fatal("serving plaintext must produce a warning")
	}
}

func TestLoadRequiresBothTLSFiles(t *testing.T) {
	_, err := Load(env(validEnv(map[string]string{"TRUENAS_MCP_TLS_KEY": ""})), ModeHTTP)
	if err == nil {
		t.Fatal("expected an error when only one half of the TLS pair is set")
	}
}

func TestDefaults(t *testing.T) {
	cfg, err := Load(env(validEnv(nil)), ModeHTTP)
	if err != nil {
		t.Fatalf("valid config should load: %v", err)
	}
	if cfg.Listen != DefaultListen {
		t.Fatalf("Listen = %q, want %q", cfg.Listen, DefaultListen)
	}
	if cfg.EnableWrites {
		t.Fatal("the write tier must be off by default")
	}
	if cfg.TargetInsecureSkipVerify {
		t.Fatal("certificate verification must be on by default")
	}
	if cfg.TargetAllowPlaintext {
		t.Fatal("plaintext to the target must be off by default")
	}
}

// Certificate verification and transport scheme are independent knobs:
// a self-signed certificate must not push an operator onto plaintext,
// where TrueNAS revokes the API key. See design D11c and the connection spec.
func TestTargetInsecureIsSeparateFromPlaintext(t *testing.T) {
	cfg, err := Load(env(validEnv(map[string]string{"TRUENAS_MCP_TARGET_INSECURE": "true"})), ModeHTTP)
	if err != nil {
		t.Fatalf("relaxing certificate verification should load: %v", err)
	}
	if !cfg.TargetInsecureSkipVerify {
		t.Fatal("TargetInsecureSkipVerify should be set")
	}
	if cfg.TargetAllowPlaintext {
		t.Fatal("relaxing verification must not imply plaintext")
	}
	if len(cfg.Warnings()) == 0 {
		t.Fatal("unverified certificates must produce a warning")
	}
}

func TestTargetURLScheme(t *testing.T) {
	for _, tc := range []struct {
		name      string
		overrides map[string]string
		want      string
	}{
		{"bare host becomes wss", nil, "wss://nas.local/api/current"},
		{
			"plaintext when explicitly allowed",
			map[string]string{"TRUENAS_MCP_TARGET_ALLOW_PLAINTEXT": "true"},
			"ws://nas.local/api/current",
		},
		{
			"explicit host and port preserved",
			map[string]string{"TRUENAS_MCP_TARGET": "nas.local:8443"},
			"wss://nas.local:8443/api/current",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Load(env(validEnv(tc.overrides)), ModeHTTP)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if got := cfg.TargetURL(); got != tc.want {
				t.Fatalf("TargetURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The startup summary is what an operator reads to confirm the server is
// configured the way they intended. See the container-deployment spec.
func TestSummaryReportsPosture(t *testing.T) {
	cfg, err := Load(env(validEnv(map[string]string{"TRUENAS_MCP_ENABLE_WRITES": "true"})), ModeHTTP)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	s := cfg.Summary()
	for _, want := range []string{"nas.local", "tls=on", "writes=enabled"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary must contain %q, got: %s", want, s)
		}
	}
}

func TestSummaryReportsReadOnlyDefault(t *testing.T) {
	cfg, err := Load(env(validEnv(nil)), ModeHTTP)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !strings.Contains(cfg.Summary(), "writes=disabled") {
		t.Errorf("summary must report the read-only default, got: %s", cfg.Summary())
	}
}

// Per design D11b the server holds no TrueNAS credential of its own in HTTP
// mode. Callers supply their own, so no credential may be configurable here.
// TRUENAS_MCP_API_KEY is covered separately below: unlike TRUENAS_MCP_USERNAME
// it is a real, meaningful variable (for stdio mode), so setting it in HTTP
// mode is now a startup error rather than a silently ignored value -- see
// TestLoadRejectsAPIKeyInHTTPMode.
func TestNoCredentialIsConfigurable(t *testing.T) {
	cfg, err := Load(env(validEnv(map[string]string{
		"TRUENAS_MCP_USERNAME": "should-be-ignored",
	})), ModeHTTP)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if strings.Contains(cfg.Summary(), "should-be-ignored") {
		t.Fatal("no credential may be read into configuration")
	}
}

// TRUENAS_MCP_API_KEY is only meaningful with --stdio: a server-wide key in
// HTTP mode would hollow out the design that each caller supplies its own.
func TestLoadRejectsAPIKeyInHTTPMode(t *testing.T) {
	_, err := Load(env(validEnv(map[string]string{
		"TRUENAS_MCP_API_KEY": "1-somekey",
	})), ModeHTTP)
	if err == nil {
		t.Fatal("expected an error when TRUENAS_MCP_API_KEY is set in HTTP mode")
	}
	if !strings.Contains(err.Error(), "TRUENAS_MCP_API_KEY") || !strings.Contains(err.Error(), "--stdio") {
		t.Fatalf("error must name the variable and point at --stdio, got: %v", err)
	}
}

// Over stdio the process is spawned by one user's client, so a single key is
// per-user rather than server-wide -- unlike HTTP, where it would be shared
// across every caller.
func TestLoadRequiresAPIKeyInStdioMode(t *testing.T) {
	_, err := Load(env(map[string]string{"TRUENAS_MCP_TARGET": "nas.local"}), ModeStdio)
	if err == nil {
		t.Fatal("expected an error when TRUENAS_MCP_API_KEY is missing in stdio mode")
	}
	if !strings.Contains(err.Error(), "TRUENAS_MCP_API_KEY") {
		t.Fatalf("error must name the missing variable, got: %v", err)
	}
}

func TestLoadAcceptsAPIKeyInStdioMode(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"TRUENAS_MCP_TARGET":  "nas.local",
		"TRUENAS_MCP_API_KEY": "1-somekey",
	}), ModeStdio)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.APIKey != "1-somekey" {
		t.Fatalf("APIKey = %q, want 1-somekey", cfg.APIKey)
	}
}

// TRUENAS_MCP_TARGET is the middleware endpoint, needed regardless of which
// transport the server speaks MCP over.
func TestStdioModeStillRequiresTarget(t *testing.T) {
	_, err := Load(env(map[string]string{"TRUENAS_MCP_API_KEY": "1-somekey"}), ModeStdio)
	if err == nil {
		t.Fatal("expected an error when the target is missing in stdio mode")
	}
	if !strings.Contains(err.Error(), "TRUENAS_MCP_TARGET") {
		t.Fatalf("error must name the missing variable, got: %v", err)
	}
}

// Stdio mode has no bind address and no MCP-boundary TLS to enforce: the
// process talks to its one client over stdin/stdout, not a TCP listener.
func TestStdioModeSkipsHTTPOnlyValidation(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"TRUENAS_MCP_TARGET":  "nas.local",
		"TRUENAS_MCP_API_KEY": "1-somekey",
		"TRUENAS_MCP_LISTEN":  "not a valid address",
	}), ModeStdio)
	if err != nil {
		t.Fatalf("stdio mode must not validate the bind address: %v", err)
	}
	if len(cfg.Warnings()) == 0 {
		t.Error("setting an HTTP-only variable in stdio mode should still be surfaced as a warning")
	}
}

// Target-side settings (verification, plaintext, writes) apply to the
// middleware connection regardless of which transport serves MCP, so they
// keep working unchanged in stdio mode.
func TestStdioModeAppliesTargetSettings(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"TRUENAS_MCP_TARGET":                 "nas.local",
		"TRUENAS_MCP_API_KEY":                "1-somekey",
		"TRUENAS_MCP_TARGET_INSECURE":        "true",
		"TRUENAS_MCP_TARGET_ALLOW_PLAINTEXT": "true",
		"TRUENAS_MCP_ENABLE_WRITES":          "true",
	}), ModeStdio)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.TargetInsecureSkipVerify || !cfg.TargetAllowPlaintext || !cfg.EnableWrites {
		t.Fatalf("target settings did not carry through in stdio mode: %+v", cfg)
	}
}

// Settings that only mean something on the HTTP transport are worth a
// warning in stdio mode rather than a hard error or silent acceptance: an
// operator who set them likely expects them to matter, and a warning is
// how this project already surfaces a weakened or unexpected posture (see
// Warnings) without refusing to start over something recoverable.
func TestStdioModeWarnsOnHTTPOnlySettings(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
	}{
		{"listen", map[string]string{"TRUENAS_MCP_LISTEN": ":9090"}},
		{"tls cert and key", map[string]string{"TRUENAS_MCP_TLS_CERT": "/tls/cert.pem", "TRUENAS_MCP_TLS_KEY": "/tls/key.pem"}},
		{"allow plaintext", map[string]string{"TRUENAS_MCP_ALLOW_PLAINTEXT": "true"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := map[string]string{
				"TRUENAS_MCP_TARGET":  "nas.local",
				"TRUENAS_MCP_API_KEY": "1-somekey",
			}
			for k, v := range tc.env {
				base[k] = v
			}
			cfg, err := Load(env(base), ModeStdio)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if len(cfg.Warnings()) == 0 {
				t.Error("expected a warning for an HTTP-only setting in stdio mode")
			}
		})
	}
}

// The API key is a startup-only secret the process holds for the lifetime of
// the connection. It must never leak into the log line that reports it.
func TestSummaryOmitsAPIKeyInStdioMode(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"TRUENAS_MCP_TARGET":  "nas.local",
		"TRUENAS_MCP_API_KEY": "1-verysecretkey",
	}), ModeStdio)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if strings.Contains(cfg.Summary(), "1-verysecretkey") {
		t.Fatal("Summary must never include the API key")
	}
	if !strings.Contains(cfg.Summary(), "transport=stdio") {
		t.Errorf("summary should report the stdio transport, got: %s", cfg.Summary())
	}
}
