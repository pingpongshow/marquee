package api

import (
	"strings"
	"testing"

	"marquee/internal/items"
)

func TestMixEntries(t *testing.T) {
	lib := []items.Summary{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}, {ID: 5}}
	vod := []VodSuggestion{{Key: "movie:1"}, {Key: "series:2"}}
	var keys []string
	for _, e := range mixEntries(lib, vod) {
		keys = append(keys, e.Key)
	}
	if got, want := strings.Join(keys, " "), "item:1 item:2 vod:movie:1 item:3 item:4 vod:series:2 item:5"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	if got := mixEntries(nil, vod); len(got) != 2 || got[0].Kind != HomeEntryKindVod {
		t.Errorf("on demand only: %+v", got)
	}
}
