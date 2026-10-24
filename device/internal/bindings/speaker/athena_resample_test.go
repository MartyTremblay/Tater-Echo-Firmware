package speaker

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestAthenaStereoUpsamplerCarriesPeriodBoundary(t *testing.T) {
	frame := func(left, right int16) []byte {
		out := make([]byte, 4)
		binary.LittleEndian.PutUint16(out, uint16(left))
		binary.LittleEndian.PutUint16(out[2:], uint16(right))
		return out
	}
	var input []byte
	for i := 0; i < 160; i++ {
		input = append(input, frame(int16(900+i), int16(-900-i))...)
	}
	var whole stereo2x
	want := whole.expand(input)
	var chunked stereo2x
	got := append(chunked.expand(input[:51*4]), chunked.expand(input[51*4:])...)
	if len(got) != len(input)*2 {
		t.Fatalf("expanded size = %d, want %d", len(got), len(input)*2)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("period boundary changed filtered output")
	}
}

func TestAthenaStereoUpsamplerSettlesAtUnityGain(t *testing.T) {
	frame := make([]byte, 4)
	leftInput, rightInput := int16(1200), int16(-1200)
	binary.LittleEndian.PutUint16(frame, uint16(leftInput))
	binary.LittleEndian.PutUint16(frame[2:], uint16(rightInput))
	input := bytes.Repeat(frame, athenaUpsampleTaps*4)
	var up stereo2x
	out := up.expand(input)
	left := int16(binary.LittleEndian.Uint16(out[len(out)-8:]))
	right := int16(binary.LittleEndian.Uint16(out[len(out)-6:]))
	if left < 1198 || left > 1202 || right < -1202 || right > -1198 {
		t.Fatalf("settled stereo = %d, %d; want 1200, -1200", left, right)
	}
}
