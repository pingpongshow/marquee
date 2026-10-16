package plexsync

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// fakePlex is a Plex Media Server (and plex.tv under /tv) holding per-account watch state.
type fakePlex struct {
	srv *httptest.Server

	mu      sync.Mutex
	tokens  map[string]int64 // server token → account
	items   map[string]*fakeItem
	state   map[int64]map[string]*fakeState
	history []fakePlay
	calls   []string       // write calls: "scrobble acct=2 key=101"
	fail    map[string]int // path → HTTP status to return
	now     func() int64
}

type fakeItem struct {
	key, section, typ, title, grandparent, file string
	duration                                    int64
}

type fakeState struct {
	viewCount, lastViewedAt, viewOffset, lastRatedAt int64
	rating                                           *float64
}

type fakePlay struct {
	account  int64
	key      string
	viewedAt int64
}

const (
	ownerTok  = "owner-secret-token-1234"
	kidSrvTok = "kid-server-token-5678"
	kidTVTok  = "kid-plextv-token-9012"
)

func newFakePlex() *fakePlex {
	f := &fakePlex{
		tokens: map[string]int64{ownerTok: 1, kidSrvTok: 2},
		items:  map[string]*fakeItem{},
		state:  map[int64]map[string]*fakeState{},
		fail:   map[string]int{},
		now:    func() int64 { return time.Now().Unix() },
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	return f
}

func (f *fakePlex) addItem(it fakeItem) { f.items[it.key] = &it }

func (f *fakePlex) st(acct int64, key string) *fakeState {
	if f.state[acct] == nil {
		f.state[acct] = map[string]*fakeState{}
	}
	if f.state[acct][key] == nil {
		f.state[acct][key] = &fakeState{}
	}
	return f.state[acct][key]
}

func (f *fakePlex) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakePlex) metadata(acct int64, it *fakeItem) map[string]any {
	m := map[string]any{"ratingKey": it.key, "type": it.typ, "title": it.title, "duration": it.duration,
		"Media": []any{map[string]any{"Part": []any{map[string]any{"file": it.file}}}}}
	if it.grandparent != "" {
		m["grandparentTitle"] = it.grandparent
	}
	if s := f.state[acct][it.key]; s != nil {
		if s.viewCount > 0 {
			m["viewCount"] = s.viewCount
		}
		if s.lastViewedAt > 0 {
			m["lastViewedAt"] = s.lastViewedAt
		}
		if s.viewOffset > 0 {
			m["viewOffset"] = s.viewOffset
		}
		if s.rating != nil {
			m["userRating"] = *s.rating
			m["lastRatedAt"] = s.lastRatedAt
		}
	}
	return m
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func mc(v map[string]any) map[string]any { return map[string]any{"MediaContainer": v} }

func (f *fakePlex) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.Contains(r.URL.RawQuery, "Token") {
		http.Error(w, "token in URL", http.StatusBadRequest)
		return
	}
	if code := f.fail[r.URL.Path]; code != 0 {
		w.WriteHeader(code)
		return
	}
	tok := r.Header.Get("X-Plex-Token")
	q := r.URL.Query()
	if strings.HasPrefix(r.URL.Path, "/tv/") {
		f.serveTV(w, r, tok)
		return
	}
	acct, ok := f.tokens[tok]
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	now := f.now()
	key := q.Get("key")
	switch p := r.URL.Path; {
	case p == "/identity":
		writeJSON(w, mc(map[string]any{"machineIdentifier": "machine-1"}))
	case p == "/accounts":
		writeJSON(w, mc(map[string]any{"Account": []any{
			map[string]any{"id": 1, "name": "Owner"}, map[string]any{"id": 2, "name": "Kid"}, map[string]any{"id": 3, "name": "Friend"},
		}}))
	case p == "/library/sections":
		writeJSON(w, mc(map[string]any{"Directory": []any{
			map[string]any{"key": "1", "type": "movie", "title": "Movies", "Location": []any{map[string]any{"path": "/data/movies"}}},
			map[string]any{"key": "2", "type": "artist", "title": "Music", "Location": []any{map[string]any{"path": "/data/music"}}},
		}}))
	case strings.HasPrefix(p, "/library/sections/") && strings.HasSuffix(p, "/all"):
		sec := strings.TrimSuffix(strings.TrimPrefix(p, "/library/sections/"), "/all")
		want := map[string]string{"1": "movie", "4": "episode", "10": "track"}[q.Get("type")]
		var keys []string
		for k, it := range f.items {
			if it.section == sec && it.typ == want {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		start, _ := strconv.Atoi(q.Get("X-Plex-Container-Start"))
		size, _ := strconv.Atoi(q.Get("X-Plex-Container-Size"))
		var list []any
		for i := start; i < len(keys) && i < start+size; i++ {
			list = append(list, f.metadata(acct, f.items[keys[i]]))
		}
		writeJSON(w, mc(map[string]any{"size": len(list), "totalSize": len(keys), "Metadata": list}))
	case strings.HasPrefix(p, "/library/metadata/"):
		it := f.items[strings.TrimPrefix(p, "/library/metadata/")]
		if it == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(w, mc(map[string]any{"size": 1, "Metadata": []any{f.metadata(acct, it)}}))
	case p == "/status/sessions/history/all":
		if acct != 1 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		since, _ := strconv.ParseInt(q.Get("viewedAt>"), 10, 64)
		var list []any
		for _, h := range f.history {
			if h.viewedAt >= since {
				it := f.items[h.key]
				list = append(list, map[string]any{"ratingKey": h.key, "title": it.title, "type": it.typ, "viewedAt": h.viewedAt, "accountID": h.account})
			}
		}
		start, _ := strconv.Atoi(q.Get("X-Plex-Container-Start"))
		if start > len(list) {
			start = len(list)
		}
		list = list[start:]
		writeJSON(w, mc(map[string]any{"size": len(list), "totalSize": len(list) + start, "Metadata": list}))
	case p == "/:/scrobble":
		if q.Get("identifier") != libraryIdentifier || f.items[key] == nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		s := f.st(acct, key)
		s.viewCount++
		s.lastViewedAt, s.viewOffset = now, 0
		f.history = append(f.history, fakePlay{acct, key, now})
		f.calls = append(f.calls, fmt.Sprintf("scrobble acct=%d key=%s", acct, key))
	case p == "/:/unscrobble":
		if q.Get("identifier") != libraryIdentifier || f.items[key] == nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		s := f.st(acct, key)
		s.viewCount, s.viewOffset = 0, 0
		f.calls = append(f.calls, fmt.Sprintf("unscrobble acct=%d key=%s", acct, key))
	case p == "/:/rate":
		if r.Method != http.MethodPut || q.Get("identifier") != libraryIdentifier || f.items[key] == nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		s := f.st(acct, key)
		v, _ := strconv.ParseFloat(q.Get("rating"), 64)
		if v < 0 {
			s.rating = nil
		} else {
			s.rating = &v
		}
		s.lastRatedAt = now
		f.calls = append(f.calls, fmt.Sprintf("rate acct=%d key=%s rating=%s", acct, key, q.Get("rating")))
	case p == "/:/timeline":
		rk := q.Get("ratingKey")
		if q.Get("state") != "stopped" || q.Get("key") != "/library/metadata/"+rk || f.items[rk] == nil || r.Header.Get("X-Plex-Client-Identifier") == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		s := f.st(acct, rk)
		s.viewOffset, _ = strconv.ParseInt(q.Get("time"), 10, 64)
		s.lastViewedAt = now
		f.calls = append(f.calls, fmt.Sprintf("timeline acct=%d key=%s time=%s duration=%s", acct, rk, q.Get("time"), q.Get("duration")))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// serveTV is plex.tv: account 2 is a managed Plex Home user; account 3 is a friend the
// owner's token can't act for.
func (f *fakePlex) serveTV(w http.ResponseWriter, r *http.Request, tok string) {
	switch p := strings.TrimPrefix(r.URL.Path, "/tv"); {
	case p == "/api/v2/home/users" && tok == ownerTok:
		writeJSON(w, map[string]any{"users": []any{
			map[string]any{"id": 99, "uuid": "owner-uuid", "title": "Owner", "admin": true},
			map[string]any{"id": 2, "uuid": "kid-uuid", "title": "Kid", "restricted": true},
		}})
	case p == "/api/v2/home/users/kid-uuid/switch" && r.Method == http.MethodPost && tok == ownerTok:
		writeJSON(w, map[string]any{"authToken": kidTVTok})
	case p == "/api/v2/resources" && tok == kidTVTok:
		writeJSON(w, []any{map[string]any{"clientIdentifier": "other", "accessToken": "nope"},
			map[string]any{"clientIdentifier": "machine-1", "accessToken": kidSrvTok}})
	case p == "/api/servers/machine-1/shared_servers" && tok == ownerTok:
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(`<MediaContainer size="0"></MediaContainer>`))
	case p == "/api/v2/pins" && r.Method == http.MethodPost:
		writeJSON(w, map[string]any{"id": 77, "code": "ABCD"})
	case p == "/api/v2/pins/77":
		writeJSON(w, map[string]any{"id": 77, "code": "ABCD", "authToken": "pin-token-xyz"})
	default:
		w.WriteHeader(http.StatusUnauthorized)
	}
}
