// Package displaycontrol contains the hardware-independent policy for Echo
// screen brightness and scheduled night mode.
package displaycontrol

import (
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	BacklightMax = 255
	showFloor    = 4
	spotFloor    = 120
	darkLux      = 2.0
	brightLux    = 400.0
	smooth       = 0.25
)

// Config is the per-satellite display policy sent by Tater.
type Config struct {
	Brightness      int
	Auto            bool
	AutoMinimum     int
	NightEnabled    bool
	NightBrightness int
	NightStart      string
	NightEnd        string
	NightDisplay    string
}

// NightActive reports whether local is inside the configured daily window.
// Equal start and end times are treated as disabled instead of as an
// accidental all-day blackout.
func NightActive(local time.Time, start, end string) bool {
	startMinute, startOK := clockMinute(start)
	endMinute, endOK := clockMinute(end)
	if !startOK || !endOK || startMinute == endMinute {
		return false
	}
	now := local.Hour()*60 + local.Minute()
	if startMinute < endMinute {
		return now >= startMinute && now < endMinute
	}
	return now >= startMinute || now < endMinute
}

func clockMinute(value string) (int, bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, false
	}
	hour, hourErr := strconv.Atoi(parts[0])
	minute, minuteErr := strconv.Atoi(parts[1])
	if hourErr != nil || minuteErr != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, false
	}
	return hour*60 + minute, true
}

// TargetLevel returns a logical backlight level from 0 through BacklightMax.
// Checkers, Crown, and Athena use the panel's full range. Rook's aging round
// panel has a much narrower readable daytime range, so normal brightness maps
// above its measured floor while night mode may deliberately go below it.
func TargetLevel(config Config, target string, lux float64, haveLux, night bool) float64 {
	if night && config.NightEnabled {
		if normalizeNightDisplay(config.NightDisplay) == "off" {
			return 0
		}
		return percentLevel(config.NightBrightness, 1)
	}

	ceiling := clampPercent(config.Brightness)
	if ceiling == 0 {
		return 0
	}
	if strings.EqualFold(strings.TrimSpace(target), "rook") {
		full := spotFloor + float64(BacklightMax-spotFloor)*float64(ceiling)/100
		if config.Auto && haveLux {
			// Rook's usable hardware range is already protected by spotFloor.
			// Apply the user's automatic minimum inside that safe range while
			// retaining the selected brightness as the ceiling.
			minimum := float64(clamp(config.AutoMinimum, 1, 50)) / 100
			fraction := minimum + (1-minimum)*logFraction(lux)
			full = spotFloor + (full-spotFloor)*fraction
		}
		return clampLevel(full)
	}

	level := float64(ceiling) * BacklightMax / 100
	if config.Auto && haveLux {
		minimum := float64(clamp(config.AutoMinimum, 1, 50)) / 100
		level *= minimum + (1-minimum)*logFractionFrom(lux, darkLux)
	}
	return math.Max(float64(showFloor), clampLevel(level))
}

func normalizeNightDisplay(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "dim", "red_clock", "off":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "red_clock"
	}
}

func percentLevel(percent, nonzeroFloor int) float64 {
	percent = clampPercent(percent)
	if percent == 0 {
		return 0
	}
	return math.Max(float64(nonzeroFloor), float64(percent)*BacklightMax/100)
}

func logFraction(lux float64) float64 {
	return math.Min(math.Max(math.Log10(1+math.Max(lux, 0))/math.Log10(1+brightLux), 0), 1)
}

func logFractionFrom(lux, floorLux float64) float64 {
	fraction := math.Log10(math.Max(lux, floorLux)/floorLux) / math.Log10(brightLux/floorLux)
	return math.Min(math.Max(fraction, 0), 1)
}

// SmoothStep takes one half-second-sized step toward target. It settles small
// differences exactly so integer backlight writes do not oscillate forever.
func SmoothStep(current, target float64) float64 {
	current, target = clampLevel(current), clampLevel(target)
	if math.Abs(target-current) <= 1 {
		return target
	}
	return current + (target-current)*smooth
}

func clampLevel(value float64) float64 {
	return math.Min(math.Max(value, 0), BacklightMax)
}

func clampPercent(value int) int { return clamp(value, 0, 100) }

func clamp(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
