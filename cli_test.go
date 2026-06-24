package main

import (
	"net/http/httptest"
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
