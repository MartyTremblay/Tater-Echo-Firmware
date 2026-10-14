package main

import (
	"testing"
	"time"
)

func TestEchoStreamTargetsTwentyFramesPerSecond(t *testing.T) {
	if got := cameraStreamPeriod(true); got != time.Second/20 {
		t.Fatalf("echo stream period = %v, want %v", got, time.Second/20)
	}
	if got := cameraStreamPeriod(false); got != time.Second/15 {
		t.Fatalf("normal stream period = %v, want %v", got, time.Second/15)
	}
}
