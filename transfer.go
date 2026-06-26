package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// bundleVersion is bumped if the export format changes incompatibly.
const bundleVersion = 1

// maxImportBytes caps the import body so a malformed/hostile bundle can't OOM
// the server. A real board is kilobytes; 64 MiB is generous.
const maxImportBytes = 64 << 20

// Bundle is a portable snapshot of a board: its tasks and memory notes. It moves
// a board between instances (e.g. local → remote VM) over the API.
type Bundle struct {
	Version    int       `json:"version"`
	ExportedAt time.Time `json:"exported_at"`
	Tasks      []*Task   `json:"tasks"`
	Memory     []*Note   `json:"memory"`
}

// registerTransferRoutes wires GET /api/export and POST /api/import.
func registerTransferRoutes(mux *http.ServeMux, s *Store, ms *MemoryStore) {
	mux.HandleFunc("GET /api/export", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, Bundle{
			Version:    bundleVersion,
			ExportedAt: time.Now().UTC(),
			Tasks:      s.List(),
			Memory:     ms.ListNotes("", ""),
		})
	})

	// POST /api/import?replace=true loads a bundle. Without replace it refuses a
	// non-empty board (409) so an import can't silently clobber existing work.
	mux.HandleFunc("POST /api/import", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxImportBytes)
		var b Bundle
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if b.Version != bundleVersion {
			http.Error(w, "unsupported bundle version", http.StatusBadRequest)
			return
		}
		// Validate BEFORE touching the board: ids and statuses here are
		// caller-controlled and get rendered/bucketed by the UI, so reject anything
		// malformed rather than store it verbatim.
		for _, t := range b.Tasks {
			if t == nil || !validID(t.ID) || !validStatus(t.Status) {
				http.Error(w, "bundle has an invalid task (id/status)", http.StatusBadRequest)
				return
			}
		}
		for _, n := range b.Memory {
			if n == nil || !validID(n.ID) {
				http.Error(w, "bundle has an invalid note (id)", http.StatusBadRequest)
				return
			}
		}
		replace, _ := strconv.ParseBool(r.URL.Query().Get("replace")) // only truthy enables overwrite
		if !replace && (!s.Empty() || !ms.Empty()) {
			http.Error(w, "target board not empty (pass replace=true to overwrite)", http.StatusConflict)
			return
		}
		oldTasks := s.List() // snapshot for best-effort rollback if the memory swap fails
		if err := s.Replace(b.Tasks); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := ms.Replace(b.Memory); err != nil {
			s.Replace(oldTasks) // undo the task swap so we don't leave a half-import
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"tasks": len(b.Tasks), "memory": len(b.Memory)})
	})
}
