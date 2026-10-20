package server

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

// statePath persists mute state across reboots and OTA restarts. It lives
// next to the TLS credentials in /data/local/etc — OTA slot flips only touch
// /data/local/bin, so the file survives them.
//
// Only mute lives here: mute is device-sovereign (the physical button cannot
// be overridden remotely, so the device must restore it itself, controller or
// no controller). Volume is the opposite — the controller's stored
// startupVolume is the source of truth, re-applied via SeedVolume on the
// first config push each run.
const statePath = "/data/local/etc/tater/state.json"

type deviceState struct {
	Muted bool `json:"muted"`
}

// loadDeviceState reads the persisted state. ok=false means no usable state
// (first boot of persisting firmware, or unreadable file) — boot unmuted as
// before.
func loadDeviceState(path string) (st deviceState, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return deviceState{}, false
	}
	if err := json.Unmarshal(data, &st); err != nil {
		log.Printf("[state] %s unreadable, ignoring: %v", path, err)
		return deviceState{}, false
	}
	return st, true
}

// loadDeviceStateWithFallback preserves the mute state of already-installed
// Tater-native devices while moving the file into the final Tater namespace.
// The old filename is data compatibility only; no retired runtime is used.
func loadDeviceStateWithFallback(current, legacy string) (deviceState, bool) {
	if st, ok := loadDeviceState(current); ok {
		return st, true
	}
	st, ok := loadDeviceState(legacy)
	if !ok {
		return deviceState{}, false
	}
	saveDeviceState(current, st)
	if migrated, migratedOK := loadDeviceState(current); migratedOK {
		if err := os.Remove(legacy); err != nil && !os.IsNotExist(err) {
			log.Printf("[state] remove migrated state failed: %v", err)
		}
		return migrated, true
	}
	return st, true
}

// previousStatePaths finds state files from an earlier namespace without
// coupling the current firmware to a retired project or directory name.
func previousStatePaths(current string) []string {
	root := filepath.Dir(filepath.Dir(current))
	matches, err := filepath.Glob(filepath.Join(root, "*", filepath.Base(current)))
	if err != nil {
		return nil
	}
	paths := matches[:0]
	for _, path := range matches {
		if path != current {
			paths = append(paths, path)
		}
	}
	return paths
}

// saveDeviceState writes the state atomically (write-then-rename, so a power
// cut mid-write can't leave a truncated file). Called on every mute toggle —
// rare enough that a synchronous flash write needs no debounce.
func saveDeviceState(path string, st deviceState) {
	data, err := json.Marshal(st)
	if err != nil {
		log.Printf("[state] marshal failed: %v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("[state] mkdir failed: %v", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		log.Printf("[state] write failed: %v", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Printf("[state] rename failed: %v", err)
	}
}
