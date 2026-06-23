package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Status string

const (
	StatusTodo       Status = "todo"
	StatusInProgress Status = "in_progress"
	StatusInReview   Status = "in_review"
	StatusDone       Status = "done"
	StatusBlocked    Status = "blocked"
	StatusFailed     Status = "failed"
)

type Task struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Acceptance  string    `json:"acceptance"`
	Repo        string    `json:"repo"`
	Priority    int       `json:"priority"`
	Status      Status    `json:"status"`
	PRURL       string    `json:"pr_url"`
	Branch      string    `json:"branch"`
	Artifacts   []string  `json:"artifacts"`
	Note        string    `json:"note"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type TaskInput struct {
	Title       string
	Description string
	Acceptance  string
	Repo        string
	Priority    int
}

type Store struct {
	mu    sync.Mutex
	path  string
	tasks map[string]*Task
	now   func() time.Time
}

func NewStore(path string) (*Store, error) {
	s := &Store{path: path, tasks: map[string]*Task{}, now: time.Now}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	var list []*Task
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for _, t := range list {
		s.tasks[t.ID] = t
	}
	return s, nil
}

// nextID returns t_NNNN one past the current max numeric suffix.
func (s *Store) nextID() string {
	max := 0
	for id := range s.tasks {
		n, _ := strconv.Atoi(strings.TrimPrefix(id, "t_"))
		if n > max {
			max = n
		}
	}
	return fmt.Sprintf("t_%04d", max+1)
}

// save marshals the whole map atomically. MUST be called with s.mu held.
func (s *Store) save() error {
	list := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		list = append(list, t)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".tasks-*.json") // same dir => same filesystem => atomic rename
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}

func (s *Store) Get(id string) (*Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	return t, ok
}

// Claim atomically selects and flips the next todo task to in_progress.
func (s *Store) Claim() (*Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var best *Task
	for _, t := range s.tasks {
		if t.Status != StatusTodo {
			continue
		}
		if best == nil || better(t, best) {
			best = t
		}
	}
	if best == nil {
		return nil, false
	}
	best.Status = StatusInProgress
	best.UpdatedAt = s.now()
	if err := s.save(); err != nil {
		best.Status = StatusTodo // roll back the flip on flush failure
		return nil, false
	}
	return best, true
}

// better reports whether a should be claimed before b.
func better(a, b *Task) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.Before(b.CreatedAt)
	}
	return a.ID < b.ID
}

func (s *Store) Create(in TaskInput) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	t := &Task{
		ID: s.nextID(), Title: in.Title, Description: in.Description,
		Acceptance: in.Acceptance, Repo: in.Repo, Priority: in.Priority,
		Status: StatusTodo, Artifacts: []string{}, CreatedAt: now, UpdatedAt: now,
	}
	s.tasks[t.ID] = t
	if err := s.save(); err != nil {
		delete(s.tasks, t.ID)
		return nil, err
	}
	return t, nil
}
