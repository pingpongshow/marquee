package webhooks

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"marquee/internal/db"
	"marquee/internal/settings"
)

func TestPublishDeliversSignedSubscribedEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan *http.Request, 4)
	bodies := make(chan []byte, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- r
		bodies <- b
	}))
	defer srv.Close()

	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "m.db"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := settings.Open(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(ctx, func(s *settings.Settings) error {
		s.Webhooks = []settings.Webhook{{ID: "a", Name: "Home Assistant", URL: srv.URL, Secret: "s3cret", Events: []string{PlaybackStarted}, Enabled: true}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	disp := &Dispatcher{DB: d, Settings: store, Server: func() Server { return Server{Name: "Test"} }}
	go disp.Run(ctx)

	disp.Publish(Event{Event: PlaybackStopped}) // not subscribed
	disp.Publish(Event{Event: PlaybackStarted, User: &User{ID: 1, Name: "Ann"}})
	select {
	case r := <-got:
		body := <-bodies
		if r.Header.Get("X-Marquee-Signature") != Sign("s3cret", body) {
			t.Fatal("bad signature")
		}
		var ev Event
		if err := json.Unmarshal(body, &ev); err != nil || ev.Event != PlaybackStarted || ev.User.Name != "Ann" || ev.Server.Name != "Test" {
			t.Fatalf("payload %s", body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing delivered")
	}
	select {
	case <-got:
		t.Fatal("an unsubscribed event was delivered")
	case <-time.After(300 * time.Millisecond):
	}
}
