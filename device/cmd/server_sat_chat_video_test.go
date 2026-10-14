package main

import (
	"testing"
	"time"
)

func TestSatChatVideoCaptureDelayTargetsTenFramesPerSecond(t *testing.T) {
	if got := satChatVideoCaptureDelay(40*time.Millisecond, true); got != 60*time.Millisecond {
		t.Fatalf("delay after fast capture = %v, want 60ms", got)
	}
	if got := satChatVideoCaptureDelay(250*time.Millisecond, true); got != satChatVideoCaptureYield {
		t.Fatalf("delay after slow capture = %v, want yield %v", got, satChatVideoCaptureYield)
	}
	if got := satChatVideoCaptureDelay(time.Millisecond, false); got != satChatVideoErrorBackoff {
		t.Fatalf("delay after failed capture = %v, want %v", got, satChatVideoErrorBackoff)
	}
}
