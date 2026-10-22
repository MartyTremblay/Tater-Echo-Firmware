//go:build server

package speaker

import "testing"

func TestLinuxDotTargetsUseTheSharedDirectPlaybackPath(t *testing.T) {
	for _, target := range []string{"biscuit", "radar", " BISCUIT ", "Radar"} {
		if !isLinuxDotPlaybackTarget(target) {
			t.Errorf("%q was not recognized as a Linux Dot playback target", target)
		}
	}
	for _, target := range []string{"checkers", "crown", "rook", ""} {
		if isLinuxDotPlaybackTarget(target) {
			t.Errorf("%q incorrectly entered the Linux Dot playback path", target)
		}
	}
}

func TestPlaybackDriftCorrectionIsTargetScoped(t *testing.T) {
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
