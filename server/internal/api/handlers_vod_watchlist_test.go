package api

import (
	"testing"
	"time"

	"marquee/internal/items"
)

// The Your Watchlist row puts the library's titles and the ones on demand together in the
// order they were added, newest first, up to the row's size.
func TestMergeWatchlist(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 10, 25, 12, m, 0, 0, time.UTC) }
	lib := []items.Summary{{ID: 10, Type: "movie", Title: "Ten"}, {ID: 11, Type: "show", Title: "Eleven"}}
	libAdded := map[int64]time.Time{10: at(30), 11: at(5)}
	vodList := []VodSuggestion{{Kind: VodSuggestionKindSeries, Key: "series:7"}, {Kind: VodSuggestionKindMovie, Key: "movie:3"}}
	vodAdded := map[string]time.Time{"series:7": at(40), "movie:3": at(10)}

	got := mergeWatchlist(lib, libAdded, vodList, vodAdded, 10)
	want := []string{"vod:series:7", "item:10", "vod:movie:3", "item:11"}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %v", len(got), want)
	}
	for i, k := range want {
		if got[i].Key != k {
			t.Fatalf("entry %d: %s, want %v", i, got[i].Key, want)
		}
	}
	if got[0].Kind != HomeEntryKindVod || got[0].Vod == nil || got[1].Kind != HomeEntryKindItem || got[1].Item == nil {
		t.Errorf("kinds: %+v", got[:2])
	}
	if short := mergeWatchlist(lib, libAdded, vodList, vodAdded, 2); len(short) != 2 || short[1].Key != "item:10" {
		t.Errorf("capped: %+v", short)
	}
}
