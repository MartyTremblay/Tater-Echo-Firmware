package displaycontrol

import (
	"math"
	"testing"
	"time"
)

func TestNightActiveSupportsOvernightAndDaytimeWindows(t *testing.T) {
	for _, test := range []struct {
		hour, minute int
		start, end   string
		want         bool
	}{
		{23, 0, "22:00", "07:00", true},
		{6, 59, "22:00", "07:00", true},
		{7, 0, "22:00", "07:00", false},
		{14, 0, "09:00", "17:00", true},
		{8, 59, "09:00", "17:00", false},
		{12, 0, "12:00", "12:00", false},
		{12, 0, "bad", "07:00", false},
	} {
		at := time.Date(2026, time.October, 24, test.hour, test.minute, 0, 0, time.FixedZone("local", -5*3600))
		if got := NightActive(at, test.start, test.end); got != test.want {
			t.Errorf("NightActive(%02d:%02d, %q, %q) = %t, want %t", test.hour, test.minute, test.start, test.end, got, test.want)
		}
	}
}

func TestShowAutoBrightnessUsesCeilingAndDarkFloor(t *testing.T) {
	config := Config{Brightness: 80, Auto: true, AutoMinimum: 10}
	dark := TargetLevel(config, "athena", 0, true, false)
	bright := TargetLevel(config, "athena", 400, true, false)
	wantBright := .8 * BacklightMax
	if math.Abs(dark-.1*wantBright) > .01 {
		t.Fatalf("dark level = %.2f, want %.2f", dark, .1*wantBright)
	}
	if math.Abs(bright-wantBright) > .01 {
		t.Fatalf("bright level = %.2f, want %.2f", bright, wantBright)
	}
	if noReading := TargetLevel(config, "athena", 0, false, false); math.Abs(noReading-wantBright) > .01 {
		t.Fatalf("missing sensor did not fall back to manual ceiling: %.2f", noReading)
	}
}

func TestRookUsesReadableDayRangeButAllowsDimNightClock(t *testing.T) {
	config := Config{Brightness: 1, NightEnabled: true, NightBrightness: 2, NightDisplay: "red_clock"}
	if day := TargetLevel(config, "rook", 0, false, false); day < spotFloor {
		t.Fatalf("Rook daytime level %.2f is below readable floor %d", day, spotFloor)
	}
	if night := TargetLevel(config, "rook", 0, false, true); night >= spotFloor || night <= 0 {
		t.Fatalf("Rook night level %.2f does not use the low night range", night)
	}
}

func TestRookAutoBrightnessHonorsMinimumInsideSafeRange(t *testing.T) {
	config := Config{Brightness: 100, Auto: true, AutoMinimum: 20}
	got := TargetLevel(config, "rook", 0, true, false)
	want := float64(spotFloor) + float64(BacklightMax-spotFloor)*.2
	if math.Abs(got-want) > .01 {
		t.Fatalf("dark Rook level = %.2f, want %.2f", got, want)
	}
	if bright := TargetLevel(config, "rook", 400, true, false); math.Abs(bright-BacklightMax) > .01 {
		t.Fatalf("bright Rook level = %.2f, want %d", bright, BacklightMax)
	}
}

func TestNightOffAndZeroBrightnessTurnBacklightOff(t *testing.T) {
	if got := TargetLevel(Config{Brightness: 0}, "checkers", 100, true, false); got != 0 {
		t.Fatalf("manual zero = %.2f", got)
	}
	config := Config{Brightness: 80, NightEnabled: true, NightBrightness: 25, NightDisplay: "off"}
	if got := TargetLevel(config, "checkers", 100, true, true); got != 0 {
		t.Fatalf("night off = %.2f", got)
	}
}

func TestSmoothStepConvergesWithoutOvershoot(t *testing.T) {
	level := 200.0
	for range 40 {
		level = SmoothStep(level, 10)
		if level < 10 || level > 200 {
			t.Fatalf("smooth level escaped bounds: %.2f", level)
		}
	}
	if level != 10 {
		t.Fatalf("smooth level did not settle: %.2f", level)
	}
}
