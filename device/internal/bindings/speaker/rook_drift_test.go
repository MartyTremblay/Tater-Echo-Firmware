package speaker

import (
	"encoding/binary"
	"testing"
)

func TestRookPlaybackDriftExpansion(t *testing.T) {
	const inputFrames = 2048
	var drift playbackDriftCorrection
	input := make([]byte, inputFrames*4)
	for frame := 0; frame < inputFrames; frame++ {
		binary.LittleEndian.PutUint16(input[frame*4:], uint16(int16(1200)))
		negative := int16(-1200)
		binary.LittleEndian.PutUint16(input[frame*4+2:], uint16(negative))
	}
	output := drift.expand(input)
	outputFrames := inputFrames + playbackDriftFrames
	if len(output) != outputFrames*4 {
		t.Fatalf("output length = %d, want %d", len(output), outputFrames*4)
	}
	for frame := 0; frame < outputFrames; frame++ {
		left := int16(binary.LittleEndian.Uint16(output[frame*4:]))
		right := int16(binary.LittleEndian.Uint16(output[frame*4+2:]))
		if left != 1200 || right != -1200 {
			t.Fatalf("frame %d = (%d, %d), want (1200, -1200)", frame, left, right)
		}
	}
	if got := int16(binary.LittleEndian.Uint16(drift.expand(input)[:2])); got != 1200 {
		t.Fatalf("second period first sample = %d, want 1200", got)
	}
}

func TestRookPlaybackDriftPeriodBoundary(t *testing.T) {
	const inputFrames = 2048
	var drift playbackDriftCorrection
	first := make([]byte, inputFrames*4)
	second := make([]byte, inputFrames*4)
	for frame := 0; frame < inputFrames; frame++ {
		value := int16(frame * 8)
		binary.LittleEndian.PutUint16(first[frame*4:], uint16(value))
		binary.LittleEndian.PutUint16(first[frame*4+2:], uint16(value))
		value = int16((inputFrames + frame) * 8)
		binary.LittleEndian.PutUint16(second[frame*4:], uint16(value))
		binary.LittleEndian.PutUint16(second[frame*4+2:], uint16(value))
	}
	outputFrames := inputFrames + playbackDriftFrames
	last := int16(binary.LittleEndian.Uint16(drift.expand(first)[(outputFrames-1)*4:]))
	begin := int16(binary.LittleEndian.Uint16(drift.expand(second)[:2]))
	if last != int16((inputFrames-1)*8) || begin < last || begin > last+8 {
		t.Fatalf("period boundary last=%d begin=%d", last, begin)
	}
}

func TestDotPlaybackDriftExpansion(t *testing.T) {
	const inputFrames = 1024
	var drift playbackDriftCorrection
	input := make([]byte, inputFrames*4)
	for frame := 0; frame < inputFrames; frame++ {
		binary.LittleEndian.PutUint16(input[frame*4:], uint16(int16(900)))
		negative := int16(-900)
		binary.LittleEndian.PutUint16(input[frame*4+2:], uint16(negative))
	}
	output := drift.expand(input)
	wantFrames := inputFrames + playbackDriftFrames
	if len(output) != wantFrames*4 {
		t.Fatalf("output length = %d, want %d", len(output), wantFrames*4)
	}
	for frame := 0; frame < wantFrames; frame++ {
		left := int16(binary.LittleEndian.Uint16(output[frame*4:]))
		right := int16(binary.LittleEndian.Uint16(output[frame*4+2:]))
		if left != 900 || right != -900 {
			t.Fatalf("frame %d = (%d, %d), want (900, -900)", frame, left, right)
		}
	}
}

func TestDotPlaybackDriftPeriodBoundary(t *testing.T) {
	const inputFrames = 1024
	var drift playbackDriftCorrection
	first := make([]byte, inputFrames*4)
	second := make([]byte, inputFrames*4)
	for frame := 0; frame < inputFrames; frame++ {
		value := int16(frame * 8)
		binary.LittleEndian.PutUint16(first[frame*4:], uint16(value))
		binary.LittleEndian.PutUint16(first[frame*4+2:], uint16(value))
		value = int16((inputFrames + frame) * 8)
		binary.LittleEndian.PutUint16(second[frame*4:], uint16(value))
		binary.LittleEndian.PutUint16(second[frame*4+2:], uint16(value))
	}
	outputFrames := inputFrames + playbackDriftFrames
	last := int16(binary.LittleEndian.Uint16(drift.expand(first)[(outputFrames-1)*4:]))
	begin := int16(binary.LittleEndian.Uint16(drift.expand(second)[:2]))
	if last != int16((inputFrames-1)*8) || begin < last || begin > last+8 {
		t.Fatalf("period boundary last=%d begin=%d", last, begin)
	}
}

func TestPlaybackDriftRejectsUnsupportedWrite(t *testing.T) {
	var drift playbackDriftCorrection
	input := make([]byte, 1000*4)
	if output := drift.expand(input); len(output) != len(input) {
		t.Fatalf("unsupported write changed from %d to %d bytes", len(input), len(output))
	}
}

func TestPlaybackDriftCorrectionIsTargetScopedWithoutServerBuild(t *testing.T) {
	for _, target := range []string{"biscuit", "radar", "rook", " BISCUIT ", "Radar"} {
		if !needsPlaybackDriftCorrection(target) {
			t.Errorf("%q was not recognized as a drift-corrected playback target", target)
		}
	}
	for _, target := range []string{"checkers", "crown", "", "unknown"} {
		if needsPlaybackDriftCorrection(target) {
			t.Errorf("%q incorrectly received playback drift correction", target)
		}
	}
}
