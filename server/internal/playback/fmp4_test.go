package playback

import (
	"encoding/binary"
	"testing"
)

func mkbox(typ string, body ...[]byte) []byte {
	n := 8
	for _, b := range body {
		n += len(b)
	}
	out := binary.BigEndian.AppendUint32(nil, uint32(n))
	out = append(out, typ...)
	for _, b := range body {
		out = append(out, b...)
	}
	return out
}

// A copied stream starting before zero (The Fountain: B-frames and an edit list) has a
// negative first decode time, written wrapped. It belongs to segment 0, not the last one.
func TestNegativeFragmentTimeIsSegmentZero(t *testing.T) {
	tfhd := mkbox("tfhd", []byte{0, 0, 0, 0}, binary.BigEndian.AppendUint32(nil, 1))
	neg := int64(-100_000) // -0.083 s at 1.2 MHz
	tfdt := mkbox("tfdt", []byte{1, 0, 0, 0}, binary.BigEndian.AppendUint64(nil, uint64(neg)))
	moof := mkbox("moof", mkbox("traf", tfhd, tfdt))
	got, ok := fragmentTime(moof, 1, 1_200_000)
	if !ok || got > -0.08 || got < -0.09 {
		t.Fatalf("time %v %v", got, ok)
	}
	if k := segmentFor([]float64{0, 13.68, 22.94, 33.28}, got); k != 0 {
		t.Fatalf("segment %d", k)
	}
}
