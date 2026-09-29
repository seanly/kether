package record_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/seanly/kether/id"
	"github.com/seanly/kether/record"
)

func sample(t *testing.T, exp time.Time) (*id.PrivateKey, *record.Record) {
	t.Helper()
	k, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	r := &record.Record{
		Seq:       2,
		ExpiresAt: exp.Unix(),
		Name:      "echo",
		Types:     []string{"echo"},
		Access:    record.AccessPublic,
		Addrs:     []string{"/ip4/127.0.0.1/tcp/1/p2p/placeholder"},
		Pay:       []record.PayMethod{{Rail: "lightning", Amount: 1000}},
	}
	if err := record.Sign(k, r); err != nil {
		t.Fatal(err)
	}
	return k, r
}

func TestRoundTrip(t *testing.T) {
	_, r := sample(t, time.Now().Add(time.Hour))
	b1, err := r.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	got, err := record.Unmarshal(b1)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := got.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b1, b2) {
		t.Fatal("encoding not stable")
	}
	if err := got.Verify(time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestTamper(t *testing.T) {
	_, r := sample(t, time.Now().Add(time.Hour))
	r.Name = "nope"
	if err := r.Verify(time.Now()); err == nil {
		t.Fatal("expected bad signature")
	}
}

func TestMeta(t *testing.T) {
	k, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	bad := &record.Record{
		Seq: 1, ExpiresAt: time.Now().Add(time.Hour).Unix(),
		Types: []string{"BAD"},
		Addrs: []string{"/ip4/127.0.0.1/tcp/1"},
	}
	if err := record.Sign(k, bad); err == nil {
		t.Fatal("expected bad type")
	}
	empty := &record.Record{
		Seq: 1, ExpiresAt: time.Now().Add(time.Hour).Unix(),
		Types: []string{"echo"},
	}
	if err := record.Sign(k, empty); err == nil {
		t.Fatal("expected empty addrs")
	}
}

func TestExpiryAndPeer(t *testing.T) {
	now := time.Now()
	_, fresh := sample(t, now.Add(-time.Minute))
	if err := fresh.Verify(now); err != nil {
		t.Fatal(err)
	}
	_, old := sample(t, now.Add(-3*time.Minute))
	if err := old.Verify(now); err == nil {
		t.Fatal("expected expired")
	}
	_, r := sample(t, now.Add(time.Hour))
	r.PeerID = "12D3KooWchanged"
	if err := r.Verify(now); err == nil {
		t.Fatal("expected peer mismatch")
	}
}

func TestPreferSeq(t *testing.T) {
	_, a := sample(t, time.Now().Add(time.Hour))
	b := *a
	b.Seq = a.Seq
	if record.Prefer(a, &b) != a {
		t.Fatal("equal seq should keep the first")
	}
	c := *a
	c.Seq = a.Seq + 1
	if record.Prefer(a, &c) != &c {
		t.Fatal("higher seq should win")
	}
}

func TestTooBig(t *testing.T) {
	k, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	addrs := make([]string, 8)
	for i := range addrs {
		addrs[i] = "/ip4/127.0.0.1/tcp/1/p2p/" + strings.Repeat("a", 1200)
	}
	r := &record.Record{
		Seq: 1, ExpiresAt: time.Now().Add(time.Hour).Unix(),
		Types: []string{"echo"}, Addrs: addrs,
	}
	if err := record.Sign(k, r); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Marshal(); err == nil {
		t.Fatal("expected 8KiB rejection")
	}
}
