package speaker

import (
	"encoding/binary"
	"strings"
)

// MediaTek's I2S0 DL1 path can consume 16 more stereo frames than it removes
// from the ALSA application's ring accounting for every write. Rook exposed
// the defect first: its XRUN interval was about 2.3s at 768 frames, 9.2s at
// 1536, and 16.4s at 2048. Biscuit and Radar exposed the same signature after
// v3 moved them onto direct Linux ALSA: 4.096s at a 1024-frame write.
//
// Expand every affected hardware write by those 16 frames. Linear
// interpolation distributes the 0.33ms correction across the write instead
// of inserting a repeated block at its boundary. Checkers and Crown do not
// exhibit this driver defect and must not use the correction.
const (
	playbackDriftFrames         = 16
	maximumPlaybackInputFrames  = 2048
	maximumPlaybackOutputFrames = maximumPlaybackInputFrames + playbackDriftFrames
)

type playbackDriftCorrection struct {
	buf     [maximumPlaybackOutputFrames * 4]byte
	last    [2]int16
	started bool
}

// needsPlaybackDriftCorrection identifies only the hardware whose direct
// MediaTek DL1 path has shown the measured accounting defect. Checkers and
// Crown use the same session implementation but their playback clocks do not
// drift; correcting them would introduce real skew.
func needsPlaybackDriftCorrection(target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	return target == "biscuit" || target == "radar" || target == "rook"
}

func (r *playbackDriftCorrection) expand(in []byte) []byte {
	inputFrames := len(in) / 4
	if len(in)%4 != 0 || inputFrames <= playbackDriftFrames ||
		inputFrames > maximumPlaybackInputFrames || inputFrames%playbackDriftFrames != 0 {
		return in
	}
	outputFrames := inputFrames + playbackDriftFrames
	step := inputFrames / playbackDriftFrames
	denominator := step + 1
	out := r.buf[:outputFrames*4]
	if !r.started {
		r.last[0] = int16(binary.LittleEndian.Uint16(in[0:2]))
		r.last[1] = int16(binary.LittleEndian.Uint16(in[2:4]))
		r.started = true
	}
	for outFrame := 0; outFrame < outputFrames; outFrame++ {
		// For Rook, 2048 / 2064 reduces to 128 / 129. For a Dot,
		// 1024 / 1040 reduces to 64 / 65. Offset by one output sample
		// so the only cross-write interpolation uses the saved last frame.
		pos := (outFrame+1)*step - denominator
		for channel := 0; channel < 2; channel++ {
			var a, b int32
			var fraction int
			if pos < 0 {
				a = int32(r.last[channel])
				b = int32(int16(binary.LittleEndian.Uint16(in[channel*2:])))
				fraction = step
			} else {
				frame := pos / denominator
				fraction = pos % denominator
				a = int32(int16(binary.LittleEndian.Uint16(in[frame*4+channel*2:])))
				if frame+1 < inputFrames {
					b = int32(int16(binary.LittleEndian.Uint16(in[(frame+1)*4+channel*2:])))
				} else {
					b = a
				}
			}
			value := (a*int32(denominator-fraction) + b*int32(fraction)) / int32(denominator)
			binary.LittleEndian.PutUint16(out[outFrame*4+channel*2:], uint16(int16(value)))
		}
	}
	for channel := 0; channel < 2; channel++ {
		r.last[channel] = int16(binary.LittleEndian.Uint16(in[(inputFrames-1)*4+channel*2:]))
	}
	return out
}
