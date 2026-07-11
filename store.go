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
	Images      []string  `json:"images,omitempty"` // user-attached reference images (mockups/screenshots) for the implementer
	Note        string    `json:"note"`
	Progress    string    `json:"progress,omitempty"`   // agent-posted current step while in_progress ("running tests")
	DependsOn   []string  `json:"depends_on,omitempty"` // task ids that must be merged before this can be claimed
	Merged      bool      `json:"merged,omitempty"`     // this task's PR landed in the default branch
	Worktree    bool      `json:"worktree,omitempty"`   // run in an isolated git worktree instead of the repo's existing working copy; parallel-safe. Default (unset) runs in place and holds the repo exclusively while in_progress.
	Base        string    `json:"base,omitempty"`       // git ref to branch from instead of the repo's default branch (advisory metadata; the agent runs git, the binary never does)
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// validStatuses is the closed set a task may hold. Status is user-editable
// (board drag-and-drop, `punch update --status`), so the API rejects anything
// else — a typo must never silently drop a card off every column.
var validStatuses = map[Status]bool{
	StatusTodo: true, StatusInProgress: true, StatusInReview: true,
	StatusDone: true, StatusBlocked: true, StatusFailed: true, StatusCancelled: true,
}

func validStatus(s Status) bool { return validStatuses[s] }

