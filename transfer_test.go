package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func postJSON(t *testing.T, h http.Handler, path string, payload any) {
	t.Helper()
	body, _ := json.Marshal(payload)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", path, bytes.NewReader(body)))
	if rec.Code >= 300 {
		t.Fatalf("POST %s status=%d body=%s", path, rec.Code, rec.Body)
	}
}

// TestExportImportRoundTrip seeds one board, exports it, imports into a fresh
// board, and verifies tasks (incl depends_on + merged) and memory survive — plus
// the non-empty guard and --replace path.
func TestExportImportRoundTrip(t *testing.T) {
	src := newTestServer(t)
	postJSON(t, src, "/api/tasks", map[string]any{"title": "A"})
	postJSON(t, src, "/api/tasks", map[string]any{"title": "B", "depends_on": []string{"t_0001"}})
	patch, _ := json.Marshal(map[string]any{"status": "done", "merged": true, "pr_url": "http://pr/1"})
	rec := httptest.NewRecorder()
	src.ServeHTTP(rec, httptest.NewRequest("PATCH", "/api/tasks/t_0001", bytes.NewReader(patch)))
	postJSON(t, src, "/api/memory", map[string]any{"title": "conv", "body": "use tabs"})

	// Export.
	rec = httptest.NewRecorder()
	src.ServeHTTP(rec, httptest.NewRequest("GET", "/api/export", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("export status=%d", rec.Code)
	}
	bundle := rec.Body.Bytes()

	// Import into a fresh (empty) board — no replace needed.
	dst := newTestServer(t)
	rec = httptest.NewRecorder()
	dst.ServeHTTP(rec, httptest.NewRequest("POST", "/api/import", bytes.NewReader(bundle)))
	if rec.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", rec.Code, rec.Body)
	}

	// Tasks survived with state.
	rec = httptest.NewRecorder()
	dst.ServeHTTP(rec, httptest.NewRequest("GET", "/api/tasks", nil))
	var tasks []Task
	json.Unmarshal(rec.Body.Bytes(), &tasks)
	if len(tasks) != 2 {
		t.Fatalf("want 2 tasks after import, got %d", len(tasks))
	}
	byID := map[string]Task{}
	for _, tk := range tasks {
		byID[tk.ID] = tk
	}
	if a := byID["t_0001"]; a.Status != StatusDone || !a.Merged {
		t.Fatalf("A not preserved: %+v", a)
	}
	if b := byID["t_0002"]; len(b.DependsOn) != 1 || b.DependsOn[0] != "t_0001" {
		t.Fatalf("B depends_on not preserved: %+v", b)
	}

	// Memory survived.
	rec = httptest.NewRecorder()
	dst.ServeHTTP(rec, httptest.NewRequest("GET", "/api/memory", nil))
	var notes []Note
	json.Unmarshal(rec.Body.Bytes(), &notes)
	if len(notes) != 1 || notes[0].Title != "conv" {
		t.Fatalf("memory not preserved: %+v", notes)
	}

	// Re-import into a non-empty board without replace → 409.
	rec = httptest.NewRecorder()
	dst.ServeHTTP(rec, httptest.NewRequest("POST", "/api/import", bytes.NewReader(bundle)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("import into non-empty without replace: want 409, got %d", rec.Code)
	}
	// With replace → 200.
	rec = httptest.NewRecorder()
	dst.ServeHTTP(rec, httptest.NewRequest("POST", "/api/import?replace=true", bytes.NewReader(bundle)))
	if rec.Code != http.StatusOK {
		t.Fatalf("import with replace: want 200, got %d", rec.Code)
	}
}

func TestImportRejectsBadVersion(t *testing.T) {
	h := newTestServer(t)
	bad, _ := json.Marshal(map[string]any{"version": 999, "tasks": []any{}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/import", bytes.NewReader(bad)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad version: want 400, got %d", rec.Code)
	}
}

// TestImportRejectsMalformedTasks guards the import trust boundary: a hostile id
// (XSS payload) or an out-of-set status must be rejected, not stored.
func TestImportRejectsMalformedTasks(t *testing.T) {
	h := newTestServer(t)
	cases := []map[string]any{
		{"id": "'-evil()-'", "title": "x", "status": "todo"}, // id with breakout chars
		{"id": "t_0001", "title": "x", "status": "pwned"},     // bogus status
		{"id": "", "title": "x", "status": "todo"},            // empty id
	}
	for i, tk := range cases {
		bad, _ := json.Marshal(map[string]any{"version": 1, "tasks": []any{tk}})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/import?replace=true", bytes.NewReader(bad)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("case %d (%v): want 400, got %d", i, tk, rec.Code)
		}
	}
}

// TestImportReplaceFalseDoesNotOverwrite guards that replace is a real boolean:
// ?replace=false must NOT clobber a non-empty board.
func TestImportReplaceFalseDoesNotOverwrite(t *testing.T) {
	h := newTestServer(t)
	postJSON(t, h, "/api/tasks", map[string]any{"title": "existing"})
	bundle, _ := json.Marshal(map[string]any{"version": 1, "tasks": []any{
		map[string]any{"id": "t_0050", "title": "imported", "status": "todo"},
	}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/import?replace=false", bytes.NewReader(bundle)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("replace=false should not overwrite: want 409, got %d", rec.Code)
	}
}
