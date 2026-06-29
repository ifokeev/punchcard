package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestCmdUpdateFlagsAfterID guards the flag-after-positional bug: `punch update
// <id> --status done` must apply the flags even though the id comes first (Go's
// flag package stops parsing at the first non-flag argument).
func TestCmdUpdateFlagsAfterID(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(filepath.Join(dir, "tasks.json"))
	ms, _ := NewMemoryStore(filepath.Join(dir, "memory.json"))
	cs, _ := NewControlStore(filepath.Join(dir, "control.json"))
	srv := httptest.NewServer(newMux(s, ms, cs, ""))
	defer srv.Close()
	t.Setenv("PUNCH_URL", srv.URL)

	task, _ := s.Create(TaskInput{Title: "x"})
	cmdUpdate([]string{task.ID, "--status", "done", "--pr", "http://p/1", "--branch", "b1"})

	got, _ := s.Get(task.ID)
	if got.Status != StatusDone || got.PRURL != "http://p/1" || got.Branch != "b1" {
		t.Fatalf("flags after id not applied: %+v", got)
	}
}

// TestCmdImportReplaceFlagOrder guards that `punch import <file> --replace` honors
// --replace even though the file comes first.
func TestCmdImportReplaceFlagOrder(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(filepath.Join(dir, "tasks.json"))
	ms, _ := NewMemoryStore(filepath.Join(dir, "memory.json"))
	cs, _ := NewControlStore(filepath.Join(dir, "control.json"))
	s.Create(TaskInput{Title: "existing"}) // makes the board non-empty (import needs --replace)
	srv := httptest.NewServer(newMux(s, ms, cs, ""))
	defer srv.Close()
	t.Setenv("PUNCH_URL", srv.URL)
	t.Setenv("PUNCH_TOKEN", "")
	t.Setenv("PUNCH_PROFILE", "")

	bf := filepath.Join(dir, "bundle.json")
	os.WriteFile(bf, []byte(`{"version":1,"tasks":[{"id":"t_0050","title":"imported","status":"todo"}],"memory":[]}`), 0o644)

	cmdImport([]string{bf, "--replace"}) // file first, flag after

	if _, ok := s.Get("t_0050"); !ok {
		t.Fatalf("import --replace (flag after file) did not load the bundle")
	}
	if _, ok := s.Get("t_0001"); ok {
		t.Fatalf("--replace should have dropped the pre-existing task")
	}
}

func TestResolveServeAddr(t *testing.T) {
	// PORT used only when --addr was not explicitly given (container/PaaS default)
	if got := resolveServeAddr("127.0.0.1:8080", false, "10000"); got != "0.0.0.0:10000" {
		t.Fatalf("PORT fallback: got %q, want 0.0.0.0:10000", got)
	}
	// an explicit --addr always wins over PORT
	if got := resolveServeAddr("127.0.0.1:9000", true, "10000"); got != "127.0.0.1:9000" {
		t.Fatalf("explicit addr should win: got %q", got)
	}
	// no PORT -> keep the flag/default
	if got := resolveServeAddr("127.0.0.1:8080", false, ""); got != "127.0.0.1:8080" {
		t.Fatalf("no PORT: got %q", got)
	}
}

func TestResolveServeToken(t *testing.T) {
	if got := resolveServeToken("flagtok", "envtok"); got != "flagtok" {
		t.Fatalf("flag token should win: got %q", got)
	}
	if got := resolveServeToken("", "envtok"); got != "envtok" {
		t.Fatalf("env fallback: got %q", got)
	}
	if got := resolveServeToken("", ""); got != "" {
		t.Fatalf("no token should stay empty: got %q", got)
	}
}
