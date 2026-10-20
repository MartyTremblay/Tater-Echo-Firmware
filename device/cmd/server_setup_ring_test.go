package main

import "testing"

func TestSetupRingFrameIsWarmWhiteTwinkle(t *testing.T) {
	levels := map[[3]uint8]bool{
		{9, 8, 7}:       true,
		{50, 45, 36}:    true,
		{122, 109, 86}:  true,
		{242, 215, 171}: true,
	}
	for tick := -31; tick <= 31; tick++ {
		frame := setupRingTwinkleFrame(tick)
		if len(frame) != 12 {
			t.Fatalf("frame length = %d, want 12", len(frame))
		}
		for index, pixel := range frame {
			if pixel.ID != index {
				t.Fatalf("pixel %d ID = %d", index, pixel.ID)
			}
			colour := [3]uint8{pixel.R, pixel.G, pixel.B}
			if !levels[colour] {
				t.Fatalf("tick %d pixel %d has unexpected colour: %+v", tick, index, pixel)
			}
		}
	}
}

func TestSetupRingTwinkleRepeatsEvery31Ticks(t *testing.T) {
	first := setupRingTwinkleFrame(0)
	repeated := setupRingTwinkleFrame(31)
	for index := range first {
		if first[index] != repeated[index] {
			t.Fatalf("pixel %d does not repeat: %+v != %+v", index, first[index], repeated[index])
		}
	}
}
