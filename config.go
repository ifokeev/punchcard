package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config holds persistent client configuration saved to ~/.punch/config.json.
type Config struct {
	URL   string `json:"url,omitempty"`
	Token string `json:"token,omitempty"`
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
// PUNCH_URL env var > config file > localhost default.
func resolvedURL() string {
	if u := os.Getenv("PUNCH_URL"); u != "" {
		return u
	}
	if u := loadConfig().URL; u != "" {
		return u
	}
	return "http://127.0.0.1:8080"
}

// resolvedToken returns the bearer token with precedence:
// PUNCH_TOKEN env var > config file.
func resolvedToken() string {
	if t := os.Getenv("PUNCH_TOKEN"); t != "" {
		return t
	}
	return loadConfig().Token
}
