package main

import (
	"image"
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

func TestParseMuteStateFailsClosed(t *testing.T) {
	for _, test := range []struct {
		name, data string
		want       bool
		wantError  bool
	}{
		{"unmuted", `{"muted":false}`, false, false},
		{"muted", `{"muted":true}`, true, false},
		{"missing", `{}`, false, true},
		{"invalid", `{`, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseMuteState([]byte(test.data))
			if got != test.want || (err != nil) != test.wantError {
				t.Fatalf("parseMuteState(%q) = %t, %v", test.data, got, err)
			}
		})
	}
}

func TestEchoVideoImageCapsLargeFramesWithoutUpscalingSmallCamera(t *testing.T) {
	for _, test := range []struct {
		name   string
		source image.Point
		want   image.Point
	}{
		{name: "large", source: image.Pt(800, 600), want: image.Pt(400, 225)},
		{name: "native Rook", source: image.Pt(320, 240), want: image.Pt(320, 180)},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := echoVideoImage(image.NewRGBA(image.Rectangle{Max: test.source}), nil)
			if got.Bounds().Size() != test.want {
				t.Fatalf("echo video size = %v, want %v", got.Bounds().Size(), test.want)
			}
		})
	}
}
