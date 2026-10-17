//go:build cgo && (darwin || linux)

package taternative

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/TaterTotterson/Tater-Echo-Firmware/internal/wakeword/microwakeword"
)

// TestTimerAlarmDoesNotTriggerStopModel feeds the exact embedded alarm through
// the real native Stop engine. It is deliberately a stronger signal than the
// microphone normally receives after acoustic attenuation and AEC; even that
// direct waveform must remain below the Stop model's calibrated five-score
// average.
func TestTimerAlarmDoesNotTriggerStopModel(t *testing.T) {
	library := os.Getenv("TATER_MWW_LIBRARY")
	modelPath := os.Getenv("TATER_MWW_STOP_MODEL")
	if library == "" || modelPath == "" {
		t.Skip("set TATER_MWW_LIBRARY and TATER_MWW_STOP_MODEL to run native Stop integration")
	}

	manifestRaw, err := os.ReadFile(filepath.Join("..", "wakeword", "microwakeword", "models", "stop.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := microwakeword.ParseManifest(manifestRaw)
	if err != nil {
		t.Fatal(err)
	}
	model, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := microwakeword.OpenNativeRuntime(library)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := runtime.NewEngine(model, manifest.RuntimeConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	settings := manifest.RuntimeConfig()

	// A raw 16 kHz mono S16LE fixture can be supplied during model calibration
	// to prove a positive utterance as well as the always-on alarm rejection
	// below. It remains optional because recorded speech is not generated during
	// cross-compilation or stored in the firmware repository.
	if fixturePath := os.Getenv("TATER_MWW_STOP_PCM"); fixturePath != "" {
		raw, readErr := os.ReadFile(fixturePath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if len(raw)%2 != 0 {
			t.Fatalf("Stop fixture contains an odd byte count: %d", len(raw))
		}
		fixture := make([]int16, len(raw)/2)
		for index := range fixture {
			fixture[index] = int16(binary.LittleEndian.Uint16(raw[index*2:]))
		}
		scores, pushErr := engine.PushPCM(fixture)
		if pushErr != nil {
			t.Fatal(pushErr)
		}
		maxMean := maximumSlidingMean(scores, settings.SlidingWindow)
		t.Logf("spoken Stop fixture max five-score mean %.3f (threshold %.3f)", maxMean, settings.Threshold)
		if maxMean < settings.Threshold {
			t.Fatalf("spoken Stop fixture was not detected: max five-score mean %.3f < %.3f", maxMean, settings.Threshold)
		}
		if resetErr := engine.Reset(); resetErr != nil {
			t.Fatal(resetErr)
		}
	}

	// Local playback is 48 kHz mono S16. The wake frontend receives 16 kHz,
	// so select each third sample exactly as this tonal regression requires.
	// Run two repeats to cover model state across the quiet listening tail.
	alarm := timerSound()
	samples := make([]int16, 0, len(alarm)/3)
	for repeat := 0; repeat < 2; repeat++ {
		for offset := 0; offset+1 < len(alarm); offset += 6 {
			samples = append(samples, int16(binary.LittleEndian.Uint16(alarm[offset:])))
		}
	}
	scores, err := engine.PushPCM(samples)
	if err != nil {
		t.Fatal(err)
	}
	if len(scores) < settings.SlidingWindow {
		t.Fatalf("alarm produced %d scores, need at least %d", len(scores), settings.SlidingWindow)
	}
	maxMean := maximumSlidingMean(scores, settings.SlidingWindow)
	t.Logf("embedded timer alarm max five-score mean %.3f (threshold %.3f)", maxMean, settings.Threshold)
	if maxMean >= settings.Threshold {
		t.Fatalf("embedded timer alarm triggered Stop: max five-score mean %.3f >= %.3f", maxMean, settings.Threshold)
	}
}

func maximumSlidingMean(scores []float32, window int) float32 {
	if window < 1 || len(scores) < window {
		return 0
	}
	var sum float32
	for _, score := range scores[:window] {
		sum += score
	}
	maximum := sum / float32(window)
	for end := window; end < len(scores); end++ {
		sum += scores[end] - scores[end-window]
		if mean := sum / float32(window); mean > maximum {
			maximum = mean
		}
	}
	return maximum
}
