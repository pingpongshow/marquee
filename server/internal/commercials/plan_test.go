package commercials

import (
	"reflect"
	"testing"
)

func TestMakePlanKeyframes(t *testing.T) {
	kfs := []int64{0, 2000, 4000, 6000, 8000, 10000, 12000, 14000, 16000, 18000, 20000, 22000, 24000, 26000, 28000, 30000}
	// A break ending between keyframes resumes on the keyframe before its end (keeping all
	// the programme, a little advert stays), never the one after.
	p := MakePlan([]Break{{5000, 12900}, {20500, 27100}}, kfs, 31000)
	if !p.Copy {
		t.Fatal("expected stream copy")
	}
	wantKeep := []Segment{{0, 5000}, {12000, 20500}, {26000, 31000}}
	wantRemoved := []Break{{5000, 12000}, {20500, 26000}}
	if !reflect.DeepEqual(p.Keep, wantKeep) || !reflect.DeepEqual(p.Removed, wantRemoved) {
		t.Errorf("keep %v removed %v", p.Keep, p.Removed)
	}
	if p.RemovedMS() != 12500 || p.KeptMS() != 18500 {
		t.Errorf("removed %d kept %d", p.RemovedMS(), p.KeptMS())
	}
	// On a keyframe exactly: no advert left.
	if p := MakePlan([]Break{{5000, 12000}}, kfs, 31000); !reflect.DeepEqual(p.Removed, []Break{{5000, 12000}}) {
		t.Errorf("exact keyframe: %v", p.Removed)
	}

	// A break with no keyframe inside it can't be copy-cut: exact cuts for a re-encode.
	p = MakePlan([]Break{{4500, 5900}}, kfs, 31000)
	if p.Copy || !reflect.DeepEqual(p.Removed, []Break{{4500, 5900}}) || !reflect.DeepEqual(p.Keep, []Segment{{0, 4500}, {5900, 31000}}) {
		t.Errorf("no keyframe inside: %+v", p)
	}
	// Keyframes too far apart (more than 5 s of advert would stay): re-encode.
	sparse := []int64{0, 10000, 20000, 30000}
	if p := MakePlan([]Break{{5000, 19000}}, sparse, 31000); p.Copy || !reflect.DeepEqual(p.Removed, []Break{{5000, 19000}}) {
		t.Errorf("sparse keyframes: %+v", p)
	}
	// No keyframe index at all.
	if p := MakePlan([]Break{{5000, 19000}}, nil, 31000); p.Copy {
		t.Errorf("no keyframes: %+v", p)
	}
}

func TestPositions(t *testing.T) {
	removed := []Break{{20_000, 30_000}, {50_000, 60_000}}
	for _, c := range []struct{ orig, cut int64 }{{10_000, 10_000}, {25_000, 20_000}, {40_000, 30_000}, {55_000, 40_000}, {70_000, 50_000}} {
		if got := CutPosition(c.orig, removed); got != c.cut {
			t.Errorf("CutPosition(%d) = %d, want %d", c.orig, got, c.cut)
		}
	}
	for _, c := range []struct{ cut, orig int64 }{{10_000, 10_000}, {20_000, 30_000}, {30_000, 40_000}, {50_000, 70_000}} {
		if got := OriginalPosition(c.cut, removed); got != c.orig {
			t.Errorf("OriginalPosition(%d) = %d, want %d", c.cut, got, c.orig)
		}
	}
}
