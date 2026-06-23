package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	s, _ := NewStore(filepath.Join(dir, "tasks.json"))
	ms, _ := NewMemoryStore(filepath.Join(dir, "memory.json"))
	return newMux(s, ms, "")
}

func TestCreateAndListViaAPI(t *testing.T) {
	h := newTestServer(t)
	body, _ := json.Marshal(map[string]any{"title": "do x", "priority": 3})
	req := httptest.NewRequest("POST", "/api/tasks", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body)
	}
	var created Task
	json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID == "" {
		t.Fatalf("no id returned")
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/tasks", nil))
	var list []Task
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 {
		t.Fatalf("want 1 task, got %d", len(list))
	}
}

func TestNextReturnsIDThenEmpty(t *testing.T) {
	h := newTestServer(t)
	body, _ := json.Marshal(map[string]any{"title": "t", "priority": 1})
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/tasks", bytes.NewReader(body)))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/next", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("next status=%d", rec.Code)
	}
	var claimed Task
	json.Unmarshal(rec.Body.Bytes(), &claimed)
	if claimed.ID == "" || claimed.Status != StatusInProgress {
		t.Fatalf("bad claim: %+v", claimed)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/next", nil))
	if rec.Code != http.StatusNoContent { // drained queue
		t.Fatalf("want 204 on empty, got %d", rec.Code)
	}
}

func TestPatchViaAPI(t *testing.T) {
	h := newTestServer(t)
	body, _ := json.Marshal(map[string]any{"title": "t"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/tasks", bytes.NewReader(body)))
	var created Task
	json.Unmarshal(rec.Body.Bytes(), &created)

	patch, _ := json.Marshal(map[string]any{"status": "done", "pr_url": "u"})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("PATCH", "/api/tasks/"+created.ID, bytes.NewReader(patch)))
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", rec.Code, rec.Body)
	}
	var got Task
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Status != StatusDone || got.PRURL != "u" {
		t.Fatalf("patch not applied: %+v", got)
	}
}

func TestMemoryPostAndSearchViaAPI(t *testing.T) {
	h := newTestServer(t)

	// POST a note
	noteBody, _ := json.Marshal(map[string]any{
		"title": "Go atomic rename pattern",
		"body":  "Use os.CreateTemp in same dir then os.Rename for atomic writes.",
		"repo":  "/myrepo",
		"tags":  []string{"go", "io"},
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/memory", bytes.NewReader(noteBody)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/memory status=%d body=%s", rec.Code, rec.Body)
	}
	var created Note
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal created note: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("no id returned for created note")
	}

	// Search by keyword in body
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/memory?q=atomic", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/memory?q=atomic status=%d", rec.Code)
	}
	var results []Note
	json.Unmarshal(rec.Body.Bytes(), &results)
	if len(results) != 1 {
		t.Fatalf("want 1 search result, got %d", len(results))
	}
	if results[0].ID != created.ID {
		t.Fatalf("wrong note returned: %+v", results[0])
	}

	// GET by id
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/memory/"+created.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/memory/%s status=%d", created.ID, rec.Code)
	}

	// DELETE
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("DELETE", "/api/memory/"+created.ID, nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /api/memory/%s status=%d", created.ID, rec.Code)
	}

	// 404 after delete
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/memory/"+created.ID, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", rec.Code)
	}
}
