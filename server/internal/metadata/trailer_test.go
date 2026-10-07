package metadata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"marquee/internal/metadata/tmdb"
)

func TestBestTrailer(t *testing.T) {
	videos := []tmdb.Video{
		{Key: "clip", Site: "YouTube", Type: "Clip", Official: true},
		{Key: "teaser", Site: "YouTube", Type: "Teaser", Official: true, PublishedAt: "2024-05-01"},
		{Key: "vimeo", Site: "Vimeo", Type: "Trailer", Official: true},
		{Key: "fan", Site: "YouTube", Type: "Trailer", PublishedAt: "2024-07-01"},
		{Key: "old", Site: "YouTube", Type: "Trailer", Official: true, PublishedAt: "2023-01-01"},
		{Key: "new", Site: "YouTube", Type: "Trailer", Official: true, PublishedAt: "2024-06-01"},
	}
	got, err := bestTrailer(videos)
	if err != nil || got.YouTubeKey != "new" {
		t.Fatalf("got %+v %v", got, err)
	}
	if _, err := bestTrailer(videos[:1]); err != ErrNoTrailer {
		t.Fatalf("a clip isn't a trailer: %v", err)
	}
}

func TestVideosRequest(t *testing.T) {
	var path, langs string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, langs = r.URL.Path, r.URL.Query().Get("include_video_language")
		w.Write([]byte(`{"results":[{"key":"abc","site":"YouTube","type":"Trailer","official":true,"name":"Official Trailer"}]}`))
	}))
	defer srv.Close()
	c := tmdb.New("k", "de-DE")
	c.BaseURL = srv.URL
	v, err := c.Videos(context.Background(), "movie", 603)
	if err != nil || len(v) != 1 || v[0].Key != "abc" {
		t.Fatalf("%+v %v", v, err)
	}
	if path != "/movie/603/videos" || langs != "de,en,null" {
		t.Fatalf("path %s langs %s", path, langs)
	}
}
