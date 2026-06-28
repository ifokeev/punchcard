package main

import (
	"encoding/json"
	"net/http"
	"time"
)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// newMux wires the API. originBase, when non-empty, overrides the public origin for
// absolute upload URLs (see upload.go); the board uses relative paths regardless.
func newMux(s *Store, ms *MemoryStore, cs *ControlStore, originBase string) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.List())
	})

	mux.HandleFunc("POST /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Title, Description, Acceptance, Repo string
			Priority                             int
			DependsOn                            []string `json:"depends_on"`
			Force                                bool     `json:"force"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if in.Title == "" {
			http.Error(w, "title required", http.StatusBadRequest)
			return
		}
		if !in.Force {
			if dups := s.SimilarActive(in.Title); len(dups) > 0 {
				writeJSON(w, http.StatusConflict, map[string]any{
					"error":      "an active task with this title already exists (set force to add anyway)",
					"duplicates": dups,
				})
				return
			}
		}
		t, err := s.Create(TaskInput{
			Title: in.Title, Description: in.Description, Acceptance: in.Acceptance,
			Repo: in.Repo, Priority: in.Priority, DependsOn: in.DependsOn,
		})
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

	// POST /api/next claims work for the loop.
	//   paused        -> 423 Locked (loop should idle, not stop)
	//   ?batch=1      -> up to `concurrency` tasks as a JSON array (204 if none)
	//   (no batch)    -> a single task object (204 if none) — back-compat
	mux.HandleFunc("POST /api/next", func(w http.ResponseWriter, r *http.Request) {
		cs.TouchPoll() // worker liveness: a loop reached us, even if paused/empty
		if cs.Get().Paused {
			writeJSON(w, http.StatusLocked, map[string]string{"status": "paused"})
			return
		}
		ceiling := cs.Get().Concurrency
		if r.URL.Query().Get("batch") != "" {
			tasks := s.ClaimBatch(ceiling, ceiling)
			if len(tasks) == 0 {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			writeJSON(w, http.StatusOK, tasks)
			return
		}
		// Single claim still respects the concurrency ceiling.
		tasks := s.ClaimBatch(ceiling, 1)
		if len(tasks) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, tasks[0])
	})

	// GET /api/pending-merges lists the done-but-unmerged tasks that are blocking a
	// todo via depends_on — exactly the PRs the loop should check for merge.
	mux.HandleFunc("GET /api/pending-merges", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.PendingMerges())
	})

	// POST /api/cancel-all is the kill-switch: cancel every in_progress task.
	mux.HandleFunc("POST /api/cancel-all", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]int{"cancelled": s.CancelInProgress()})
	})

	mux.HandleFunc("GET /api/control", func(w http.ResponseWriter, r *http.Request) {
		// last_poll is null until a loop has polled — the board reads it as
		// "no worker connected" vs "worker active · Ns ago".
		resp := struct {
			Control
			LastPoll *time.Time `json:"last_poll"`
		}{Control: cs.Get()}
		if lp := cs.LastPoll(); !lp.IsZero() {
			resp.LastPoll = &lp
		}
		writeJSON(w, http.StatusOK, resp)
	})

	mux.HandleFunc("PATCH /api/control", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Paused      *bool `json:"paused"`
			Concurrency *int  `json:"concurrency"`
			Stopped     *bool `json:"stopped"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		ctl, err := cs.Patch(in.Paused, in.Concurrency, in.Stopped)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, ctl)
	})

	mux.HandleFunc("PATCH /api/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Status   *Status `json:"status"`
			PRURL    *string `json:"pr_url"`
			Branch   *string `json:"branch"`
			Note     *string `json:"note"`
			Merged   *bool   `json:"merged"`
			Progress *string `json:"progress"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if in.Status != nil && !validStatus(*in.Status) {
			http.Error(w, "invalid status", http.StatusBadRequest)
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

	registerTransferRoutes(mux, s, ms)       // export/import — defined in transfer.go
	registerUploadRoutes(mux, s, originBase) // defined in upload.go (Task 5)
	registerBoard(mux)                       // defined in server_board.go via Task 8
	return mux
}
