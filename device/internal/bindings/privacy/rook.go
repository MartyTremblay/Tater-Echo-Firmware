package privacy

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const rookPrivacyPollInterval = 100 * time.Millisecond

var rookPrivacyStatePatterns = []string{
	"/sys/devices/soc/10010000.keypad/privacy_state",
	"/sys/devices/soc/10010000.keypad/amz_privacy/privacy_state",
	"/sys/devices/soc/*/privacy_state",
	"/sys/devices/soc/*/amz_privacy/privacy_state",
}

// Controller exposes Rook's kernel privacy switch. The 3.18 Rook kernel owns
// the physical mic/camera button, so this state is authoritative; evdev alone
// is not guaranteed to publish a usable release event on every device.
type Controller struct {
	statePath   string
	triggerPath string
}

// Open locates the Rook privacy driver's state node. A missing driver is not
// fatal to the daemon: callers can retain the legacy evdev mute fallback.
func Open() (*Controller, error) {
	paths := make([]string, 0, len(rookPrivacyStatePatterns))
	seen := map[string]bool{}
	for _, pattern := range rookPrivacyStatePatterns {
		matches := []string{pattern}
		if strings.ContainsAny(pattern, "*?[") {
			matches, _ = filepath.Glob(pattern)
		}
		for _, path := range matches {
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			paths = append(paths, path)
		}
	}
	var lastErr error
	for _, path := range paths {
		if _, err := readState(path); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				lastErr = err
			}
			continue
		}
		return &Controller{
			statePath:   path,
			triggerPath: filepath.Join(filepath.Dir(path), "privacy_trigger"),
		}, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, os.ErrNotExist
}

// State returns true while the hardware privacy switch blocks the microphones
// and camera.
func (c *Controller) State() (bool, error) {
	if c == nil || strings.TrimSpace(c.statePath) == "" {
		return false, os.ErrNotExist
	}
	return readState(c.statePath)
}

// Engage asks the kernel driver to enter privacy mode. The hardware button is
// still the only authority that may leave privacy mode.
func (c *Controller) Engage() error {
	if c == nil || strings.TrimSpace(c.triggerPath) == "" {
		return os.ErrNotExist
	}
	if err := os.WriteFile(c.triggerPath, []byte("1\n"), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", c.triggerPath, err)
	}
	return nil
}

// Watch synchronizes transitions from the kernel driver into the daemon. A
// callback error is retried on the next poll, which matters when an unmute is
// temporarily refused because codec readback has not settled yet.
func (c *Controller) Watch(ctx context.Context, initial bool, callback func(bool) error) {
	ticker := time.NewTicker(rookPrivacyPollInterval)
	defer ticker.Stop()
	last := initial
	lastStateError := ""
	lastCallbackError := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			muted, err := c.State()
			if err != nil {
				message := err.Error()
				if message != lastStateError {
					log.Printf("[privacy] Rook state unavailable: %v", err)
					lastStateError = message
				}
				continue
			}
			if lastStateError != "" {
				log.Printf("[privacy] Rook state recovered")
				lastStateError = ""
			}
			if muted == last {
				continue
			}
			if callback != nil {
				if err := callback(muted); err != nil {
					message := err.Error()
					if message != lastCallbackError {
						log.Printf("[privacy] Rook mute synchronization deferred: %v", err)
						lastCallbackError = message
					}
					continue
				}
			}
			last = muted
			lastCallbackError = ""
		}
	}
}

func readState(path string) (bool, error) {
	value, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case "1", "on", "true", "muted":
		return true, nil
	case "0", "off", "false", "unmuted":
		return false, nil
	default:
		return false, fmt.Errorf("invalid privacy state %q from %s", strings.TrimSpace(string(value)), path)
	}
}
