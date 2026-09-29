package dht_test

import (
	"testing"
	"time"

	"github.com/seanly/kether/dht"
	"github.com/seanly/kether/id"
	"github.com/seanly/kether/record"
)

func signed(t *testing.T, exp time.Time, seq uint64) []byte {
	t.Helper()
	k, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	r := &record.Record{
		Seq: seq, ExpiresAt: exp.Unix(), Name: "echo",
		Types: []string{"echo"}, Access: record.AccessPublic,
		Addrs: []string{"/ip4/127.0.0.1/tcp/9"},
	}
	if err := record.Sign(k, r); err != nil {
		t.Fatal(err)
	}
	b, err := r.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestValidator(t *testing.T) {
	v := dht.Validator{}
	good := signed(t, time.Now().Add(time.Hour), 3)
	rec, err := record.Unmarshal(good)
	if err != nil {
		t.Fatal(err)
	}
	aid, err := rec.AgentID()
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Validate(dht.AgentKey(aid), good); err != nil {
		t.Fatal(err)
	}
	bad := append([]byte(nil), good...)
	bad[len(bad)-1] ^= 0xff
	if err := v.Validate(dht.AgentKey(aid), bad); err == nil {
		t.Fatal("expected bad signature")
	}
	old := signed(t, time.Now().Add(-3*time.Minute), 1)
	orec, _ := record.Unmarshal(old)
	oaid, _ := orec.AgentID()
	if err := v.Validate(dht.AgentKey(oaid), old); err == nil {
		t.Fatal("expected expired")
	}
	blob := make([]byte, record.MaxBytes+1)
	if err := v.Validate(dht.AgentKey(aid), blob); err == nil {
		t.Fatal("expected size rejection")
	}
}

func TestSelectKeepsEarlierOnEqualSeq(t *testing.T) {
	v := dht.Validator{}
	k, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	mk := func(seq uint64, name string) []byte {
		r := &record.Record{
			Seq: seq, ExpiresAt: time.Now().Add(time.Hour).Unix(), Name: name,
			Types: []string{"echo"}, Addrs: []string{"/ip4/127.0.0.1/tcp/9"},
		}
		if err := record.Sign(k, r); err != nil {
			t.Fatal(err)
		}
		b, err := r.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	newer := mk(2, "new")
	older := mk(1, "old")
	idx, err := v.Select("/kether/agent/1/x", [][]byte{older, newer})
	if err != nil || idx != 1 {
		t.Fatalf("idx %d err %v", idx, err)
	}
	a := mk(4, "a")
	b := mk(4, "b")
	idx, err = v.Select("/kether/agent/1/x", [][]byte{a, b})
	if err != nil || idx != 1 {
		t.Fatalf("tie idx %d err %v", idx, err)
	}
}
