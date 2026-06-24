package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// defaultConcurrencyFallback is used when PUNCH_CONCURRENCY is unset or invalid.
const defaultConcurrencyFallback = 3

// defaultConcurrency is how many engineer subagents the loop may run at once
// when the server has no persisted value yet: PUNCH_CONCURRENCY if set and >=1,
// otherwise 3. A persisted control.json value always wins over this.
func defaultConcurrency() int {
	if v := os.Getenv("PUNCH_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			return n
		}
	}
	return defaultConcurrencyFallback
}

// Control is the server-side, board-controllable run state the loop honors:
// whether claiming is paused, and how many engineer subagents may run at once.
type Control struct {
	Paused      bool `json:"paused"`
	Concurrency int  `json:"concurrency"`
}

// ControlStore persists Control atomically, guarded by a single mutex.
type ControlStore struct {
	mu   sync.Mutex
	path string
	ctl  Control
}

func NewControlStore(path string) (*ControlStore, error) {
	cs := &ControlStore{path: path, ctl: Control{Concurrency: defaultConcurrency()}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cs, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, &cs.ctl); err != nil {
		return nil, err
	}
	if cs.ctl.Concurrency < 1 {
		cs.ctl.Concurrency = defaultConcurrency()
	}
	return cs, nil
}

func (cs *ControlStore) Get() Control {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.ctl
}

// Patch applies the non-nil fields, clamps concurrency to >=1, persists, and
// returns the new state. On flush failure it rolls back and returns the error.
func (cs *ControlStore) Patch(paused *bool, concurrency *int) (Control, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	old := cs.ctl
	if paused != nil {
		cs.ctl.Paused = *paused
	}
	if concurrency != nil {
		c := *concurrency
		if c < 1 {
			c = 1
		}
		cs.ctl.Concurrency = c
	}
	if err := cs.save(); err != nil {
		cs.ctl = old
		return Control{}, err
	}
	return cs.ctl, nil
}

// save writes control.json atomically (temp + fsync + rename). Caller holds cs.mu.
func (cs *ControlStore) save() error {
	data, err := json.MarshalIndent(cs.ctl, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(cs.path)
	tmp, err := os.CreateTemp(dir, ".control-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, cs.path)
}
