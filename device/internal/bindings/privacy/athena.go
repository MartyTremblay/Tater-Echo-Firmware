package privacy

import (
	"context"
	"errors"
	"log"
	"regexp"
	"strings"
	"time"
)

const (
	athenaMuteEventType = 0x05 // EV_SW
	athenaMuteStateCode = 0x0e // SW_MUTE_DEVICE
	athenaRetryInterval = 100 * time.Millisecond
)

type athenaEvent struct {
	eventType uint16
	code      uint16
	value     int32
}

var athenaEventHandler = regexp.MustCompile(`\b(event[0-9]+)\b`)

func athenaInputPathByName(data []byte, name string) string {
	for _, block := range strings.Split(string(data), "\n\n") {
		if !strings.Contains(block, `N: Name="`+name+`"`) {
			continue
		}
		for _, line := range strings.Split(block, "\n") {
			if !strings.HasPrefix(line, "H: Handlers=") {
				continue
			}
			if match := athenaEventHandler.FindStringSubmatch(line); len(match) == 2 {
				return "/dev/input/" + match[1]
			}
		}
	}
	return ""
}

// AthenaController follows the hardware privacy latch exposed by the TECHO5
// mainline kernel. The switch is authoritative: the button changes the latch
// before userspace receives its key event.
type AthenaController struct {
	state     func() (bool, error)
	readEvent func() (athenaEvent, error)
	close     func() error
}

// State returns true while Athena's hardware latch cuts the microphones and
// camera.
func (c *AthenaController) State() (bool, error) {
	if c == nil || c.state == nil {
		return false, errors.New("athena privacy switch is unavailable")
	}
	return c.state()
}

// Watch applies every hardware switch transition as an explicit mute state.
// A rejected transition is retried against the current switch position so a
// transient codec or capture error cannot strand software out of sync.
func (c *AthenaController) Watch(ctx context.Context, initial bool, callback func(bool) error) {
	if c == nil || c.readEvent == nil {
		return
	}
	if done := ctx.Done(); done != nil && c.close != nil {
		go func() {
			<-done
			_ = c.close()
		}()
	}

	last := initial
	for {
		event, err := c.readEvent()
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("[privacy] Athena switch watcher stopped: %v", err)
			}
			return
		}
		if event.eventType != athenaMuteEventType || event.code != athenaMuteStateCode {
			continue
		}
		muted := event.value != 0
		if muted == last {
			continue
		}
		for {
			if callback == nil {
				last = muted
				break
			}
			if err := callback(muted); err == nil {
				last = muted
				break
			} else {
				log.Printf("[privacy] Athena mute synchronization deferred: %v", err)
			}
			timer := time.NewTimer(athenaRetryInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			if state, err := c.State(); err == nil {
				muted = state
			}
		}
	}
}
