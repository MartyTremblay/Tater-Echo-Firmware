package client

import (
	"testing"
	"time"
)

func TestWakeBeamLatchFollowsIdleThenHoldsActiveUtterance(t *testing.T) {
	latch := newWakeBeamLatch()
	now := time.Now()

	if direction, latched, changed := latch.selectDirection(1, true, false, now); direction != 1 || latched || changed {
		t.Fatalf("idle selection = (%d, %t, %t), want adaptive direction 1", direction, latched, changed)
	}
	if direction, latched, _ := latch.selectDirection(2, true, true, now.Add(160*time.Millisecond)); direction != 2 || latched {
		t.Fatalf("first active selection = (%d, %t), want unlatched direction 2", direction, latched)
	}
	if direction, latched, changed := latch.selectDirection(2, true, true, now.Add(320*time.Millisecond)); direction != 2 || !latched || !changed {
		t.Fatalf("confirmed selection = (%d, %t, %t), want new latch on direction 2", direction, latched, changed)
	}
	if direction, latched, changed := latch.selectDirection(5, true, true, now.Add(time.Second)); direction != 2 || !latched || changed {
		t.Fatalf("moving estimator changed utterance path: (%d, %t, %t)", direction, latched, changed)
	}
}

func TestWakeBeamLatchExpiresToAdaptiveOrOmni(t *testing.T) {
	latch := newWakeBeamLatch()
	now := time.Now()
	latch.selectDirection(3, true, true, now)
	latch.selectDirection(3, true, true, now.Add(160*time.Millisecond))

	after := now.Add(160*time.Millisecond + wakeBeamLatchDuration + time.Millisecond)
	if direction, latched, changed := latch.selectDirection(4, true, false, after); direction != 4 || latched || !changed {
		t.Fatalf("expired selection = (%d, %t, %t), want adaptive direction 4", direction, latched, changed)
	}
	if direction, latched, _ := latch.selectDirection(-1, false, false, after.Add(time.Millisecond)); direction != -1 || latched {
		t.Fatalf("uncertain selection = (%d, %t), want omni fallback", direction, latched)
	}
}

func TestWakeBeamLatchReleasesAfterUtteranceSilence(t *testing.T) {
	latch := newWakeBeamLatch()
	now := time.Now()
	latch.selectDirection(3, true, true, now)
	latch.selectDirection(3, true, true, now.Add(160*time.Millisecond))
	for batch := 1; batch < wakeBeamLatchReleaseBatches; batch++ {
		if direction, latched, _ := latch.selectDirection(4, true, false, now.Add(time.Duration(batch+1)*160*time.Millisecond)); direction != 3 || !latched {
			t.Fatalf("latch released after only %d quiet batches", batch)
		}
	}
	if direction, latched, changed := latch.selectDirection(4, true, false, now.Add(time.Duration(wakeBeamLatchReleaseBatches+1)*160*time.Millisecond)); direction != 4 || latched || !changed {
		t.Fatalf("post-utterance selection = (%d, %t, %t), want adaptive direction 4", direction, latched, changed)
	}
}
