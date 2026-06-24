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
	cs, _ := NewControlStore(filepath.Join(dir, "control.json"))
	return newMux(s, ms, cs, "")
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

func TestControlAndPauseViaAPI(t *testing.T) {
	h := newTestServer(t)

	// Default control: not paused, concurrency 3.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/control", nil))
	var ctl Control
	json.Unmarshal(rec.Body.Bytes(), &ctl)
	if ctl.Paused || ctl.Concurrency != 3 {
		t.Fatalf("default control = %+v, want {false 3}", ctl)
	}

	// Pause: claiming must report 423, not hand out work.
	body, _ := json.Marshal(map[string]any{"title": "t"})
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/tasks", bytes.NewReader(body)))

	patch, _ := json.Marshal(map[string]any{"paused": true})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("PATCH", "/api/control", bytes.NewReader(patch)))
	if rec.Code != http.StatusOK {
		t.Fatalf("pause patch status=%d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/next", nil))
	if rec.Code != http.StatusLocked {
		t.Fatalf("paused next: want 423, got %d", rec.Code)
	}

	// Resume: the queued task is now claimable.
	patch, _ = json.Marshal(map[string]any{"paused": false})
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("PATCH", "/api/control", bytes.NewReader(patch)))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/next", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("resumed next: want 200, got %d", rec.Code)
	}

	// Concurrency clamps to >=1.
	patch, _ = json.Marshal(map[string]any{"concurrency": 0})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("PATCH", "/api/control", bytes.NewReader(patch)))
	json.Unmarshal(rec.Body.Bytes(), &ctl)
	if ctl.Concurrency != 1 {
		t.Fatalf("concurrency clamp: want 1, got %d", ctl.Concurrency)
	}
}

func TestBatchClaimViaAPI(t *testing.T) {
	h := newTestServer(t)
	// concurrency 2, three todos => batch returns exactly 2.
	patch, _ := json.Marshal(map[string]any{"concurrency": 2})
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("PATCH", "/api/control", bytes.NewReader(patch)))
	for i := 0; i < 3; i++ {
		body, _ := json.Marshal(map[string]any{"title": "t", "priority": i})
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/tasks", bytes.NewReader(body)))
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/next?batch=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("batch status=%d", rec.Code)
	}
	var batch []Task
	json.Unmarshal(rec.Body.Bytes(), &batch)
	if len(batch) != 2 {
		t.Fatalf("batch size = %d, want 2", len(batch))
	}
	if batch[0].Priority < batch[1].Priority {
		t.Fatalf("batch not in priority order: %+v", batch)
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

func TestPatchRejectsInvalidStatus(t *testing.T) {
	h := newTestServer(t)
	body, _ := json.Marshal(map[string]any{"title": "t"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/tasks", bytes.NewReader(body)))
	var created Task
	json.Unmarshal(rec.Body.Bytes(), &created)

	// Bogus status -> 400, task unchanged.
	bad, _ := json.Marshal(map[string]any{"status": "shipped"})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("PATCH", "/api/tasks/"+created.ID, bytes.NewReader(bad)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid status: want 400, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/tasks/"+created.ID, nil))
	var got Task
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Status != StatusTodo {
		t.Fatalf("status changed despite 400: %v", got.Status)
	}

	// A valid status still works.
	ok, _ := json.Marshal(map[string]any{"status": "in_review"})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("PATCH", "/api/tasks/"+created.ID, bytes.NewReader(ok)))
	if rec.Code != http.StatusOK {
		t.Fatalf("valid status: want 200, got %d", rec.Code)
	}
}

func TestDeleteTaskViaAPI(t *testing.T) {
	h := newTestServer(t)

	// Create a task.
	body, _ := json.Marshal(map[string]any{"title": "to delete"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/tasks", bytes.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body)
	}
	var created Task
	json.Unmarshal(rec.Body.Bytes(), &created)

	// DELETE /api/tasks/{id} → 204 No Content.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("DELETE", "/api/tasks/"+created.ID, nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status=%d body=%s", rec.Code, rec.Body)
	}

	// GET /api/tasks/{id} → 404 after delete.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/tasks/"+created.ID, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", rec.Code)
	}

	// DELETE a bogus id → 404.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("DELETE", "/api/tasks/t_9999", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for bogus delete, got %d", rec.Code)
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
