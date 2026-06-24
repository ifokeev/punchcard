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
func newMux(s *Store, ms *MemoryStore, originBase string) *http.ServeMux {
	mux := http.NewServeMux()

	// Liveness/health probe. No auth, no store access — must answer even if the
	// data store is unavailable. tokenMiddleware bypasses auth for this path.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

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

	// Memory routes
	mux.HandleFunc("GET /api/memory", func(w http.ResponseWriter, r *http.Request) {
		repo := r.URL.Query().Get("repo")
		q := r.URL.Query().Get("q")
		writeJSON(w, http.StatusOK, ms.ListNotes(repo, q))
	})

	mux.HandleFunc("POST /api/memory", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Title string   `json:"title"`
			Body  string   `json:"body"`
			Repo  string   `json:"repo"`
			Tags  []string `json:"tags"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if in.Title == "" {
			http.Error(w, "title required", http.StatusBadRequest)
			return
		}
		n, err := ms.AddNote(NoteInput{Title: in.Title, Body: in.Body, Repo: in.Repo, Tags: in.Tags})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, n)
	})

	mux.HandleFunc("GET /api/memory/{id}", func(w http.ResponseWriter, r *http.Request) {
		n, ok := ms.GetNote(r.PathValue("id"))
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, n)
	})

	mux.HandleFunc("PATCH /api/memory/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Title        *string   `json:"title"`
			Body         *string   `json:"body"`
			Repo         *string   `json:"repo"`
			Tags         *[]string `json:"tags"`
			SupersededBy *string   `json:"superseded_by"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		n, err := ms.UpdateNote(r.PathValue("id"), NotePatch{
			Title:        in.Title,
			Body:         in.Body,
			Repo:         in.Repo,
			Tags:         in.Tags,
			SupersededBy: in.SupersededBy,
		})
		if err == errNoteNotFound {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, n)
	})

	mux.HandleFunc("DELETE /api/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.DeleteTask(r.PathValue("id")); err == errNotFound {
			http.Error(w, "not found", http.StatusNotFound)
			return
		} else if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /api/memory/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := ms.DeleteNote(r.PathValue("id")); err == errNoteNotFound {
			http.Error(w, "not found", http.StatusNotFound)
			return
		} else if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	registerUploadRoutes(mux, s, originBase) // defined in upload.go (Task 5)
	registerBoard(mux)                       // defined in server_board.go via Task 8
	return mux
}
