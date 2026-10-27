//go:build linux

package main

import (
	"image"
	"testing"

	"github.com/TaterTotterson/Tater-Echo-Firmware/internal/show"
)

func TestCameraStreamIdentityRequiresSelectedStream(t *testing.T) {
	size := image.Pt(896, 672)
	if got := cameraStreamIdentity(nil, size); got != "" {
		t.Fatalf("nil camera identity = %q", got)
	}
	if got := cameraStreamIdentity(&show.Cameras{SelectedID: "front"}, size); got != "" {
		t.Fatalf("snapshot-only camera identity = %q", got)
	}
	got := cameraStreamIdentity(&show.Cameras{SelectedID: "front", StreamURL: "https://ha.invalid/live"}, size)
	if got == "" {
		t.Fatal("stream-capable camera has no identity")
	}
}

func TestCameraStreamControllerRecyclesSupersededFrames(t *testing.T) {
	controller := newCameraStreamController()
	first := image.NewRGBA(image.Rect(0, 0, 16, 16))
	second := image.NewRGBA(image.Rect(0, 0, 16, 16))
	controller.publish(cameraStreamResult{identity: "one", image: first})
	controller.publish(cameraStreamResult{identity: "one", image: second})
	got := <-controller.frames
	if got.image != second {
		t.Fatal("latest stream frame was not retained")
	}
	select {
	case recycled := <-controller.recycle:
		if recycled != first {
			t.Fatal("wrong superseded frame was recycled")
		}
	default:
		t.Fatal("superseded frame was not recycled")
	}
}

func TestSatChatVideoUsesTheVideoCoreFloor(t *testing.T) {
	state := show.Snapshot{SatChat: &show.SatChat{Call: &show.SatChatCall{State: "active", Mode: "video"}}}
	if !satChatVideoActive(state) {
		t.Fatal("active Sat Chat video was not recognized")
	}
	state.SatChat.Call.Mode = "audio"
	if satChatVideoActive(state) {
		t.Fatal("audio-only Sat Chat requested the video core floor")
	}
}

func TestNativeCameraRelayUsesTheVideoCoreFloor(t *testing.T) {
	state := show.Snapshot{Cameras: &show.Cameras{SelectedID: "show-kitchen", StreamActive: true}}
	if !cameraVideoActive(state) {
		t.Fatal("active native camera stream was not recognized")
	}
	state.Cameras.StreamActive = false
	if cameraVideoActive(state) {
		t.Fatal("snapshot-only camera requested the video core floor")
	}
}
