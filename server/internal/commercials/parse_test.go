package commercials

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseEDL(t *testing.T) {
	in := "0.00\t3.50\t0\n" + // a cut at the very start
		"  612.45   790.12  3 \n" + // a commercial break, odd spacing
		"900\t905\t1\n" + // mute: not a break
		"1000\t1001\t2\n" + // scene marker: not a break
		"garbage line\n" +
		"1200.5\n" + // too few fields
		"1500.25\t1680.75\n" + // no type: a cut
		"NaN\t10\t0\n-5\t10\t0\n"
	got, err := ParseEDL(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []Break{{0, 3500}, {612450, 790120}, {1500250, 1680750}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, _ := ParseEDL(strings.NewReader("")); len(got) != 0 {
		t.Errorf("empty: %v", got)
	}
}

func TestParseTXT(t *testing.T) {
	in := "FILE PROCESSING COMPLETE  107892 FRAMES AT  2997\n-------------------\n" +
		"1\t899\n" +
		"18352\t23686\n" +
		"bad\tline\n"
	got, err := ParseTXT(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []Break{{33, 29997}, {612346, 790324}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// 25 fps (PAL) and no breaks.
	got, err = ParseTXT(strings.NewReader("FILE PROCESSING COMPLETE 45000 FRAMES AT 2500\n-------------------\n"))
	if err != nil || len(got) != 0 {
		t.Errorf("no breaks: %v %v", got, err)
	}
	if _, err := ParseTXT(strings.NewReader("1\t2\n")); err != ErrNoHeader {
		t.Errorf("no header: %v", err)
	}
	if _, err := ParseTXT(strings.NewReader("FILE PROCESSING COMPLETE 1 FRAMES AT 0\n")); err != ErrNoHeader {
		t.Errorf("zero fps: %v", err)
	}
}

func TestNormalize(t *testing.T) {
	const dur = 3_600_000 // an hour
	for _, c := range []struct {
		name string
		in   []Break
		want []Break
	}{
		{"nothing", nil, nil},
		{"sorted and kept", []Break{{900_000, 1_080_000}, {300_000, 420_000}}, []Break{{300_000, 420_000}, {900_000, 1_080_000}}},
		{"merged when less than 10 s apart", []Break{{300_000, 360_000}, {369_000, 420_000}}, []Break{{300_000, 420_000}}},
		{"not merged at 10 s", []Break{{300_000, 360_000}, {370_000, 420_000}}, []Break{{300_000, 360_000}, {370_000, 420_000}}},
		{"overlapping merged", []Break{{300_000, 400_000}, {350_000, 380_000}}, []Break{{300_000, 400_000}}},
		{"short ones dropped", []Break{{300_000, 314_999}, {600_000, 615_000}}, []Break{{600_000, 615_000}}},
		{"short ones merged first", []Break{{300_000, 308_000}, {312_000, 320_000}}, []Break{{300_000, 320_000}}},
		{"first 5 s never marked", []Break{{0, 60_000}}, []Break{{5_000, 60_000}}},
		{"last 5 s never marked", []Break{{3_500_000, dur}}, []Break{{3_500_000, dur - 5_000}}},
		{"too short after trimming the edge", []Break{{0, 18_000}}, nil},
		{"beyond the end", []Break{{dur + 1_000, dur + 60_000}}, nil},
		{"backwards", []Break{{500_000, 400_000}}, nil},
		{"most of the recording: detection went wrong", []Break{{0, dur}, {600_000, 700_000}}, nil},
		{"half is allowed", []Break{{100_000, 100_000 + dur/2}}, []Break{{100_000, 100_000 + dur/2}}},
	} {
		if got := Normalize(c.in, dur); !reflect.DeepEqual(got, c.want) && !(len(got) == 0 && len(c.want) == 0) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	// Unknown duration: only the start is trimmed.
	if got := Normalize([]Break{{0, 60_000}, {7_000_000, 7_200_000}}, 0); !reflect.DeepEqual(got, []Break{{5_000, 60_000}, {7_000_000, 7_200_000}}) {
		t.Errorf("unknown duration: %v", got)
	}
}
