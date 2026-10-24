package beamformer

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestAthenaFrontEndDecimatesFourS32Channels(t *testing.T) {
	front := NewAthena()
	raw := make([]byte, athenaRatio*athenaChannels*4*80)
	for frame := 0; frame < len(raw)/(athenaChannels*4); frame++ {
		for channel := 0; channel < athenaChannels; channel++ {
			binary.LittleEndian.PutUint32(raw[(frame*athenaChannels+channel)*4:], uint32(int32(4096<<16)))
		}
	}
	mono, angle := front.Process(raw, 0, 1)
	if len(mono) != 80*2 || angle != -1 {
		t.Fatalf("Athena output = %d bytes, angle %v", len(mono), angle)
	}
	got := int16(binary.LittleEndian.Uint16(mono[len(mono)-2:]))
	want := 4096 * math.Pow(10, athenaMicBoost/20)
	if math.Abs(float64(got)-want) > 2 {
		t.Fatalf("settled sample = %d, want %.0f", got, want)
	}
	if front.EchoRefInto(raw, nil) != nil {
		t.Fatal("Athena must retain the software echo reference until paired capture is implemented")
	}
}

func TestAthenaSelectedByTarget(t *testing.T) {
	if _, ok := NewForTarget(" ATHENA ").(*AthenaFrontEnd); !ok {
		t.Fatalf("Athena target selected %T", NewForTarget("athena"))
	}
}
