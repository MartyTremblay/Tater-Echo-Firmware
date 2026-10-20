package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeviceStateRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "etc", "state.json")

	// Missing file → not ok.
	if _, ok := loadDeviceState(path); ok {
		t.Fatal("expected ok=false for missing file")
	}

	// Save creates parent dirs and persists the muted flag.
	saveDeviceState(path, deviceState{Muted: true})
	st, ok := loadDeviceState(path)
	if !ok || !st.Muted {
		t.Fatalf("roundtrip failed: ok=%v st=%+v", ok, st)
	}

	saveDeviceState(path, deviceState{Muted: false})
	st, ok = loadDeviceState(path)
	if !ok || st.Muted {
		t.Fatalf("overwrite failed: ok=%v st=%+v", ok, st)
	}

	// Corrupt file → not ok, no panic.
	if err := os.WriteFile(path, []byte("{truncated"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadDeviceState(path); ok {
		t.Fatal("expected ok=false for corrupt file")
	}
}

func TestDeviceStateMigratesLegacyMute(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "tater", "state.json")
	legacy := filepath.Join(dir, "legacy", "state.json")
	saveDeviceState(legacy, deviceState{Muted: true})

	st, ok := loadDeviceStateWithFallback(current, legacy)
	if !ok || !st.Muted {
		t.Fatalf("migration failed: ok=%v st=%+v", ok, st)
	}
	if migrated, migratedOK := loadDeviceState(current); !migratedOK || !migrated.Muted {
		t.Fatalf("current state missing after migration: ok=%v st=%+v", migratedOK, migrated)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy state still exists: %v", err)
	}
}

func TestPreviousStatePathsExcludeCurrentNamespace(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "tater", "state.json")
	previous := filepath.Join(dir, "previous", "state.json")
	saveDeviceState(current, deviceState{Muted: false})
	saveDeviceState(previous, deviceState{Muted: true})

	paths := previousStatePaths(current)
	if len(paths) != 1 || paths[0] != previous {
		t.Fatalf("previous paths = %v, want [%s]", paths, previous)
	}
}
