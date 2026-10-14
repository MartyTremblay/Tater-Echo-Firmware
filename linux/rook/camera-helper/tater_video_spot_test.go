//go:build spot

package camera

import (
	"math"
	"testing"
)

func TestRenderTaterEchoVideoUsesCallSizeAndReusesBuffer(t *testing.T) {
	raw := make([]byte, sensorW*sensorH)
	for index := range raw {
		raw[index] = uint8(index)
	}
	tone := tone{gainR: 1, gainB: 1, black: 0, white: 255}
	first := renderTaterEchoVideo(raw, tone, nil)
	if first == nil {
		t.Fatal("direct Rook video is nil")
	}
	if first.Bounds().Dx() != 320 || first.Bounds().Dy() != 180 {
		t.Fatalf("direct Rook video = %v, want 320x180", first.Bounds())
	}
	if first.RGBAAt(160, 90).A != 255 {
		t.Fatal("direct Rook video is not opaque")
	}
	second := renderTaterEchoVideo(raw, tone, first)
	if second != first {
		t.Fatal("direct Rook video did not reuse its frame buffer")
	}
}

func TestTaterVideoLUTMatchesCameraGammaCurve(t *testing.T) {
	for _, level := range []tone{
		{gainR: 1, gainB: 1, black: 0, white: 255},
		{gainR: 1.7, gainB: 0.8, black: 12, white: 210},
	} {
		for _, gain := range []float64{level.gainR, 1, level.gainB} {
			want := level.lut(gain)
			got := taterVideoLUT(level, gain)
			for index := range got {
				if delta := math.Abs(float64(got[index]) - float64(want[index])); delta > 1 {
					t.Fatalf("tone=%+v gain=%v index=%d fast=%d reference=%d", level, gain, index, got[index], want[index])
				}
			}
		}
	}
}

func TestRenderTaterEchoVideoRejectsShortRawFrame(t *testing.T) {
	if got := renderTaterEchoVideo(make([]byte, sensorW*sensorH-1), tone{}, nil); got != nil {
		t.Fatalf("short raw frame produced %v", got.Bounds())
	}
}
