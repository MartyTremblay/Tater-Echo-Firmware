package client

import "time"

const (
	// Two consecutive active capture batches reject a one-batch reflection or
	// noise excursion while still latching near the beginning of speech.
	wakeBeamLatchConfirmations = 2
	// Four quiet batches release the latch between utterances at the current
	// 160ms ALSA cadence. The timeout is a backstop for continuous room audio.
	wakeBeamLatchReleaseBatches = 4
	// Five seconds covers a normal phrase, the full 1.2s dual-detector
	// agreement window, and the verifier's configurable two-second timeout.
	wakeBeamLatchDuration = 5 * time.Second
)

// wakeBeamLatch turns the free-running array estimator into an
// utterance-scoped acoustic view. It never resets or replays a neural model:
// MWW and OWW keep consuming one continuous stream, but that stream stops
// changing direction once near-end speech establishes a credible bearing.
type wakeBeamLatch struct {
	direction     int
	candidate     int
	confirmations int
	quietBatches  int
	until         time.Time
}

func newWakeBeamLatch() wakeBeamLatch {
	return wakeBeamLatch{direction: -1, candidate: -1}
}

func (l *wakeBeamLatch) reset() {
	l.direction = -1
	l.candidate = -1
	l.confirmations = 0
	l.quietBatches = 0
	l.until = time.Time{}
}

// selectDirection returns the acoustic path to form for this batch. Before a
// latch it follows the estimator exactly. Once active speech confirms one
// direction it holds that path, falling back to adaptive/omni after timeout.
func (l *wakeBeamLatch) selectDirection(
	observed int,
	observedOK bool,
	active bool,
	now time.Time,
) (direction int, latched bool, changed bool) {
	if l.direction >= 0 {
		if active {
			l.quietBatches = 0
		} else {
			l.quietBatches++
		}
		if now.Before(l.until) && l.quietBatches < wakeBeamLatchReleaseBatches {
			return l.direction, true, false
		}
		l.reset()
		changed = true
	}

	if !observedOK || observed < 0 {
		l.candidate = -1
		l.confirmations = 0
		return -1, false, changed
	}
	if !active {
		l.candidate = -1
		l.confirmations = 0
		return observed, false, changed
	}

	if l.candidate == observed {
		l.confirmations++
	} else {
		l.candidate = observed
		l.confirmations = 1
	}
	if l.confirmations < wakeBeamLatchConfirmations {
		return observed, false, changed
	}

	l.direction = observed
	l.until = now.Add(wakeBeamLatchDuration)
	return l.direction, true, true
}
