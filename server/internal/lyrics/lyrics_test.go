package lyrics

import "testing"

func TestParseLRC(t *testing.T) {
	lines, synced := Parse("[ar:Someone]\n[00:12.50]First line\n[00:10.00][00:20.1]Chorus\n\n[01:02.345]Last")
	if !synced || len(lines) != 4 {
		t.Fatalf("%v %+v", synced, lines)
	}
	want := []int64{10000, 12500, 20100, 62345}
	for i, w := range want {
		if lines[i].TimeMS != w {
			t.Errorf("line %d at %d, want %d", i, lines[i].TimeMS, w)
		}
	}
	if lines[0].Text != "Chorus" || lines[3].Text != "Last" {
		t.Errorf("text: %+v", lines)
	}
}

func TestParsePlain(t *testing.T) {
	lines, synced := Parse("\nHello\n\nWorld\n")
	if synced || len(lines) != 3 || lines[0].Text != "Hello" || lines[2].Text != "World" {
		t.Fatalf("%v %+v", synced, lines)
	}
}

func TestEmbeddedTags(t *testing.T) {
	if got := embedded(`{"format":{"tags":{"LYRICS":"La la"}},"streams":[]}`); got != "La la" {
		t.Fatalf("got %q", got)
	}
	if got := embedded(`{"format":{"tags":{}},"streams":[{"tags":{"lyrics-eng":"Hi"}}]}`); got != "Hi" {
		t.Fatalf("stream tag: %q", got)
	}
}
