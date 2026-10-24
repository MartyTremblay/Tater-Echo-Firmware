//go:build linux

package linuxinput

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

func TestTouchInputProfileAcceptsAthenaMainlineName(t *testing.T) {
	protocolA, supported := touchInputProfile("generic ft5x06 (3e)")
	if !supported || protocolA {
		t.Fatalf("Athena touch profile = protocolA %v, supported %v", protocolA, supported)
	}
}

func TestTouchInputProfileKeepsRookProtocolA(t *testing.T) {
	protocolA, supported := touchInputProfile("mtk-tpd")
	if !supported || !protocolA {
		t.Fatalf("Rook touch profile = protocolA %v, supported %v", protocolA, supported)
	}
}

func TestTouchInputProfileRejectsUnrelatedInput(t *testing.T) {
	if _, supported := touchInputProfile("gpio-keys"); supported {
		t.Fatal("gpio-keys was accepted as a touchscreen")
	}
}

func TestRookProtocolATouchUsesNativeCoordinates(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "rook-touch-")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	write := func(kind, code uint16, value int32) {
		buffer := make([]byte, inputEventBytes)
		binary.LittleEndian.PutUint16(buffer[inputEventType:], kind)
		binary.LittleEndian.PutUint16(buffer[inputEventCode:], code)
		binary.LittleEndian.PutUint32(buffer[inputEventValue:], uint32(value))
		if _, err := file.Write(buffer); err != nil {
			t.Fatal(err)
		}
	}
	write(evAbs, absMTPositionX, 120)
	write(evAbs, absMTPositionY, 180)
	write(evKey, btnTouch, 1)
	write(evSyn, synReport, 0)
	write(evAbs, absMTPositionX, 130)
	write(evAbs, absMTPositionY, 190)
	write(evSyn, synReport, 0)
	write(evKey, btnTouch, 0)
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	device := &Device{file: file, path: file.Name(), rawWidth: 480, rawHeight: 480,
		frameWidth: 480, frameHeight: 480, protocolA: true}
	events := make(chan TouchEvent, 4)
	if err := device.runProtocolA(context.Background(), events); !errors.Is(err, io.EOF) {
		t.Fatalf("reading recorded touch = %v, want EOF", err)
	}
	want := []TouchEvent{{Kind: Down, X: 120, Y: 180}, {Kind: Move, X: 130, Y: 190}, {Kind: Up, X: 130, Y: 190}}
	for index, expected := range want {
		select {
		case got := <-events:
			if got != expected {
				t.Errorf("event %d = %+v, want %+v", index, got, expected)
			}
		default:
			t.Fatalf("missing event %d", index)
		}
	}
}

func TestDeliverPreservesReleaseWhenMovementQueueIsFull(t *testing.T) {
	events := make(chan TouchEvent, 1)
	events <- TouchEvent{Kind: Move, X: 10, Y: 10}
	delivered := make(chan struct{})
	go func() {
		deliver(context.Background(), events, TouchEvent{Kind: Up, X: 20, Y: 20})
		close(delivered)
	}()
	select {
	case <-delivered:
		t.Fatal("release was discarded while the movement queue was full")
	case <-time.After(10 * time.Millisecond):
	}
	<-events
	select {
	case event := <-events:
		if event.Kind != Up || event.X != 20 || event.Y != 20 {
			t.Fatalf("preserved event = %+v, want release", event)
		}
	case <-time.After(time.Second):
		t.Fatal("release was not delivered after queue space became available")
	}
	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("release delivery did not complete")
	}
}

func TestCheckersTouchStillRotates(t *testing.T) {
	device := &Device{rawWidth: 480, rawHeight: 960, frameWidth: 960, frameHeight: 480}
	x, y := device.frame(&position{x: 120, y: 180, seenX: true, seenY: true})
	if x != 180 || y != 359 {
		t.Fatalf("Checkers touch = (%d,%d), want (180,359)", x, y)
	}
}

func TestCrownTouchRotatesTo1280By800(t *testing.T) {
	device := &Device{rawWidth: 800, rawHeight: 1280, frameWidth: 1280, frameHeight: 800}
	x, y := device.frame(&position{x: 200, y: 320, seenX: true, seenY: true})
	if x != 320 || y != 599 {
		t.Fatalf("Crown touch = (%d,%d), want (320,599)", x, y)
	}
}

