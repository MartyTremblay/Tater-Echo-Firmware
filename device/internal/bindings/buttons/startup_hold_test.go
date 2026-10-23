package buttons

import (
	"errors"
	"testing"
	"time"
)

type fakeKeyStateReader struct {
	states []bool
	reads  int
	closed bool
}

func (f *fakeKeyStateReader) Pressed(_ uint16) (bool, error) {
	f.reads++
	if len(f.states) == 0 {
		return false, nil
	}
	state := f.states[0]
	if len(f.states) > 1 {
		f.states = f.states[1:]
	}
	return state, nil
}

func (f *fakeKeyStateReader) Close() error {
	f.closed = true
	return nil
}

func TestVolumeUpMustAlreadyBeHeld(t *testing.T) {
	reader := &fakeKeyStateReader{states: []bool{false}}
	original := openKeyStateReader
	openKeyStateReader = func(path string) (keyStateReader, error) {
		if path != "/dev/input/volume" {
			t.Fatalf("path = %q", path)
		}
		return reader, nil
	}
	defer func() { openKeyStateReader = original }()

	controller := &EvDevController{volumePath: "/dev/input/volume"}
	held, err := controller.VolumeUpHeldFor(time.Second)
	if err != nil || held {
		t.Fatalf("held=%v err=%v", held, err)
	}
	if reader.reads != 1 || !reader.closed {
		t.Fatalf("reads=%d closed=%v", reader.reads, reader.closed)
	}
}

func TestVolumeUpImmediateHeldCheck(t *testing.T) {
	reader := &fakeKeyStateReader{states: []bool{true}}
	original := openKeyStateReader
	openKeyStateReader = func(string) (keyStateReader, error) { return reader, nil }
	defer func() { openKeyStateReader = original }()

	held, err := (&EvDevController{volumePath: "volume"}).VolumeUpHeldFor(0)
	if err != nil || !held {
		t.Fatalf("held=%v err=%v", held, err)
	}
}

func TestVolumeUpMustRemainHeldContinuously(t *testing.T) {
	reader := &fakeKeyStateReader{states: []bool{true, false}}
	original := openKeyStateReader
	openKeyStateReader = func(string) (keyStateReader, error) { return reader, nil }
	defer func() { openKeyStateReader = original }()

	held, err := (&EvDevController{volumePath: "volume"}).VolumeUpHeldFor(time.Second)
	if err != nil || held {
		t.Fatalf("held=%v err=%v", held, err)
	}
	if reader.reads != 2 {
		t.Fatalf("reads=%d, want 2", reader.reads)
	}
}

func TestVolumeUpContinuousHoldCompletes(t *testing.T) {
	reader := &fakeKeyStateReader{states: []bool{true}}
	original := openKeyStateReader
	openKeyStateReader = func(string) (keyStateReader, error) { return reader, nil }
	defer func() { openKeyStateReader = original }()

	held, err := (&EvDevController{volumePath: "volume"}).VolumeUpHeldFor(50 * time.Millisecond)
	if err != nil || !held {
		t.Fatalf("held=%v err=%v", held, err)
	}
}

func TestVolumeUpReaderOpenFailure(t *testing.T) {
	original := openKeyStateReader
	openKeyStateReader = func(string) (keyStateReader, error) {
		return nil, errors.New("missing")
	}
	defer func() { openKeyStateReader = original }()

	_, err := (&EvDevController{volumePath: "volume"}).VolumeUpHeldFor(0)
	if err == nil {
		t.Fatal("expected open failure")
	}
}
