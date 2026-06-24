package main

import (
	"os"
	"path/filepath"
	"sync"
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

func TestClaimOrdering(t *testing.T) {
	s, _ := NewStore(filepath.Join(t.TempDir(), "tasks.json"))
	s.now = fixedClock()
	low, _ := s.Create(TaskInput{Title: "low", Priority: 1})
	high, _ := s.Create(TaskInput{Title: "high", Priority: 5})
	_, _ = s.Create(TaskInput{Title: "mid", Priority: 1}) // same prio as low, later created

	c1, ok := s.Claim()
	if !ok || c1.ID != high.ID {
		t.Fatalf("want high first, got %+v", c1)
	}
	if c1.Status != StatusInProgress {
		t.Fatalf("claimed task not in_progress: %v", c1.Status)
	}
	c2, _ := s.Claim()
	if c2.ID != low.ID { // FIFO within equal priority: low created before mid
		t.Fatalf("want low (FIFO) second, got %+v", c2)
	}
}

func TestClaimAtomicNoDoubleClaim(t *testing.T) {
	s, _ := NewStore(filepath.Join(t.TempDir(), "tasks.json"))
	for i := 0; i < 50; i++ {
		s.Create(TaskInput{Title: "t", Priority: 1})
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := map[string]int{}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				task, ok := s.Claim()
				if !ok {
					return
				}
				mu.Lock()
				seen[task.ID]++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("task %s claimed %d times (want 1)", id, n)
		}
	}
	if len(seen) != 50 {
		t.Fatalf("claimed %d tasks, want 50", len(seen))
	}
}

func TestPatchAndAttach(t *testing.T) {
	s, _ := NewStore(filepath.Join(t.TempDir(), "tasks.json"))
	s.now = fixedClock()
	task, _ := s.Create(TaskInput{Title: "t"})
	rev := StatusInReview
	pr := "https://github.com/x/y/pull/1"
	got, err := s.Patch(task.ID, Patch{Status: &rev, PRURL: &pr})
	if err != nil || got.Status != StatusInReview || got.PRURL != pr {
		t.Fatalf("patch: %+v err=%v", got, err)
	}
	at, err := s.Attach(task.ID, "/artifacts/"+task.ID+"/demo.gif")
	if err != nil || len(at.Artifacts) != 1 {
		t.Fatalf("attach: %+v err=%v", at, err)
	}
	if _, err := s.Patch("nope", Patch{Status: &rev}); err == nil {
		t.Fatalf("expected error patching missing task")
	}
}

func TestListReturnsCopy(t *testing.T) {
	s, _ := NewStore(filepath.Join(t.TempDir(), "tasks.json"))
	s.Create(TaskInput{Title: "a"})
	s.Create(TaskInput{Title: "b"})
	if len(s.List()) != 2 {
		t.Fatalf("want 2 tasks")
	}
}

func TestDeleteTask(t *testing.T) {
	s, _ := NewStore(filepath.Join(t.TempDir(), "tasks.json"))
	s.now = fixedClock()

	// Create then delete — Get should return false afterwards.
	task, err := s.Create(TaskInput{Title: "to delete"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.DeleteTask(task.ID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	_, ok := s.Get(task.ID)
	if ok {
		t.Fatalf("task should be gone after delete")
	}

	// Deleting a missing id returns errNotFound.
	if err := s.DeleteTask("t_9999"); err == nil {
		t.Fatalf("expected error deleting missing task")
	} else if err != errNotFound {
		t.Fatalf("expected errNotFound, got %v", err)
	}
}

func TestSweepStuck(t *testing.T) {
	s, _ := NewStore(filepath.Join(t.TempDir(), "tasks.json"))
	clk := fixedClock()
	s.now = clk
	task, _ := s.Create(TaskInput{Title: "t"})
	s.Claim() // -> in_progress at an early timestamp
	// advance the clock well past maxAge
	for i := 0; i < 10000; i++ {
		clk()
	}
	n := s.SweepStuck(time.Minute)
	if n != 1 {
		t.Fatalf("want 1 swept, got %d", n)
	}
	got, _ := s.Get(task.ID)
	if got.Status != StatusFailed {
		t.Fatalf("stuck task not failed: %v", got.Status)
	}
}
