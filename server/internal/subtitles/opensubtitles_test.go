package subtitles

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearchAndDownload(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/file.srt" && r.Header.Get("Api-Key") != "k" { // download links are plain URLs
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"message": "bad key"})
			return
		}
		switch r.URL.Path {
		case "/subtitles":
			q := r.URL.Query()
			if q.Get("parent_imdb_id") != "903747" || q.Get("season_number") != "1" || q.Get("episode_number") != "2" || q.Get("languages") != "en" {
				t.Errorf("query %s", r.URL.RawQuery)
			}
			w.Write([]byte(`{"data":[
				{"attributes":{"language":"en","release":"Popular.WEB","download_count":900,"files":[{"file_id":1,"file_name":"a.srt"}]}},
				{"attributes":{"language":"en","release":"Exact.Match","download_count":3,"moviehash_match":true,"files":[{"file_id":2,"file_name":"b.srt"}]}},
				{"attributes":{"language":"en","release":"Robot","download_count":5000,"ai_translated":true,"files":[{"file_id":3,"file_name":"c.srt"}]}}]}`))
		case "/download":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["file_id"].(float64) != 2 {
				t.Errorf("download body %v", body)
			}
			json.NewEncoder(w).Encode(map[string]any{"link": srv.URL + "/file.srt", "file_name": "b.srt", "remaining": 4})
		case "/file.srt":
			w.Write([]byte("1\n00:00:01,000 --> 00:00:02,000\nHello\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, APIKey: "k", UserAgent: "test"}
	res, err := c.Search(context.Background(), Query{IMDbID: "tt0903747", IsEpisode: true, Season: 1, Episode: 2, Languages: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 3 || res[0].FileID != 2 || res[1].FileID != 1 || res[2].FileID != 3 {
		t.Fatalf("order %+v", res)
	}
	data, _, left, err := c.Download(context.Background(), res[0].FileID)
	if err != nil || left != 4 || string(data[:1]) != "1" {
		t.Fatalf("download %q %d %v", data, left, err)
	}
	if _, err := (&Client{Base: srv.URL, APIKey: "nope", UserAgent: "test"}).Search(context.Background(), Query{Title: "x"}); err == nil || err.Error() != "OpenSubtitles: bad key" {
		t.Fatalf("error %v", err)
	}
}