// validID is the allowed shape for a task/note id. Import is the only path that
// lets a caller choose ids, and ids are interpolated into the board's HTML/JS, so
// reject anything outside [A-Za-z0-9_-] at that boundary.
func validID(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !(c == '_' || c == '-' ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

// validRef reports whether s is a safe git ref to interpolate into the engineer's
// git command line: non-empty, bounded, no leading '-' (git would read it as a flag),
// and limited to [A-Za-z0-9._/-]. Blocks shell/argument injection through --base.
func validRef(s string) bool {
	if s == "" || len(s) > 200 || s[0] == '-' {
		return false
	}
	for _, r := range s {
		ok := r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' ||
			r == '.' || r == '_' || r == '/' || r == '-'
		if !ok {
			return false
		}
	}
	return true
}

// validationError marks input the store rejects as malformed; HTTP handlers map it to 400
// (via errors.As) rather than the default 500. See validateExecMode.
type validationError struct{ msg string }

func (e *validationError) Error() string { return e.msg }

// validateExecMode enforces the execution-mode invariants EVERY write path shares, so no
// caller can forge a task that skips them: a worktree task needs a repo, and a non-empty
// base must be a safe git ref. Called by Create and Replace — do not re-check in handlers.
func validateExecMode(repo, base string, worktree bool) error {
	if worktree && repo == "" {
		return &validationError{"worktree tasks require a repo"}
	}
	if base != "" && !validRef(base) {
		return &validationError{"invalid base ref"}
	}
	return nil
}

// activeStatuses are the non-terminal statuses a duplicate check considers — a
// task that's done/merged/cancelled/failed is closed, so re-filing it is fine.
var activeStatuses = map[Status]bool{
	StatusTodo: true, StatusInProgress: true, StatusInReview: true, StatusBlocked: true,
}

// normalizeTitle lowercases and collapses whitespace so trivially-different
// titles ("Add  CSV Export" vs "add csv export") compare equal.
func normalizeTitle(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

type TaskInput struct {
	Title       string
	Description string
	Acceptance  string
	Repo        string
	Priority    int
	DependsOn   []string
	Worktree    bool
	Base        string
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

// claimBest flips the single best CLAIMABLE todo task to in_progress and returns
// it (nil if none). A task is claimable only when every task it depends on has
// merged. Caller holds s.mu and is responsible for save().
func (s *Store) claimBest() *Task {
	occ := s.repoOccupancy() // once per call: O(1) lock check per candidate below
	var best *Task
	for _, t := range s.tasks {
		if t.Status != StatusTodo || !s.depsSatisfied(t) || !repoLockOK(t, occ) {
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

// normalizeRepo canonicalizes a repo path for the in-place exclusion check.
// filepath.Clean only — no symlink/stat resolution (the binary stays filesystem-light),
// so callers must file a consistent --repo string for the same repo.
func normalizeRepo(p string) string { return filepath.Clean(p) }

// repoOcc summarizes how in_progress tasks hold one repo.
type repoOcc struct {
	anyInProgress bool // some task is running in this repo
	inPlaceHolder bool // an in-place (non-worktree) task is running in this repo
}

// repoOccupancy builds the per-normalized-repo occupancy of in_progress tasks ONCE, so the
// per-candidate lock check in claimBest is O(1) instead of an O(n) rescan (which made
// claimBest O(n²) and ClaimBatch O(slots·n²)). Caller holds s.mu.
func (s *Store) repoOccupancy() map[string]repoOcc {
	m := map[string]repoOcc{}
	for _, t := range s.tasks {
		if t.Status != StatusInProgress || t.Repo == "" {
			continue
		}
		k := normalizeRepo(t.Repo)
		o := m[k]
		o.anyInProgress = true
		if !t.Worktree {
			o.inPlaceHolder = true
		}
		m[k] = o
	}
	return m
}

// repoLockOK reports whether t may be claimed given the current repo occupancy.
// In-place (the default, Worktree=false) runs in the repo's shared working copy, so it needs
// the repo EXCLUSIVELY — claimable only if nothing else is running there. Worktree tasks
// (Worktree=true) are isolated, so any number run in one repo at once; they only wait on an
// in-place holder. Occupancy is derived from live status, so a lock releases automatically
// when a task leaves in_progress (done/failed/cancelled/swept/rolled-back).
func repoLockOK(t *Task, occ map[string]repoOcc) bool {
	if t.Repo == "" {
		return true // no repo => nothing to isolate, not subject to the repo lock
	}
	o := occ[normalizeRepo(t.Repo)]
	if t.Worktree {
		return !o.inPlaceHolder
	}
	return !o.anyInProgress
}

// depsSatisfied reports whether every task in t.DependsOn exists and has merged.
// An unknown or unmerged dependency blocks the task. Caller holds s.mu.
func (s *Store) depsSatisfied(t *Task) bool {
	for _, id := range t.DependsOn {
		d, ok := s.tasks[id]
		if !ok || !d.Merged {
			return false
		}
	}
	return true
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
	Status   *Status
	PRURL    *string
	Branch   *string
	Note     *string
	Merged   *bool
	Progress *string
}

var errNotFound = fmt.Errorf("task not found")

func (s *Store) Patch(id string, p Patch) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, errNotFound
	}
	old := Task{Status: t.Status, PRURL: t.PRURL, Branch: t.Branch, Note: t.Note, Merged: t.Merged, Progress: t.Progress, UpdatedAt: t.UpdatedAt}
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
	if p.Merged != nil {
		t.Merged = *p.Merged
	}
	if p.Progress != nil {
		t.Progress = *p.Progress
	}
	// progress is the live step of a run — only meaningful while in_progress.
	if t.Status != StatusInProgress {
		t.Progress = ""
	}
	t.UpdatedAt = s.now()
	if err := s.save(); err != nil {
		t.Status, t.PRURL, t.Branch, t.Note, t.Merged, t.Progress, t.UpdatedAt = old.Status, old.PRURL, old.Branch, old.Note, old.Merged, old.Progress, old.UpdatedAt
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

// AttachImage appends a reference-image URL (a mockup/screenshot the user attached as
// input for the implementer) — kept separate from proof-of-work Artifacts.
func (s *Store) AttachImage(id, relURL string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, errNotFound
	}
	t.Images = append(t.Images, relURL)
	t.UpdatedAt = s.now()
	if err := s.save(); err != nil {
		t.Images = t.Images[:len(t.Images)-1]
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

// PendingMerges returns exactly the tasks whose merge status is worth checking:
// `done`, not yet `merged`, with a PR, AND blocking at least one todo task via
// depends_on. This is the minimal set the loop reconciles each tick — usually
// empty, never "all done tasks".
func (s *Store) PendingMerges() []*Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	blocking := map[string]bool{}
	for _, t := range s.tasks {
		if t.Status == StatusTodo {
			for _, id := range t.DependsOn {
				blocking[id] = true
			}
		}
	}
	out := make([]*Task, 0, len(blocking))
	for id := range blocking {
		if d, ok := s.tasks[id]; ok && d.Status == StatusDone && !d.Merged && d.PRURL != "" {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// SimilarActive returns active tasks whose normalized title equals title's —
// the lexical duplicate check for new tasks. Closed tasks are ignored.
func (s *Store) SimilarActive(title string) []*Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	norm := normalizeTitle(title)
	if norm == "" {
		return nil
	}
	var out []*Task
	for _, t := range s.tasks {
		if activeStatuses[t.Status] && normalizeTitle(t.Title) == norm {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *Store) Create(in TaskInput) (*Task, error) {
	if err := validateExecMode(in.Repo, in.Base, in.Worktree); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	t := &Task{
		ID: s.nextID(), Title: in.Title, Description: in.Description,
		Acceptance: in.Acceptance, Repo: in.Repo, Priority: in.Priority,
		DependsOn: in.DependsOn, Worktree: in.Worktree, Base: in.Base,
		Status: StatusTodo, Artifacts: []string{}, CreatedAt: now, UpdatedAt: now,
	}
	s.tasks[t.ID] = t
	if err := s.save(); err != nil {
		delete(s.tasks, t.ID)
		return nil, err
	}
	return t, nil
}

// Empty reports whether the store holds no tasks (used to gate import).
func (s *Store) Empty() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.tasks) == 0
}

// Replace swaps the entire task set for the given tasks and persists atomically.
// Malformed entries (nil or missing id) are skipped. Used by import.
func (s *Store) Replace(tasks []*Task) error {
	for _, t := range tasks { // validate before mutating so a bad bundle changes nothing
		if t == nil {
			continue
		}
		if err := validateExecMode(t.Repo, t.Base, t.Worktree); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.tasks
	s.tasks = make(map[string]*Task, len(tasks))
	for _, t := range tasks {
		if t == nil || t.ID == "" {
			continue
		}
		if t.Artifacts == nil {
			t.Artifacts = []string{}
		}
		s.tasks[t.ID] = t
	}
	if err := s.save(); err != nil {
		s.tasks = old // rollback
		return err
	}
	return nil
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
