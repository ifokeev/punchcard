package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Profile is one board's connection settings.
type Profile struct {
	URL   string `json:"url,omitempty"`
	Token string `json:"token,omitempty"`
}

// Config holds persistent client configuration saved to ~/.punch/config.json.
// It supports multiple named profiles (one board each) plus a `current` default.
// The legacy flat url/token are still read and act as a fallback ("default").
type Config struct {
	Current  string             `json:"current,omitempty"`
	Profiles map[string]Profile `json:"profiles,omitempty"`
	// Legacy (pre-profiles) fields — still honored so old config files keep working.
	URL   string `json:"url,omitempty"`
	Token string `json:"token,omitempty"`
}

// activeProfileName resolves which profile is in effect:
// PUNCH_PROFILE env > config `current` > "default".
func activeProfileName(c Config) string {
	if p := os.Getenv("PUNCH_PROFILE"); p != "" {
		return p
	}
	if c.Current != "" {
		return c.Current
	}
	return "default"
}

// migrateLegacy folds pre-profiles flat url/token into a "default" profile so the
// next CLI write produces the new format. In-memory only until saved.
func migrateLegacy(c *Config) {
	if c.URL == "" && c.Token == "" {
		return
	}
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	if _, ok := c.Profiles["default"]; !ok {
		c.Profiles["default"] = Profile{URL: c.URL, Token: c.Token}
	}
	if c.Current == "" {
		c.Current = "default" // keep the existing board active; new profiles won't hijack it
	}
	c.URL, c.Token = "", ""
}

// configPath returns the path to the config file (~/.punch/config.json).
// Returns an empty string if the home directory cannot be resolved.
func configPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".punch", "config.json")
}

// loadConfig reads and parses the config file. On any error (missing file,
// parse failure, unresolvable home) it returns a zero Config — never panics.
func loadConfig() Config {
	path := configPath()
	if path == "" {
		return Config{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}
	}
	return c
}

// saveConfig persists the config to ~/.punch/config.json with mode 0600.
func saveConfig(c Config) error {
	path := configPath()
	if path == "" {
		return os.ErrNotExist
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// resolvedURL returns the server URL with precedence:
// PUNCH_URL env > active profile > legacy flat config > localhost default.
func resolvedURL() string {
	if u := os.Getenv("PUNCH_URL"); u != "" {
		return u
	}
	c := loadConfig()
	if p, ok := c.Profiles[activeProfileName(c)]; ok && p.URL != "" {
		return p.URL
	}
	if c.URL != "" {
		return c.URL
	}
	return "http://127.0.0.1:8080"
}

// resolvedToken returns the bearer token with precedence:
// PUNCH_TOKEN env > active profile (authoritative, even if empty) > legacy flat config.
func resolvedToken() string {
	if t := os.Getenv("PUNCH_TOKEN"); t != "" {
		return t
	}
	c := loadConfig()
	if p, ok := c.Profiles[activeProfileName(c)]; ok {
		return p.Token
	}
	return c.Token
}
