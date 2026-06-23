package main

import (
	"path/filepath"
	"testing"
	"time"
)

func fixedMemoryClock() func() time.Time {
	t := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)
	return func() time.Time { t = t.Add(time.Second); return t }
}

func newTestMemoryStore(t *testing.T) *MemoryStore {
	t.Helper()
	ms, err := NewMemoryStore(filepath.Join(t.TempDir(), "memory.json"))
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}
	ms.now = fixedMemoryClock()
	return ms
}

func TestMemoryAddAndGet(t *testing.T) {
	ms := newTestMemoryStore(t)

	n, err := ms.AddNote(NoteInput{
		Title: "Test note",
		Body:  "Some body text",
		Repo:  "/myrepo",
		Tags:  []string{"go", "test"},
	})
	if err != nil {
		t.Fatalf("AddNote: %v", err)
	}
	if n.ID != "m_0001" {
		t.Fatalf("expected id m_0001, got %q", n.ID)
	}
	if n.Title != "Test note" {
		t.Fatalf("title mismatch: %q", n.Title)
	}
	if n.CreatedAt.IsZero() || n.UpdatedAt.IsZero() {
		t.Fatalf("timestamps not set: %+v", n)
	}

	got, ok := ms.GetNote("m_0001")
	if !ok {
		t.Fatalf("GetNote: not found")
	}
	if got.Body != "Some body text" {
		t.Fatalf("body mismatch: %q", got.Body)
	}

	_, ok = ms.GetNote("m_9999")
	if ok {
		t.Fatalf("expected not found for missing id")
	}
}

func TestMemoryListFiltering(t *testing.T) {
	ms := newTestMemoryStore(t)

	// global note (repo == "")
	global, _ := ms.AddNote(NoteInput{Title: "Global tip", Body: "global body", Tags: []string{"global"}})
	// repo-scoped note matching our repo
	scoped, _ := ms.AddNote(NoteInput{Title: "Repo tip", Body: "repo body", Repo: "/myrepo", Tags: []string{"repo"}})
	// another repo note (should be excluded by repo filter)
	other, _ := ms.AddNote(NoteInput{Title: "Other repo tip", Body: "other body", Repo: "/otherrepo", Tags: []string{"other"}})

	// No filters — all 3 returned
	all := ms.ListNotes("", "")
	if len(all) != 3 {
		t.Fatalf("want 3 notes without filter, got %d", len(all))
	}

	// Repo filter: should include global (repo=="") and scoped ("/myrepo"), NOT other
	byRepo := ms.ListNotes("/myrepo", "")
	if len(byRepo) != 2 {
		t.Fatalf("want 2 notes for /myrepo filter, got %d", len(byRepo))
	}
	ids := map[string]bool{}
	for _, n := range byRepo {
		ids[n.ID] = true
	}
	if !ids[global.ID] {
		t.Fatalf("global note missing from repo-filtered list")
	}
	if !ids[scoped.ID] {
		t.Fatalf("scoped note missing from repo-filtered list")
	}
	if ids[other.ID] {
		t.Fatalf("other-repo note should be excluded from repo-filtered list")
	}

	// Query filter: "global" matches only the global note by title/tag
	byQuery := ms.ListNotes("", "global")
	if len(byQuery) != 1 || byQuery[0].ID != global.ID {
		t.Fatalf("query filter 'global' should return 1 note (global), got %d", len(byQuery))
	}

	// Query filter: "repo body" matches scoped note's body
	byBodyQuery := ms.ListNotes("", "repo body")
	if len(byBodyQuery) != 1 || byBodyQuery[0].ID != scoped.ID {
		t.Fatalf("query 'repo body' should match scoped note, got %v", byBodyQuery)
	}

	// Combined: repo + query — global note matches repo filter but not query "repo"
	// scoped note matches both; other excluded by repo filter
	combined := ms.ListNotes("/myrepo", "repo")
	// "repo" matches global note (title "Global tip" — no, but body "global body" — no).
	// Actually "repo" is in "Repo tip" title and "repo body" body and "repo" tag for scoped,
	// and in "global" tag? no. Let's verify: global has title "Global tip", body "global body",
	// tag "global" — none contain "repo". scoped has title "Repo tip" — contains "repo" (case insensitive).
	if len(combined) != 1 || combined[0].ID != scoped.ID {
		t.Fatalf("combined filter should return 1 note (scoped), got %d results", len(combined))
	}

	_ = other // used above
}

func TestMemoryUpdateNote(t *testing.T) {
	ms := newTestMemoryStore(t)

	n, _ := ms.AddNote(NoteInput{Title: "Original", Body: "body", Repo: "/r"})
	originalUpdatedAt := n.UpdatedAt

	newTitle := "Updated"
	newBody := "updated body"
	patched, err := ms.UpdateNote(n.ID, NotePatch{Title: &newTitle, Body: &newBody})
	if err != nil {
		t.Fatalf("UpdateNote: %v", err)
	}
	if patched.Title != "Updated" {
		t.Fatalf("title not updated: %q", patched.Title)
	}
	if patched.Body != "updated body" {
		t.Fatalf("body not updated: %q", patched.Body)
	}
	if patched.Repo != "/r" {
		t.Fatalf("repo should be unchanged: %q", patched.Repo)
	}
	if !patched.UpdatedAt.After(originalUpdatedAt) {
		t.Fatalf("updated_at should advance: was %v, got %v", originalUpdatedAt, patched.UpdatedAt)
	}

	// supersede
	sup := "m_0002"
	patched2, err := ms.UpdateNote(n.ID, NotePatch{SupersededBy: &sup})
	if err != nil {
		t.Fatalf("UpdateNote supersede: %v", err)
	}
	if patched2.SupersededBy != "m_0002" {
		t.Fatalf("superseded_by not set: %q", patched2.SupersededBy)
	}

	// Not-found returns error
	bogus := "nope"
	_, err = ms.UpdateNote("m_9999", NotePatch{Title: &bogus})
	if err == nil {
		t.Fatalf("expected error for missing note")
	}
	if err != errNoteNotFound {
		t.Fatalf("expected errNoteNotFound, got %v", err)
	}
}

func TestMemoryDeleteNote(t *testing.T) {
	ms := newTestMemoryStore(t)

	n, _ := ms.AddNote(NoteInput{Title: "To delete", Body: "bye"})

	if err := ms.DeleteNote(n.ID); err != nil {
		t.Fatalf("DeleteNote: %v", err)
	}

	_, ok := ms.GetNote(n.ID)
	if ok {
		t.Fatalf("note should be gone after delete")
	}

	// delete non-existent
	if err := ms.DeleteNote("m_9999"); err == nil {
		t.Fatalf("expected error deleting missing note")
	} else if err != errNoteNotFound {
		t.Fatalf("expected errNoteNotFound, got %v", err)
	}
}

func TestMemoryStoreRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.json")

	ms, _ := NewMemoryStore(path)
	ms.now = fixedMemoryClock()
	n, err := ms.AddNote(NoteInput{Title: "persist me", Body: "body", Tags: []string{"a"}})
	if err != nil {
		t.Fatalf("AddNote: %v", err)
	}

	ms2, err := NewMemoryStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, ok := ms2.GetNote(n.ID)
	if !ok {
		t.Fatalf("note not found after reload")
	}
	if got.Title != "persist me" || len(got.Tags) != 1 || got.Tags[0] != "a" {
		t.Fatalf("reload mismatch: %+v", got)
	}
}
