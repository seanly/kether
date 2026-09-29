package id_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcutil/bech32"
	"github.com/seanly/kether/id"
)

func TestAgentIDRoundTrip(t *testing.T) {
	k, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	s, err := k.Public().AgentID()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s, "keth1") {
		t.Fatalf("id %q", s)
	}
	pub, err := id.ParseAgentID(s)
	if err != nil {
		t.Fatal(err)
	}
	if string(pub.XOnly()) != string(k.Public().XOnly()) {
		t.Fatal("pubkey mismatch")
	}
	a, err := k.Public().PeerID()
	if err != nil {
		t.Fatal(err)
	}
	b, err := k.Public().PeerID()
	if err != nil {
		t.Fatal(err)
	}
	if a != b || a == "" {
		t.Fatalf("peer id %s %s", a, b)
	}
}

func TestTamperedID(t *testing.T) {
	k, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	s, err := k.Public().AgentID()
	if err != nil {
		t.Fatal(err)
	}
	b := []byte(s)
	// Flip a data character, avoiding the separator.
	idx := strings.LastIndex(s, "1") + 2
	if b[idx] == 'q' {
		b[idx] = 'p'
	} else {
		b[idx] = 'q'
	}
	if _, err := id.ParseAgentID(string(b)); err == nil {
		t.Fatal("expected checksum failure")
	}
}

func TestRejectsOtherVersion(t *testing.T) {
	k, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	raw := append([]byte{0x01}, k.Public().XOnly()...)
	five, err := bech32.ConvertBits(raw, 8, 5, true)
	if err != nil {
		t.Fatal(err)
	}
	s, err := bech32.EncodeM("keth", five)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := id.ParseAgentID(s); err == nil {
		t.Fatal("expected version rejection")
	}
}

func TestSignVerify(t *testing.T) {
	k, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	sig, err := id.Sign(k, id.DomainRecord, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if !id.Verify(k.Public(), id.DomainRecord, []byte("hello"), sig) {
		t.Fatal("verify")
	}
	if id.Verify(k.Public(), id.DomainRecord, []byte("hellp"), sig) {
		t.Fatal("tampered message verified")
	}
}

func TestKeyFileMode(t *testing.T) {
	k, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "key")
	if err := k.Save(path); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", fi.Mode().Perm())
	}
	if _, err := id.Load(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := id.Load(path); err == nil {
		t.Fatal("expected permissive file to be rejected")
	}
}
