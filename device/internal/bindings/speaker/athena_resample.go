package speaker

import (
	"encoding/binary"
	"math"
)

const athenaUpsampleTaps = 48

// Athena's TAS5805M tuning is authored for 96 kHz. This Blackman-windowed
// polyphase filter converts Tater's already mixed and limited 48 kHz stereo
// stream without the treble droop and ultrasonic images of straight-line
// interpolation. Its 96 coefficients give about 74 dB stop-band rejection
// and 1 ms of delay, matching the pinned platform implementation.
var athenaUpsamplePhases = func() [2][athenaUpsampleTaps]float64 {
	const coefficients = 2 * athenaUpsampleTaps
	var lowpass [coefficients]float64
	middle := float64(coefficients-1) / 2
	var sum float64
	for i := range lowpass {
		x := float64(i) - middle
		value := 0.5 // 2 * cutoff, where cutoff is 0.25 at the 96 kHz output rate.
		if x != 0 {
			value = math.Sin(0.5*math.Pi*x) / (math.Pi * x)
		}
		angle := 2 * math.Pi * float64(i) / float64(coefficients-1)
		value *= 0.42 - 0.5*math.Cos(angle) + 0.08*math.Cos(2*angle)
		lowpass[i] = value
		sum += value
	}
	var phases [2][athenaUpsampleTaps]float64
	for phase := range phases {
		for tap := range phases[phase] {
			// Upsampling inserts one zero between samples, so the filter needs a
			// DC gain of two. Reverse each phase to run against oldest-first history.
			phases[phase][athenaUpsampleTaps-1-tap] = lowpass[phase+tap*2] / sum * 2
		}
	}
	return phases
}()

type stereo2x struct {
	history [2][athenaUpsampleTaps * 2]float64
	at      int
}

func (u *stereo2x) expand(in []byte) []byte {
	if len(in) < 4 {
		return nil
	}
	out := make([]byte, 0, len(in)*2)
	for frame := 0; frame+4 <= len(in); frame += 4 {
		for channel := range u.history {
			sample := float64(int16(binary.LittleEndian.Uint16(in[frame+channel*2:])))
			u.history[channel][u.at] = sample
			u.history[channel][u.at+athenaUpsampleTaps] = sample
		}
		u.at++
		if u.at == athenaUpsampleTaps {
			u.at = 0
		}
		for phase := range athenaUpsamplePhases {
			for channel := range u.history {
				var sample float64
				for tap, coefficient := range athenaUpsamplePhases[phase] {
					sample += coefficient * u.history[channel][u.at+tap]
				}
				sample = math.Round(sample)
				sample = min(max(sample, math.MinInt16), math.MaxInt16)
				out = binary.LittleEndian.AppendUint16(out, uint16(int16(sample)))
			}
		}
	}
	return out
}
