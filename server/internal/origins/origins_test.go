package origins

import (
	"reflect"
	"testing"
)

func TestLookupAndFind(t *testing.T) {
	for w, want := range map[string]Origin{"Korean": {"KR", "ko"}, "british": {"GB", ""}, "Icelandic": {"IS", "is"}, "swiss": {"CH", ""}, "SE": {"SE", "sv"}} {
		if got, ok := Lookup(w); !ok || got != want {
			t.Errorf("%s: %+v %v", w, got, ok)
		}
	}
	if _, ok := Lookup("Narnia"); ok {
		t.Error("unknown place found")
	}
	got, rest := Find("south korean and british thrillers")
	if !reflect.DeepEqual(got, []Origin{{"KR", "ko"}, {"GB", ""}}) || rest != "and thrillers" {
		t.Errorf("find: %+v %q", got, rest)
	}
	if CountryName("kr") != "South Korea" || LanguageName("JA") != "Japanese" {
		t.Error("names")
	}
}
