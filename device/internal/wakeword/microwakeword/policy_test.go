package microwakeword

import (
	"math"
	"testing"
	"time"
)

func closeTo(left, right float32) bool { return math.Abs(float64(left-right)) < 0.0001 }

func TestLivePolicyOverridesFollowEchoSettingsWithoutReplacingCalibration(t *testing.T) {
	overrides := LivePolicyOverrides(" HIGH ", " TV_NEARBY ")

	if overrides.Sensitivity != "high" || overrides.Environment != "tv_nearby" {
		t.Fatalf("scorer overrides = %#v, want high/tv_nearby", overrides)
	}
	if overrides.Threshold != 0 || overrides.SlidingWindow != 0 || overrides.CloseMissThreshold != 0 {
		t.Fatalf("scorer overrides replaced model calibration: %#v", overrides)
	}
}

func TestLivePolicyOverridesRetainEchoDefaults(t *testing.T) {
	overrides := LivePolicyOverrides("", "")
	if overrides.Sensitivity != "normal" || overrides.Environment != "balanced" {
		t.Fatalf("scorer overrides = %#v, want normal/balanced", overrides)
	}
}

func TestDetectionPolicyMatchesTaterRoomProfiles(t *testing.T) {
	balanced := MakeDetectionPolicy("normal", "balanced", 0.97, 5)
	if balanced.Profile != "balanced" || !closeTo(balanced.Threshold, 0.97) ||
		balanced.MinimumActiveWindow != 3 || balanced.Refractory != 1200*time.Millisecond {
		t.Fatalf("balanced policy = %+v", balanced)
	}
	far := MakeDetectionPolicy("high", "far_field", 0.92, 5)
	if far.Profile != "far_field" || far.MinimumActiveWindow != 1 || far.Refractory != 800*time.Millisecond {
		t.Fatalf("far-field policy = %+v", far)
	}
	strict := MakeDetectionPolicy("normal", "strict", 0.90, 5)
	if strict.Profile != "strict" || strict.Threshold < 247.0/255.0 ||
		strict.MinimumActiveWindow != 4 || strict.Refractory != 1600*time.Millisecond {
		t.Fatalf("strict policy = %+v", strict)
	}
	tv := MakeDetectionPolicy("normal", "tv_nearby", 0.97, 5)
	if tv.Profile != "tv_nearby" || !tv.RequireVerification ||
		tv.Threshold > 224.0/255.0+0.0001 || tv.PeakThreshold < 235.0/255.0 ||
		tv.Refractory != 2400*time.Millisecond {
		t.Fatalf("TV-nearby policy = %+v", tv)
	}
}

func TestShadowPolicyRequiresActiveWindowsAndPeak(t *testing.T) {
	engine := &fakeShadowEngine{batches: [][]float32{{0.99, 0.70, 0.70}}}
	policy := DetectionPolicy{
		Profile: "test", Threshold: 0.79, PeakThreshold: 0.95,
		MinimumActiveWindow: 2, MinimumRiseScore: -1, Refractory: time.Second,
	}
	s, err := NewShadowScorerWithPolicy(engine, 3, 0.6, policy, ShadowHooks{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Push(make([]int16, 1280))
	waitShadow(t, "policy window", s.Ready)
	if st := s.Drain(); st.Crossings != 0 {
		t.Fatalf("single active window crossed a two-window policy: %+v", st)
	}
}

func TestTimerStopPolicyUsesCalibratedWindowAverage(t *testing.T) {
	policy := MakeTimerStopDetectionPolicy(0.50)
	if policy.Profile != "timer_stop" || !closeTo(policy.Threshold, 0.50) ||
		policy.MinimumActiveWindow != 0 || policy.RequireVerification ||
		policy.Refractory != 800*time.Millisecond {
		t.Fatalf("timer-stop policy = %+v", policy)
	}

	// The five-window mean is 0.52, but only two individual scores reach 0.50.
	// The normal balanced policy would reject this short utterance; the Stop
	// model's published average-only rule must accept it.
	engine := &fakeShadowEngine{batches: [][]float32{{1.0, 1.0, 0.2, 0.2, 0.2}}}
	crossed := make(chan float32, 1)
	scorer, err := NewShadowScorerWithPolicy(engine, 5, 0, policy, ShadowHooks{
		Cross: func(score float32, _ time.Time) { crossed <- score },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer scorer.Close()
	scorer.Push(make([]int16, 1280))
	select {
	case score := <-crossed:
		if !closeTo(score, 0.52) {
			t.Fatalf("timer-stop crossing score = %.3f, want 0.520", score)
		}
	case <-time.After(time.Second):
		t.Fatal("timer-stop average crossed 0.50 but was not accepted")
	}
}
