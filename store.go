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
	StatusCancelled  Status = "cancelled"
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
// It is the raw, ceiling-free primitive; the HTTP layer enforces concurrency
// via ClaimBatch.
func (s *Store) Claim() (*Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.claimBest()
	if t == nil {
		return nil, false
	}
	if err := s.save(); err != nil {
		t.Status = StatusTodo // roll back the flip on flush failure
		return nil, false
	}
	return t, true
}

// claimBest flips the single best todo task to in_progress and returns it
// (nil if none). Caller holds s.mu and is responsible for save().
func (s *Store) claimBest() *Task {
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
		return nil
	}
	best.Status = StatusInProgress
	best.UpdatedAt = s.now()
	return best
}

// ClaimBatch claims up to `want` best todo tasks in priority order, but never
// lets the number of in_progress tasks exceed `ceiling`. That makes concurrency
// a HARD cap on how many tasks run at once — no more than `ceiling` are ever
// in_progress, regardless of how (or how often) the loop calls it.
func (s *Store) ClaimBatch(ceiling, want int) []*Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	inProgress := 0
	for _, t := range s.tasks {
		if t.Status == StatusInProgress {
			inProgress++
		}
	}
	slots := ceiling - inProgress // free concurrency slots right now
	if want < slots {
		slots = want
	}
	var claimed []*Task
	for len(claimed) < slots {
		t := s.claimBest()
		if t == nil {
			break // queue drained
		}
		claimed = append(claimed, t)
	}
	if len(claimed) == 0 {
		return nil
	}
	if err := s.save(); err != nil {
		for _, t := range claimed { // roll back every flip on flush failure
			t.Status = StatusTodo
		}
		return nil
	}
	return claimed
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

type Patch struct {
	Status *Status
	PRURL  *string
	Branch *string
	Note   *string
}

var errNotFound = fmt.Errorf("task not found")

func (s *Store) Patch(id string, p Patch) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, errNotFound
	}
	old := Task{Status: t.Status, PRURL: t.PRURL, Branch: t.Branch, Note: t.Note, UpdatedAt: t.UpdatedAt}
	if p.Status != nil {
		t.Status = *p.Status
	}
	if p.PRURL != nil {
		t.PRURL = *p.PRURL
	}
	if p.Branch != nil {
		t.Branch = *p.Branch
	}
	if p.Note != nil {
		t.Note = *p.Note
	}
	t.UpdatedAt = s.now()
	if err := s.save(); err != nil {
		t.Status, t.PRURL, t.Branch, t.Note, t.UpdatedAt = old.Status, old.PRURL, old.Branch, old.Note, old.UpdatedAt
		return nil, err
	}
	return t, nil
}

func (s *Store) Attach(id, relURL string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, errNotFound
	}
	t.Artifacts = append(t.Artifacts, relURL)
	t.UpdatedAt = s.now()
	if err := s.save(); err != nil {
		t.Artifacts = t.Artifacts[:len(t.Artifacts)-1]
		return nil, err
	}
	return t, nil
}

func (s *Store) List() []*Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return better(out[i], out[j]) })
	return out
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

func (s *Store) DeleteTask(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return errNotFound
	}
	delete(s.tasks, id)
	if err := s.save(); err != nil {
		s.tasks[id] = t // rollback
		return err
	}
	return nil
}

// CancelInProgress flips every in_progress task to cancelled and returns the
// count — the kill-switch for work already running. Owning subagents see the
// status change at their next checkpoint and abort cleanly.
func (s *Store) CancelInProgress() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	saved := map[string]Status{}
	var changed []*Task
	for _, t := range s.tasks {
		if t.Status == StatusInProgress {
			saved[t.ID] = t.Status
			t.Status = StatusCancelled
			t.UpdatedAt = now
			changed = append(changed, t)
		}
	}
	if len(changed) == 0 {
		return 0
	}
	if err := s.save(); err != nil {
		for _, t := range changed {
			t.Status = saved[t.ID]
		}
		return 0
	}
	return len(changed)
}

func (s *Store) SweepStuck(maxAge time.Duration) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	type snapshot struct {
		status    Status
		note      string
		updatedAt time.Time
	}
	var swept []*Task
	saved := map[string]snapshot{}
	for _, t := range s.tasks {
		if t.Status == StatusInProgress && now.Sub(t.UpdatedAt) > maxAge {
			saved[t.ID] = snapshot{t.Status, t.Note, t.UpdatedAt}
			t.Status = StatusFailed
			t.Note = "auto-failed: stuck in_progress with no owning loop (reset with: punch update " + t.ID + " --status todo)"
			t.UpdatedAt = now
			swept = append(swept, t)
		}
	}
	if len(swept) == 0 {
		return 0
	}
	if err := s.save(); err != nil {
		for _, t := range swept {
			snap := saved[t.ID]
			t.Status, t.Note, t.UpdatedAt = snap.status, snap.note, snap.updatedAt
		}
		return 0
	}
	return len(swept)
}