func TestCrownProtocolBWaitsForCoordinatesBeforeTouchDown(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "crown-touch-")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	write := func(kind, code uint16, value int32) {
		buffer := make([]byte, inputEventBytes)
		binary.LittleEndian.PutUint16(buffer[inputEventType:], kind)
		binary.LittleEndian.PutUint16(buffer[inputEventCode:], code)
		binary.LittleEndian.PutUint32(buffer[inputEventValue:], uint32(value))
		if _, err := file.Write(buffer); err != nil {
			t.Fatal(err)
		}
	}

	// Crown reports a tracking ID before the coordinates in each frame. A
	// Down emitted at that first event would land at (0,0), making controls
	// that require a matching Down and Up appear to need repeated taps.
	write(evAbs, absMTSlot, 0)
	write(evAbs, absMTTrackingID, 41)
	write(evAbs, absMTPositionX, 40)
	write(evAbs, absMTPositionY, 1180)
	write(evSyn, synReport, 0)
	write(evAbs, absMTTrackingID, -1)
	write(evSyn, synReport, 0)

	write(evAbs, absMTTrackingID, 42)
	write(evAbs, absMTPositionX, 60)
	write(evAbs, absMTPositionY, 1100)
	write(evSyn, synReport, 0)
	write(evAbs, absMTTrackingID, -1)
	write(evSyn, synReport, 0)

	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	device := &Device{file: file, path: file.Name(), rawWidth: 800, rawHeight: 1280,
		frameWidth: 1280, frameHeight: 800}
	events := make(chan TouchEvent, 8)
	if err := device.Run(context.Background(), events); !errors.Is(err, io.EOF) {
		t.Fatalf("reading recorded touch = %v, want EOF", err)
	}
	want := []TouchEvent{
		{Kind: Down, X: 1180, Y: 759}, {Kind: Up, X: 1180, Y: 759},
		{Kind: Down, X: 1100, Y: 739}, {Kind: Up, X: 1100, Y: 739},
	}
	for index, expected := range want {
		select {
		case got := <-events:
			if got != expected {
				t.Errorf("event %d = %+v, want %+v", index, got, expected)
			}
		default:
			t.Fatalf("missing event %d", index)
		}
	}
	select {
	case extra := <-events:
		t.Fatalf("unexpected extra event: %+v", extra)
	default:
	}
}

func TestCrownAcceptsRepeatedBTNContactsWithoutRepeatedTrackingIDs(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "crown-hybrid-touch-")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	write := func(kind, code uint16, value int32) {
		buffer := make([]byte, inputEventBytes)
		binary.LittleEndian.PutUint16(buffer[inputEventType:], kind)
		binary.LittleEndian.PutUint16(buffer[inputEventCode:], code)
		binary.LittleEndian.PutUint32(buffer[inputEventValue:], uint32(value))
		if _, err := file.Write(buffer); err != nil {
			t.Fatal(err)
		}
	}

	// The first contact includes a protocol-B tracking ID.
	write(evAbs, absMTTrackingID, 7)
	write(evAbs, absMTPositionX, 100)
	write(evAbs, absMTPositionY, 1000)
	write(evKey, btnTouch, 1)
	write(evSyn, synReport, 0)
	write(evKey, btnTouch, 0)
	write(evSyn, synReport, 0)

	// Later contacts on some fts_ts revisions are delimited only by BTN_TOUCH.
	write(evKey, btnTouch, 1)
	write(evAbs, absMTPositionX, 200)
	write(evAbs, absMTPositionY, 1100)
	write(evSyn, synReport, 0)
	write(evKey, btnTouch, 0)
	write(evSyn, synReport, 0)

	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	device := &Device{file: file, path: file.Name(), rawWidth: 800, rawHeight: 1280,
		frameWidth: 1280, frameHeight: 800}
	events := make(chan TouchEvent, 8)
	if err := device.Run(context.Background(), events); !errors.Is(err, io.EOF) {
		t.Fatalf("reading recorded hybrid touch = %v, want EOF", err)
	}
	want := []TouchEvent{
		{Kind: Down, X: 1000, Y: 699}, {Kind: Up, X: 1000, Y: 699},
		{Kind: Down, X: 1100, Y: 599}, {Kind: Up, X: 1100, Y: 599},
	}
	for index, expected := range want {
		select {
		case got := <-events:
			if got != expected {
				t.Errorf("event %d = %+v, want %+v", index, got, expected)
			}
		default:
			t.Fatalf("missing event %d", index)
		}
	}
}
