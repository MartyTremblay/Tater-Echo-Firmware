package recovery

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	StartupGestureWindow = 90 * time.Second
	StartupGestureHold   = 5 * time.Second
)

type Command struct {
	Path string
	Args []string
}

func ParseUptime(contents string) (time.Duration, error) {
	fields := strings.Fields(contents)
	if len(fields) == 0 {
		return 0, fmt.Errorf("empty uptime")
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || seconds < 0 {
		return 0, fmt.Errorf("invalid uptime %q", fields[0])
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

func InStartupGestureWindow(path string) (bool, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	uptime, err := ParseUptime(string(contents))
	if err != nil {
		return false, err
	}
	return uptime <= StartupGestureWindow, nil
}

// TaterRecoveryCommand arms the platform's one-boot initramfs rescue marker
// and performs the normal synchronized Tater Linux restart.  All five Echo
// targets use slotctl even though Biscuit/Radar keep their store under
// /store/techo5 while the screen targets use /store directly; slotctl owns
// that platform-specific path.
//
// This deliberately does not use the bootloader's Android "recovery" target.
// On Biscuit/Radar that would replace or bypass the Linux image, and on the
// screen targets it would enter TWRP.  TWRP remains the lower-level fallback;
// the physical gesture enters the safer Tater rescue environment.
func TaterRecoveryCommand(target string) (Command, error) {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "biscuit", "radar", "checkers", "crown", "rook":
		return Command{
			Path: "/bin/sh",
			Args: []string{
				"-c",
				"/usr/local/sbin/slotctl rescue && exec /usr/local/bin/tater-reboot-now",
			},
		}, nil
	default:
		return Command{}, fmt.Errorf("unsupported recovery target %q", target)
	}
}
