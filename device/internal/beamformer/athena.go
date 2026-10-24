package beamformer

// Athena's mainline ALSA front end exposes the four TLV320ADC5140 microphones
// as 96 kHz S32_LE. The samples are 24-bit data left-aligned in the 32-bit
// container. This front end forms a conservative omni mix and applies a
// windowed-sinc anti-alias filter while decimating 6:1 into Tater's 16 kHz
// S16 pipeline. Directional steering remains disabled until the physical
// channel order and geometry have been measured on Tater hardware.

import (
	"encoding/binary"
	"math"
)

const (
	athenaChannels = 4
	athenaRatio    = 6
	// TECHO5 measured Athena 5.89 dB below Crown after conversion. Keep the
	// same normalization so Tater's established 16 kHz mic gain sees the level
	// it was tuned against.
	athenaMicBoost = 5.89
	// 192 taps at 96 kHz gives the decimator a 1 ms group delay and roughly
	// 74 dB Blackman stop-band rejection, matching the pinned platform path.
	athenaTaps = 192
)

var athenaLowPass = func() [athenaTaps]float64 {
	var result [athenaTaps]float64
	const cutoff = 0.5 / athenaRatio
	center := float64(athenaTaps-1) / 2
	var sum float64
	for i := range result {
		x := float64(i) - center
		value := 2 * cutoff
		if x != 0 {
			value = math.Sin(2*math.Pi*cutoff*x) / (math.Pi * x)
		}
		angle := 2 * math.Pi * float64(i) / float64(athenaTaps-1)
		value *= 0.42 - 0.5*math.Cos(angle) + 0.08*math.Cos(2*angle)
		result[i] = value
		sum += value
	}
	boost := math.Pow(10, athenaMicBoost/20)
	for i := range result {
		result[i] = result[i] / sum * boost
	}
	return result
}()

type AthenaFrontEnd struct {
	history          [athenaTaps]float64
	phase            int
	clipped          uint64
	clippedByChannel [7]uint64
}

func NewAthena() *AthenaFrontEnd { return &AthenaFrontEnd{} }

func (*AthenaFrontEnd) Lock(bool)                         {}
func (*AthenaFrontEnd) PrepareSpeechLock()                {}
func (*AthenaFrontEnd) LockCurrent(bool)                  {}
func (*AthenaFrontEnd) Unlock()                           {}
func (*AthenaFrontEnd) EchoRefInto([]byte, []byte) []byte { return nil }

func (a *AthenaFrontEnd) Process(raw []byte, _ float64, gain float64) ([]byte, float64) {
	frameBytes := athenaChannels * 4
	frames := len(raw) / frameBytes
	if frames == 0 {
		return nil, -1
	}
	out := make([]byte, 0, (frames/athenaRatio+1)*2)
	for frame := 0; frame < frames; frame++ {
		base := frame * frameBytes
		var mixed float64
		for channel := 0; channel < athenaChannels; channel++ {
			sample := int32(binary.LittleEndian.Uint32(raw[base+channel*4:]))
			mixed += float64(sample) / 65536.0
		}
		mixed /= athenaChannels
		copy(a.history[:], a.history[1:])
		a.history[athenaTaps-1] = mixed
		a.phase++
		if a.phase != athenaRatio {
			continue
		}
		a.phase = 0
		var filtered float64
		for tap, coefficient := range athenaLowPass {
			filtered += coefficient * a.history[athenaTaps-1-tap]
		}
		value := math.Round(filtered * gain)
		if value > math.MaxInt16 {
			value = math.MaxInt16
			a.clipped++
			a.clippedByChannel[0]++
		} else if value < math.MinInt16 {
			value = math.MinInt16
			a.clipped++
			a.clippedByChannel[0]++
		}
		out = binary.LittleEndian.AppendUint16(out, uint16(int16(value)))
	}
	return out, -1
}

func (a *AthenaFrontEnd) ClippedSamples() uint64      { return a.clipped }
func (a *AthenaFrontEnd) ClippedByChannel() [7]uint64 { return a.clippedByChannel }
func (*AthenaFrontEnd) OutputChannel() int            { return 0 }
func (*AthenaFrontEnd) Diagnostics() Diagnostics {
	return Diagnostics{OutputChannel: 0, HealthyMicChannels: athenaChannels, NoiseGain: 1}
}
