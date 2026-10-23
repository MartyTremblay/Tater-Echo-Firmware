package buttons

import (
	"fmt"
	"time"

	pkgbuttons "github.com/TaterTotterson/Tater-Echo-Firmware/pkg/buttons"
)

const startupHoldPollInterval = 100 * time.Millisecond

type keyStateReader interface {
	Pressed(code uint16) (bool, error)
	Close() error
}

var openKeyStateReader = newKeyStateReader

// VolumeUpHeldFor reports whether Volume Up is already down and remains down
// continuously for duration. Reading the kernel's current key bitmap matters
// here: a button pressed before the daemon starts has no new key-down event for
// the normal event subscription to receive.
func (e *EvDevController) VolumeUpHeldFor(duration time.Duration) (bool, error) {
	reader, err := openKeyStateReader(e.volumePath)
	if err != nil {
		return false, fmt.Errorf("open volume input %s: %w", e.volumePath, err)
	}
	defer reader.Close()

	pressed, err := reader.Pressed(uint16(pkgbuttons.VolumeUpClick))
	if err != nil {
		return false, fmt.Errorf("read Volume Up state: %w", err)
	}
	if !pressed {
		return false, nil
	}
	if duration <= 0 {
		return true, nil
	}

	deadline := time.Now().Add(duration)
	ticker := time.NewTicker(startupHoldPollInterval)
	defer ticker.Stop()
	for now := range ticker.C {
		pressed, err = reader.Pressed(uint16(pkgbuttons.VolumeUpClick))
		if err != nil {
			return false, fmt.Errorf("read Volume Up state: %w", err)
		}
		if !pressed {
			return false, nil
		}
		if !now.Before(deadline) {
			return true, nil
		}
	}
	return false, nil
}
