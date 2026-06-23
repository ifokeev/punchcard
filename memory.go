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

// Note is a durable fact stored in the memory server.
type Note struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Body         string    `json:"body"`
	Repo         string    `json:"repo"`
	Tags         []string  `json:"tags"`
	SupersededBy string    `json:"superseded_by,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// NoteInput carries the fields supplied by the caller when creating a note.
type NoteInput struct {
	Title string
	Body  string
	Repo  string
	Tags  []string
}

// NotePatch carries optional update fields; a nil pointer means "leave unchanged".
type NotePatch struct {
	Title        *string
	Body         *string
	Repo         *string
	Tags         *[]string
	SupersededBy *string
}

// MemoryStore holds notes in memory and persists them atomically to a JSON file.
type MemoryStore struct {
	mu    sync.Mutex
	path  string
	notes map[string]*Note
	now   func() time.Time
}

// NewMemoryStore opens (or creates) the JSON file at path and returns a ready store.
func NewMemoryStore(path string) (*MemoryStore, error) {
	ms := &MemoryStore{path: path, notes: map[string]*Note{}, now: time.Now}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ms, nil
		}
		return nil, err
	}
	var list []*Note
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for _, n := range list {
		ms.notes[n.ID] = n
	}
	return ms, nil
}

// nextID returns m_NNNN one past the current max numeric suffix.
func (ms *MemoryStore) nextID() string {
	max := 0
	for id := range ms.notes {
		n, _ := strconv.Atoi(strings.TrimPrefix(id, "m_"))
		if n > max {
			max = n
		}
	}
	return fmt.Sprintf("m_%04d", max+1)
}

// save marshals all notes atomically. MUST be called with ms.mu held.
func (ms *MemoryStore) save() error {
	list := make([]*Note, 0, len(ms.notes))
	for _, n := range ms.notes {
		list = append(list, n)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(ms.path)
	tmp, err := os.CreateTemp(dir, ".memory-*.json") // same dir => same filesystem => atomic rename
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
	return os.Rename(tmpName, ms.path)
}

var errNoteNotFound = fmt.Errorf("note not found")

// AddNote assigns an id, sets timestamps, and persists the new note.
func (ms *MemoryStore) AddNote(in NoteInput) (*Note, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	now := ms.now()
	tags := in.Tags
	if tags == nil {
		tags = []string{}
	}
	n := &Note{
		ID:        ms.nextID(),
		Title:     in.Title,
		Body:      in.Body,
		Repo:      in.Repo,
		Tags:      tags,
		CreatedAt: now,
		UpdatedAt: now,
	}
	ms.notes[n.ID] = n
	if err := ms.save(); err != nil {
		delete(ms.notes, n.ID)
		return nil, err
	}
	return n, nil
}

// ListNotes returns notes filtered by repo and/or query, sorted newest-first.
//
// Repo filter: if repo != "", include notes whose repo == repo OR whose repo == ""
// (global notes are always included).
// Query filter: if query != "", case-insensitive substring match against title, body,
// and each tag.
func (ms *MemoryStore) ListNotes(repo, query string) []*Note {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	q := strings.ToLower(query)
	out := make([]*Note, 0, len(ms.notes))
	for _, n := range ms.notes {
		// repo filter
		if repo != "" && n.Repo != "" && n.Repo != repo {
			continue
		}
		// query filter
		if q != "" {
			hit := strings.Contains(strings.ToLower(n.Title), q) ||
				strings.Contains(strings.ToLower(n.Body), q)
			if !hit {
				for _, tag := range n.Tags {
					if strings.Contains(strings.ToLower(tag), q) {
						hit = true
						break
					}
				}
			}
			if !hit {
				continue
			}
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].ID > out[j].ID
	})
	return out
}

// GetNote returns the note with the given id, or false if absent.
func (ms *MemoryStore) GetNote(id string) (*Note, bool) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	n, ok := ms.notes[id]
	return n, ok
}

// UpdateNote applies a patch to an existing note and persists it.
func (ms *MemoryStore) UpdateNote(id string, p NotePatch) (*Note, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	n, ok := ms.notes[id]
	if !ok {
		return nil, errNoteNotFound
	}
	old := *n // snapshot for rollback
	if p.Title != nil {
		n.Title = *p.Title
	}
	if p.Body != nil {
		n.Body = *p.Body
	}
	if p.Repo != nil {
		n.Repo = *p.Repo
	}
	if p.Tags != nil {
		n.Tags = *p.Tags
	}
	if p.SupersededBy != nil {
		n.SupersededBy = *p.SupersededBy
	}
	n.UpdatedAt = ms.now()
	if err := ms.save(); err != nil {
		*n = old
		return nil, err
	}
	return n, nil
}

// DeleteNote removes a note and persists the change.
func (ms *MemoryStore) DeleteNote(id string) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	n, ok := ms.notes[id]
	if !ok {
		return errNoteNotFound
	}
	delete(ms.notes, id)
	if err := ms.save(); err != nil {
		ms.notes[id] = n // rollback
		return err
	}
	return nil
}
