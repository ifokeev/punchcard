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

func registerUploadRoutes(mux *http.ServeMux, s *Store, originBase string) {
	mux.HandleFunc("POST /api/tasks/{id}/artifacts", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if _, ok := s.Get(id); !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxUpload) // cap BEFORE parse
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "expected multipart field 'file': "+err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()
		name := safeName(header.Filename)
		dir := filepath.Join("artifacts", safeName(id)) // id also sanitized
		if err := os.MkdirAll(dir, 0o755); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		dst, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if _, err := io.Copy(dst, file); err != nil {
			dst.Close()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		dst.Close()
		rel := "/artifacts/" + safeName(id) + "/" + name
		if _, err := s.Attach(id, rel); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"url": rel, "abs": absURL(r, originBase, rel)})
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
