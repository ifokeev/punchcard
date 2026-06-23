package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestPreviewExtensionFilter verifies that the inline preview route only serves
// allowed image/video extensions and returns 403 for dangerous types like .html
// and .svg (which could trigger XSS if served inline).
func TestPreviewExtensionFilter(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	// Create a fake artifacts directory with test files.
	taskID := "t_0001"
	artifactDir := filepath.Join(dir, "artifacts", taskID)
	if err := os.MkdirAll(artifactDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	files := []string{"ok.png", "evil.html", "evil.svg"}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(artifactDir, f), []byte("data"), 0644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}

	mux := http.NewServeMux()
	registerBoard(mux)
	h := http.Handler(mux)

	cases := []struct {
		path string
		want int
	}{
		{"/preview/" + taskID + "/ok.png", http.StatusOK},
		{"/preview/" + taskID + "/evil.html", http.StatusForbidden},
		{"/preview/" + taskID + "/evil.svg", http.StatusForbidden},
	}

	for _, tc := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", tc.path, nil))
		if rec.Code != tc.want {
			t.Errorf("GET %s: status=%d want=%d body=%s", tc.path, rec.Code, tc.want, rec.Body)
		}
	}
}
