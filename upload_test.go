package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSafeNameStripsTraversal(t *testing.T) {
	cases := map[string]string{
		"demo.gif":         "demo.gif",
		"../../etc/passwd": "passwd",
		"/abs/path/x.png":  "x.png",
		"a/b/c.mp4":        "c.mp4",
		"":                 "file",
	}
	for in, want := range cases {
		if got := safeName(in); got != want {
			t.Fatalf("safeName(%q)=%q want %q", in, got, want)
		}
	}
}

func TestUploadWritesAndAppends(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	s, _ := NewStore(filepath.Join(dir, "tasks.json"))
	task, _ := s.Create(TaskInput{Title: "t"})
	mux := http.NewServeMux()
	registerUploadRoutes(mux, s, "")

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "../evil.gif")
	fw.Write([]byte("GIF89a-bytes"))
	mw.Close()

	req := httptest.NewRequest("POST", "/api/tasks/"+task.ID+"/artifacts", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", rec.Code, rec.Body)
	}
	var resp struct{ URL string }
	json.Unmarshal(rec.Body.Bytes(), &resp)
	want := "/artifacts/" + task.ID + "/evil.gif" // traversal stripped to base name
	if resp.URL != want {
		t.Fatalf("url=%q want %q", resp.URL, want)
	}
	if _, err := os.Stat(filepath.Join("artifacts", task.ID, "evil.gif")); err != nil {
		t.Fatalf("file not written under artifacts/: %v", err)
	}
	got, _ := s.Get(task.ID)
	if len(got.Artifacts) != 1 || got.Artifacts[0] != want {
		t.Fatalf("artifacts not appended: %+v", got.Artifacts)
	}
}

func TestUploadImageAppendsToImages(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	s, _ := NewStore(filepath.Join(dir, "tasks.json"))
	task, _ := s.Create(TaskInput{Title: "t"})
	mux := http.NewServeMux()
	registerUploadRoutes(mux, s, "")

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "mockup.png")
	fw.Write([]byte("\x89PNG-bytes"))
	mw.Close()

	req := httptest.NewRequest("POST", "/api/tasks/"+task.ID+"/images", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("image upload status=%d body=%s", rec.Code, rec.Body)
	}
	var resp struct{ URL string }
	json.Unmarshal(rec.Body.Bytes(), &resp)
	want := "/artifacts/" + task.ID + "/refs/mockup.png" // input images live under refs/
	if resp.URL != want {
		t.Fatalf("url=%q want %q", resp.URL, want)
	}
	if _, err := os.Stat(filepath.Join("artifacts", task.ID, "refs", "mockup.png")); err != nil {
		t.Fatalf("image not written under artifacts/<id>/refs/: %v", err)
	}
	got, _ := s.Get(task.ID)
	if len(got.Images) != 1 || got.Images[0] != want {
		t.Fatalf("images not appended: %+v", got.Images)
	}
	// input images must NOT land in proof-of-work artifacts
	if len(got.Artifacts) != 0 {
		t.Fatalf("artifacts should stay empty, got %+v", got.Artifacts)
	}
}
