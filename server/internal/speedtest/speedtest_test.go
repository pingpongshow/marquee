package speedtest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/__down":
			n, _ := strconv.Atoi(r.URL.Query().Get("bytes"))
			w.Write(make([]byte, min(n, 1<<20)))
		case "/__up":
			io.Copy(io.Discard, r.Body)
		}
	}))
	defer srv.Close()
	res, err := (&Tester{BaseURL: srv.URL, Duration: 300 * time.Millisecond}).Run(t.Context())
	if err != nil || res.DownloadMbps <= 0 || res.UploadMbps <= 0 || res.LatencyMs <= 0 {
		t.Fatalf("%+v %v", res, err)
	}
}
