package api

import (
	"context"
	"testing"

	"marquee/internal/auth"
	"marquee/internal/netclass"
)

// A profile opened without a code asks for it again away from home, and friends never get it
// (USER-22).
func TestOpenSignInOnlyAtHome(t *testing.T) {
	at := func(c netclass.Class) context.Context {
		return context.WithValue(context.Background(), reqInfoKey{}, RequestInfo{Class: c})
	}
	u := auth.User{Restrictions: auth.Restrictions{OpenSignIn: true}}
	if !openSignIn(at(netclass.Local), u) {
		t.Fatal("at home")
	}
	if openSignIn(at(netclass.Remote), u) {
		t.Fatal("away from home")
	}
	if p := toAPIProfile(at(netclass.Remote), auth.User{HasPassword: true, Restrictions: u.Restrictions}); p.Requires != ProfileRequiresPassword {
		t.Fatalf("away from home asks for the password: %s", p.Requires)
	}
	u.Restrictions.Friend = true
	if openSignIn(at(netclass.Local), u) {
		t.Fatal("a friend")
	}
}
