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
	s, _ := NewStore(filepath.Join(t.TempDir(), "tasks.json"))
	return newMux(s, "")
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
