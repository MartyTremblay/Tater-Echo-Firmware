// Package screenambient reads the ambient-light sensors fitted to Tater's
// screened Echo targets. Platform discovery lives in sensor_linux.go; the
// conversion policy stays here so it can be tested on any build host.
package screenambient

import "math"

// Reader supplies lux samples. ok is false for a deliberately discarded
// sample, such as the first conversion after an automatic gain change.
type Reader interface {
	Name() string
	Sample() (lux float64, ok bool, err error)
	Close() error
}

type colorCounts struct{ clear, red, green, blue float64 }

const (
	luxDGF   = 446.0
	luxRCoef = 0.090
	luxGCoef = 1.000
	luxBCoef = -0.460
)

// tcs3400Lux follows the stock Athena driver's built-in conversion. The
// coefficients are useful for auto-brightness even though the finished
// device's dark glass has not yet been photometrically calibrated.
func tcs3400Lux(n colorCounts, gain, integrationSeconds float64, topGain bool) float64 {
	ir := 0.0
	if !topGain {
		ir = math.Max(0, math.Floor((n.red+n.green+n.blue-n.clear+1)/2))
	}
	countsPerLux := integrationSeconds * 1000 * gain / luxDGF
	if countsPerLux <= 0 {
		return 0
	}
	lux := (luxRCoef*(n.red-ir) + luxGCoef*(n.green-ir) + luxBCoef*(n.blue-ir)) / countsPerLux
	return math.Max(0, lux)
}

func tcsMaxCount(integrationSeconds float64) float64 {
	cycles := math.Round(integrationSeconds / 0.00278)
	return math.Min(cycles*1024, 65535)
}

func nextTCSGain(clear float64, gains []int, current int, maximum float64) int {
	if len(gains) == 0 || current < 0 || current >= len(gains) {
		return current
	}
	high := 0.8 * maximum
	switch {
	case clear >= high && current > 0:
		return current - 1
	case current < len(gains)-1 && clear*float64(gains[current+1])/float64(gains[current]) < high/2:
		return current + 1
	default:
		return current
	}
}
