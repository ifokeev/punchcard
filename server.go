package main

import (
	"encoding/json"
	"net/http"
)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// newMux wires the API. originBase, when non-empty, overrides the public origin for
// absolute upload URLs (see upload.go); the board uses relative paths regardless.
func newMux(s *Store, originBase string) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.List())
	})

	mux.HandleFunc("POST /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Title, Description, Acceptance, Repo string
			Priority                             int
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if in.Title == "" {
			http.Error(w, "title required", http.StatusBadRequest)
			return
		}
		t, err := s.Create(TaskInput(in))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, t)
	})

	mux.HandleFunc("GET /api/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		t, ok := s.Get(r.PathValue("id"))
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, t)
	})

	mux.HandleFunc("POST /api/next", func(w http.ResponseWriter, r *http.Request) {
		t, ok := s.Claim()
		if !ok {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, t)
	})

	mux.HandleFunc("PATCH /api/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Status *Status `json:"status"`
			PRURL  *string `json:"pr_url"`
			Branch *string `json:"branch"`
			Note   *string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		t, err := s.Patch(r.PathValue("id"), Patch(in))
		if err == errNotFound {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, t)
	})

	registerUploadRoutes(mux, s, originBase) // defined in upload.go (Task 5)
	registerBoard(mux)                       // defined in server_board.go via Task 8
	return mux
}

func registerUploadRoutes(mux *http.ServeMux, s *Store, originBase string) {}
func registerBoard(mux *http.ServeMux)                                      {}
