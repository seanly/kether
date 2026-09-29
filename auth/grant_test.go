package auth_test

import (
	"testing"
	"time"

	"github.com/seanly/kether/auth"
	"github.com/seanly/kether/id"
)

func grantBetween(t *testing.T, calls uint64) (*auth.Grant, *id.PrivateKey, *id.PrivateKey) {
	t.Helper()
	owner, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	guest, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	g := &auth.Grant{
		Grantee:   guest.Public().XOnly(),
		Types:     []string{"echo"},
		NotBefore: time.Now().Add(-time.Minute).Unix(),
		NotAfter:  time.Now().Add(time.Hour).Unix(),
		MaxCalls:  calls,
	}
	if err := auth.Sign(owner, g); err != nil {
		t.Fatal(err)
	}
	return g, owner, guest
}

func TestGrantHappy(t *testing.T) {
	g, _, guest := grantBetween(t, 0)
	raw := g.Marshal()
	got, err := auth.Unmarshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := got.Allows(guest.Public().XOnly(), "echo", time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestGrantRejects(t *testing.T) {
	g, _, guest := grantBetween(t, 0)
	g.Types = []string{"other"}
	if err := g.Allows(guest.Public().XOnly(), "echo", time.Now()); err == nil {
		t.Fatal("expected type mismatch or bad signature")
	}
	g, _, guest = grantBetween(t, 0)
	other, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Allows(other.Public().XOnly(), "echo", time.Now()); err == nil {
		t.Fatal("expected grantee mismatch")
	}
}

func TestExpiryWindow(t *testing.T) {
	g, owner, guest := grantBetween(t, 0)
	g.NotAfter = time.Now().Add(-time.Hour).Unix()
	if err := auth.Sign(owner, g); err != nil {
		t.Fatal(err)
	}
	if err := g.Allows(guest.Public().XOnly(), "echo", time.Now()); err == nil {
		t.Fatal("expected expired")
	}
}

func TestMaxCalls(t *testing.T) {
	g, _, guest := grantBetween(t, 2)
	c := &auth.Checker{}
	caller := guest.Public().XOnly()
	now := time.Now()
	if err := c.Accept(g, caller, "echo", now); err != nil {
		t.Fatal(err)
	}
	if err := c.Accept(g, caller, "echo", now); err != nil {
		t.Fatal(err)
	}
	if err := c.Accept(g, caller, "echo", now); err == nil {
		t.Fatal("expected call limit")
	}
	fresh := &auth.Checker{}
	if err := fresh.Accept(g, caller, "echo", now); err != nil {
		t.Fatal(err)
	}
	if err := fresh.Accept(g, caller, "echo", now); err != nil {
		t.Fatal(err)
	}
}
