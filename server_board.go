package main

import (
	_ "embed"
	"net/http"
	"path/filepath"
	"strings"
)

//go:embed board.html
var boardHTML []byte

func registerBoard(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(boardHTML)
	})
	// Inline preview ONLY for image/video extensions, same-origin, nosniff.
	preview := http.StripPrefix("/preview/", http.FileServer(http.Dir("artifacts")))
	mux.HandleFunc("GET /preview/{path...}", func(w http.ResponseWriter, r *http.Request) {
		ext := strings.ToLower(filepath.Ext(r.URL.Path))
		switch ext {
		case ".png", ".gif", ".jpg", ".jpeg", ".webp", ".mp4", ".webm":
			w.Header().Set("X-Content-Type-Options", "nosniff")
			preview.ServeHTTP(w, r)
		default:
			http.Error(w, "preview not allowed", http.StatusForbidden)
		}
	})
}
