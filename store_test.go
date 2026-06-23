package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixedClock() func() time.Time {
	t := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	return func() time.Time { t = t.Add(time.Second); return t }
}

func TestStoreSaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	s.now = fixedClock()
	task, err := s.Create(TaskInput{Title: "do x", Repo: "/tmp/r", Priority: 2})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if task.ID == "" || task.Status != StatusTodo {
		t.Fatalf("bad task: %+v", task)
	}
	// File must exist and reload identically.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("tasks.json not written: %v", err)
	}
	s2, err := NewStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, ok := s2.Get(task.ID)
	if !ok || got.Title != "do x" {
		t.Fatalf("reload mismatch: %+v ok=%v", got, ok)
	}
}
