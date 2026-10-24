package screenambient

import (
	"math"
	"testing"
)

func TestTCS3400ConversionAndGainSelection(t *testing.T) {
	counts := colorCounts{clear: 1000, red: 500, green: 700, blue: 200}
	lux := tcs3400Lux(counts, 16, .278, false)
	if lux <= 0 || math.IsNaN(lux) || math.IsInf(lux, 0) {
		t.Fatalf("converted lux = %v", lux)
	}
	gains := []int{1, 4, 16, 64}
	if got := nextTCSGain(65535, gains, 2, 65535); got != 1 {
		t.Fatalf("saturated gain index = %d, want 1", got)
	}
	if got := nextTCSGain(10, gains, 1, 65535); got != 2 {
		t.Fatalf("dark gain index = %d, want 2", got)
	}
}

func TestTCS3400ConversionNeverReportsNegativeLux(t *testing.T) {
	counts := colorCounts{clear: 100, red: 5, green: 5, blue: 100}
	if got := tcs3400Lux(counts, 64, .278, true); got < 0 {
		t.Fatalf("blue-heavy sample produced negative lux: %v", got)
	}
}
