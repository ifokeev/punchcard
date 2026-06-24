package main

import (
	"os"
	"testing"
)

// TestConfigRoundtrip verifies that saveConfig → loadConfig preserves values
// and that the written file has mode 0600.
func TestConfigRoundtrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Clear env vars so resolution functions don't interfere.
	t.Setenv("PUNCH_URL", "")
	t.Setenv("PUNCH_TOKEN", "")

	want := Config{URL: "http://example.com:9000", Token: "supersecret"}
	if err := saveConfig(want); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	// Check file permissions.
	info, err := os.Stat(configPath())
	if err != nil {
		t.Fatalf("stat config file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 0600", perm)
	}

	got := loadConfig()
	if got.URL != want.URL {
		t.Errorf("URL: got %q, want %q", got.URL, want.URL)
	}
	if got.Token != want.Token {
		t.Errorf("Token: got %q, want %q", got.Token, want.Token)
	}
}

// TestResolutionPrecedenceURL verifies env > config file > default for URL.
func TestResolutionPrecedenceURL(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PUNCH_URL", "")
	t.Setenv("PUNCH_TOKEN", "")

	// With neither env nor config, expect default.
	if got := resolvedURL(); got != "http://127.0.0.1:8080" {
		t.Errorf("default: got %q, want http://127.0.0.1:8080", got)
	}

	// With config file, expect config value.
	if err := saveConfig(Config{URL: "http://from-config:9000"}); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}
	if got := resolvedURL(); got != "http://from-config:9000" {
		t.Errorf("config: got %q, want http://from-config:9000", got)
	}

	// With env set, env wins over config file.
	t.Setenv("PUNCH_URL", "http://env-wins")
	if got := resolvedURL(); got != "http://env-wins" {
		t.Errorf("env: got %q, want http://env-wins", got)
	}
}

// TestResolutionPrecedenceToken verifies env > config file for token.
func TestResolutionPrecedenceToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PUNCH_URL", "")
	t.Setenv("PUNCH_TOKEN", "")

	// No config, no env → empty token.
	if got := resolvedToken(); got != "" {
		t.Errorf("empty: got %q, want empty", got)
	}

	// Config file sets token.
	if err := saveConfig(Config{Token: "file-token"}); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}
	if got := resolvedToken(); got != "file-token" {
		t.Errorf("config: got %q, want file-token", got)
	}

	// Env overrides config file.
	t.Setenv("PUNCH_TOKEN", "env-token")
	if got := resolvedToken(); got != "env-token" {
		t.Errorf("env: got %q, want env-token", got)
	}
}

// TestLoadConfigMissing verifies loadConfig returns zero Config when no file exists.
func TestLoadConfigMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := loadConfig()
	if cfg.URL != "" || cfg.Token != "" {
		t.Errorf("expected zero Config, got %+v", cfg)
	}
}

// TestMaskToken verifies the masking helper.
func TestMaskToken(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"abc123", "set (last 4: c123)"},
		{"abcd", "set (last 4: abcd)"},
		{"ab", "set"},
		{"", "set"},
	}
	for _, c := range cases {
		got := maskToken(c.in)
		if got != c.want {
			t.Errorf("maskToken(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
