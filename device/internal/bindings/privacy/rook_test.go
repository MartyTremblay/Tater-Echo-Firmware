package privacy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestReadState(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "privacy_state")
	for _, test := range []struct {
		name, value string
		want        bool
		wantError   bool
	}{
		{name: "kernel muted", value: "1\n", want: true},
		{name: "kernel unmuted", value: "0\n", want: false},
		{name: "named muted", value: "muted\n", want: true},
		{name: "invalid fails closed", value: "unknown\n", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(test.value), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := readState(path)
			if got != test.want || (err != nil) != test.wantError {
				t.Fatalf("readState() = %t, %v; want %t error=%t", got, err, test.want, test.wantError)
			}
		})
	}
}

func TestWatchRetriesSynchronizationUntilAccepted(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "privacy_state")
	if err := os.WriteFile(state, []byte("0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	controller := &Controller{statePath: state}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	accepted := make(chan bool, 1)
	var calls atomic.Int32
	go controller.Watch(ctx, false, func(muted bool) error {
		if calls.Add(1) == 1 {
			return errors.New("codec settling")
		}
		accepted <- muted
		return nil
	})
	if err := os.WriteFile(state, []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case muted := <-accepted:
		if !muted || calls.Load() < 2 {
			t.Fatalf("watch accepted muted=%t after %d calls", muted, calls.Load())
		}
	case <-time.After(time.Second):
		t.Fatal("privacy watcher did not retry the failed synchronization")
	}
}

func TestEngageWritesKernelTrigger(t *testing.T) {
	root := t.TempDir()
	trigger := filepath.Join(root, "privacy_trigger")
	controller := &Controller{statePath: filepath.Join(root, "privacy_state"), triggerPath: trigger}
	if err := controller.Engage(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(trigger)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "1\n" {
		t.Fatalf("privacy trigger = %q, want 1\\n", got)
	}
}
