package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

const maxUploadDefault = 100 << 20 // 100MB

func safeName(name string) string {
	base := filepath.Base(filepath.FromSlash(name))
	if base == "." || base == "/" || base == "" || base == string(filepath.Separator) {
		return "file"
	}
	return base
}

// absURL builds an origin-correct absolute URL for human display. The board renders
// the relative form, so this is only for CLI echo / pr consumers.
func absURL(r *http.Request, originBase, rel string) string {
	if originBase != "" {
		return originBase + rel
	}
	scheme := "http"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto // only set when middleware allowed it (trusted proxy)
	} else if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		host = h
	}
	return fmt.Sprintf("%s://%s%s", scheme, host, rel)
}

// saveTaskUpload stores a multipart 'file' for task id under artifacts/<id>[/subdir],
// records it via record (s.Attach or s.AttachImage), and returns the relative URL. On any
// failure it writes the HTTP error and returns ok=false.
func saveTaskUpload(w http.ResponseWriter, r *http.Request, s *Store, subdir string,
	record func(id, relURL string) (*Task, error)) (string, bool) {
	id := r.PathValue("id")
	if _, ok := s.Get(id); !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return "", false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload) // cap BEFORE parse
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "expected multipart field 'file': "+err.Error(), http.StatusBadRequest)
		return "", false
	}
	defer file.Close()
	name := safeName(header.Filename)
	relDir := safeName(id) // id also sanitized
	if subdir != "" {
		relDir += "/" + subdir
	}
	dir := filepath.Join("artifacts", filepath.FromSlash(relDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return "", false
	}
	dst, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return "", false
	}
	if _, err := io.Copy(dst, file); err != nil {
		dst.Close()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return "", false
	}
	dst.Close()
	rel := "/artifacts/" + relDir + "/" + name
	if _, err := record(id, rel); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return "", false
	}
	return rel, true
}

func registerUploadRoutes(mux *http.ServeMux, s *Store, originBase string) {
	// Proof-of-work files the agent attaches (screenshots, test output).
	mux.HandleFunc("POST /api/tasks/{id}/artifacts", func(w http.ResponseWriter, r *http.Request) {
		if rel, ok := saveTaskUpload(w, r, s, "", s.Attach); ok {
			writeJSON(w, http.StatusCreated, map[string]string{"url": rel, "abs": absURL(r, originBase, rel)})
		}
	})

	// Reference images the USER attaches as input for the implementer (mockups/screenshots),
	// stored under artifacts/<id>/refs and tracked in task.images (separate from proof of work).
	mux.HandleFunc("POST /api/tasks/{id}/images", func(w http.ResponseWriter, r *http.Request) {
		if rel, ok := saveTaskUpload(w, r, s, "refs", s.AttachImage); ok {
			writeJSON(w, http.StatusCreated, map[string]string{"url": rel, "abs": absURL(r, originBase, rel)})
		}
	})

	// Static, read-only, force-download so attacker HTML/SVG can't run as active content.
	fs := http.StripPrefix("/artifacts/", http.FileServer(http.Dir("artifacts")))
	mux.HandleFunc("GET /artifacts/{path...}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", "attachment")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		fs.ServeHTTP(w, r)
	})
}

// maxUpload is overridable from serve flags; default set here.
var maxUpload int64 = maxUploadDefault
