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
	_, err := Load(env(validEnv(map[string]string{"TRUENAS_MCP_TARGET": ""})))
	if err == nil {
		t.Fatal("expected an error when the target is missing")
	}
	if !strings.Contains(err.Error(), "TRUENAS_MCP_TARGET") {
		t.Fatalf("error must name the missing variable, got: %v", err)
	}
}

func TestLoadRejectsMalformedTarget(t *testing.T) {
	_, err := Load(env(validEnv(map[string]string{"TRUENAS_MCP_TARGET": "http://a b c/%"})))
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
	})))
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
	})))
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
	_, err := Load(env(validEnv(map[string]string{"TRUENAS_MCP_TLS_KEY": ""})))
	if err == nil {
		t.Fatal("expected an error when only one half of the TLS pair is set")
	}
}

func TestDefaults(t *testing.T) {
	cfg, err := Load(env(validEnv(nil)))
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
	cfg, err := Load(env(validEnv(map[string]string{"TRUENAS_MCP_TARGET_INSECURE": "true"})))
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
			cfg, err := Load(env(validEnv(tc.overrides)))
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
	cfg, err := Load(env(validEnv(map[string]string{"TRUENAS_MCP_ENABLE_WRITES": "true"})))
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
	cfg, err := Load(env(validEnv(nil)))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !strings.Contains(cfg.Summary(), "writes=disabled") {
		t.Errorf("summary must report the read-only default, got: %s", cfg.Summary())
	}
}

// Per design D11b the server holds no TrueNAS credential of its own.
// Callers supply their own, so no credential may be configurable here.
func TestNoCredentialIsConfigurable(t *testing.T) {
	cfg, err := Load(env(validEnv(map[string]string{
		"TRUENAS_MCP_API_KEY":  "should-be-ignored",
		"TRUENAS_MCP_USERNAME": "should-be-ignored",
	})))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if strings.Contains(cfg.Summary(), "should-be-ignored") {
		t.Fatal("no credential may be read into configuration")
	}
}
