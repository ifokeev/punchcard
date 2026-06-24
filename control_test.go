package main

import (
	"path/filepath"
	"testing"
)

func TestControlDefaultsAndRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.json")
	cs, err := NewControlStore(path)
	if err != nil {
		t.Fatalf("NewControlStore: %v", err)
	}
	if got := cs.Get(); got.Paused || got.Concurrency != defaultConcurrency() {
		t.Fatalf("defaults = %+v, want {false %d}", got, defaultConcurrency())
	}

	paused := true
	conc := 0 // must clamp to 1
	stopped := true
	ctl, err := cs.Patch(&paused, &conc, &stopped)
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	if !ctl.Paused || ctl.Concurrency != 1 || !ctl.Stopped {
		t.Fatalf("patched = %+v, want {true 1 true}", ctl)
	}

	// Reload from disk: state must persist.
	cs2, err := NewControlStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := cs2.Get(); !got.Paused || got.Concurrency != 1 || !got.Stopped {
		t.Fatalf("reloaded = %+v, want {true 1 true}", got)
	}
}

func TestControlPartialPatch(t *testing.T) {
	cs, _ := NewControlStore(filepath.Join(t.TempDir(), "control.json"))
	conc := 5
	if _, err := cs.Patch(nil, &conc, nil); err != nil { // only concurrency
		t.Fatalf("Patch: %v", err)
	}
	if got := cs.Get(); got.Paused || got.Concurrency != 5 || got.Stopped {
		t.Fatalf("partial patch = %+v, want {false 5 false}", got)
	}
}
