package playback

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// A source that answers ranges with less than was asked for and cuts the connection part
// way through every other answer: the index is still read, by asking for the rest again.
func TestRemoteKeyframesFromAPatchySource(t *testing.T) {
	ff := ffmpegPath(t)
	data, err := os.ReadFile(makeGOPSample(t, ff, t.TempDir(), "mkv"))
	if err != nil {
		t.Fatal(err)
	}
	const most = 300 << 10
	var answers atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var from, to int
		if _, err := fmt.Sscanf(strings.TrimPrefix(r.Header.Get("Range"), "bytes="), "%d-%d", &from, &to); err != nil || from >= len(data) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		to = min(to, len(data)-1, from+most-1)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, to, len(data)))
		w.Header().Set("Content-Length", strconv.Itoa(to-from+1))
		w.WriteHeader(http.StatusPartialContent)
		body := data[from : to+1]
		if answers.Add(1)%2 == 0 {
			w.Write(body[:len(body)/2]) // then the connection goes
			panic(http.ErrAbortHandler)
		}
		w.Write(body)
	}))
	t.Cleanup(srv.Close)

	kf, err := RemoteKeyframes(context.Background(), srv.Client(), srv.URL+"/1.mkv", "", "mkv")
	if err != nil {
		t.Fatalf("index: %v (after %d answers)", err, answers.Load())
	}
	if len(kf) != 15 {
		t.Fatalf("%d keyframes %v", len(kf), kf)
	}
}
