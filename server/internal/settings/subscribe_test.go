package settings

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// Subscribers hear updates one at a time, in the order they were saved, so the last call
// is the saved settings (a stale call last would, e.g., leave old LAN subnets in force).
func TestSubscribersHearUpdatesInOrder(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, openDB(t))
	if err != nil {
		t.Fatal(err)
	}
	var last string
	calls := 0
	s.Subscribe(func(n Settings) { // deliberately unsynchronised, like the real subscribers
		last = n.General.ServerName
		calls++
	})
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Update(ctx, func(n *Settings) error {
				n.General.ServerName = fmt.Sprint("server ", i)
				return nil
			}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls != 20 || last != s.Get().General.ServerName {
		t.Errorf("calls %d, last heard %q, saved %q", calls, last, s.Get().General.ServerName)
	}
}
