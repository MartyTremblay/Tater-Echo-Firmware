// Package clock sets the device's wall clock from Tater.
//
// An Echo has no RTC that survives a power cut and boots reading 2010. On the
// native Linux targets nothing else owns this clock sync. Pointing ntpd at the
// gateway works only on networks whose router happens to serve NTP.
//
// So Tater tells us instead, over the authenticated outbound link the
// device already trusts. No DNS, no listening socket, no dependency on
// anything outside the user's own network.
//
// **This does not affect any of the project's timing instrumentation, and must
// not.** The firmware never sends a timestamp: RTT, wake-crossing ages and
// stall gaps are all measured with time.Since against Go's monotonic clock,
// which settimeofday does not perturb — a time.Time from time.Now() carries a
// monotonic reading and Sub uses it. Fixing the wall clock makes log lines
// correlatable with Tater's; it changes no measurement.
//
// It also does not affect TLS. The device clamps its verification clock to the
// firmware build time precisely because it cannot trust its own clock at dial
// time, and that clamp stays: this runs after a connection exists, so it can
// never be what a connection depends on.
package clock

import (
	"strconv"
	"time"
)

// BuildUnix is the firmware build timestamp, injected by the Linux build. It is a
// trustworthy lower bound for TLS verification when an Echo boots with its
// wall clock reset to 2010.
var BuildUnix = ""

// VerificationTime clamps an untrusted device clock to the firmware build
// time. Public HTTPS downloads need this before Tater's handshake can correct
// CLOCK_REALTIME from its own timestamp.
func VerificationTime(now time.Time) time.Time {
	if BuildUnix == "" {
		return now
	}
	seconds, err := strconv.ParseInt(BuildUnix, 10, 64)
	if err != nil {
		return now
	}
	built := time.Unix(seconds, 0)
	if now.Before(built) {
		return built
	}
	return now
}

// VerificationNow returns the clock used for TLS certificate validity.
func VerificationNow() time.Time {
	return VerificationTime(time.Now())
}

// StepThreshold is how wrong the clock has to be before we touch it.
//
// Sized against the link, not against ambition. The ack crosses a network
// measured at 1-2s RTT excursions on this fleet and carries no delay
// compensation, so the answer is good to about a second and no better —
// stepping for a sub-second disagreement would be churn that cannot even be
// verified. It is also comfortably below the fault this exists for, which is a
// clock reading fifteen years out.
//
// The generous threshold also prevents needless clock churn after another
// trusted source has already corrected the device.
const StepThreshold = 30 * time.Second

// ShouldStep says whether `serverUnixMs` is far enough from the device's own
// clock to be worth applying. Pure, so the rule is testable without root.
//
// A zero or negative server time means Tater did not send one — an older
// server, or a field that did not survive a round trip. That is
// "no opinion", never "the epoch", and must leave the clock alone: setting a
// device to 1970 is strictly worse than leaving it in 2010, because it is a
// value that looks deliberate.
func ShouldStep(deviceNow time.Time, serverUnixMs int64) bool {
	if serverUnixMs <= 0 {
		return false
	}
	drift := time.UnixMilli(serverUnixMs).Sub(deviceNow)
	if drift < 0 {
		drift = -drift
	}
	return drift >= StepThreshold
}
