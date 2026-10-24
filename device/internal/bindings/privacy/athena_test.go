package privacy

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestAthenaWatchUsesSwitchStateAndRetries(t *testing.T) {
	events := make(chan athenaEvent, 1)
	mutedState := atomic.Bool{}
	controller := &AthenaController{
		state: func() (bool, error) { return mutedState.Load(), nil },
		readEvent: func() (athenaEvent, error) {
			event, ok := <-events
			if !ok {
				return athenaEvent{}, context.Canceled
			}
			return event, nil
		},
		close: func() error { close(events); return nil },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	accepted := make(chan bool, 1)
	var calls atomic.Int32
	go controller.Watch(ctx, false, func(muted bool) error {
		if calls.Add(1) == 1 {
			return errors.New("capture is settling")
		}
		accepted <- muted
		return nil
	})

	mutedState.Store(true)
	events <- athenaEvent{eventType: athenaMuteEventType, code: athenaMuteStateCode, value: 1}
	select {
	case muted := <-accepted:
		if !muted || calls.Load() < 2 {
			t.Fatalf("watch accepted muted=%t after %d calls", muted, calls.Load())
		}
	case <-time.After(time.Second):
		t.Fatal("Athena privacy watcher did not retry the transition")
	}
}

func TestAthenaInputPathUsesNamedHandler(t *testing.T) {
	data := []byte(`I: Bus=0019 Vendor=0001 Product=0001 Version=0100
N: Name="gpio-keys"
H: Handlers=kbd event12
B: EV=21

I: Bus=0019 Vendor=0001 Product=0001 Version=0100
N: Name="other"
H: Handlers=event2
B: EV=3
`)
	if got := athenaInputPathByName(data, "gpio-keys"); got != "/dev/input/event12" {
		t.Fatalf("Athena gpio-keys path = %q", got)
	}
}

func TestAthenaWatchIgnoresKeyEvents(t *testing.T) {
	events := make(chan athenaEvent, 2)
	controller := &AthenaController{
		state: func() (bool, error) { return false, nil },
		readEvent: func() (athenaEvent, error) {
			event, ok := <-events
			if !ok {
				return athenaEvent{}, context.Canceled
			}
			return event, nil
		},
		close: func() error { close(events); return nil },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	called := make(chan struct{}, 1)
	go controller.Watch(ctx, false, func(bool) error {
		called <- struct{}{}
		return nil
	})
	events <- athenaEvent{eventType: 0x01, code: 248, value: 1}
	select {
	case <-called:
		t.Fatal("KEY_MICMUTE must not stand in for Athena's switch state")
	case <-time.After(50 * time.Millisecond):
	}
}
