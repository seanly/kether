package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/seanly/kether"
)

func TestEchoTwoNodes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv, err := startServer(ctx, serveConfig{Echo: true})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	body, err := callAgent(ctx, "", srv.Node.Addrs(), "", "ping")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ping" {
		t.Fatalf("body %q", body)
	}
	if strings.Contains(string(body), "preimage") {
		t.Fatal("body leaked a preimage")
	}
}

func TestSearch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv, err := startServer(ctx, serveConfig{Echo: true})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	key, err := loadKey("")
	if err != nil {
		t.Fatal(err)
	}
	asker, err := startClient(ctx, key, srv.Node.Addrs())
	if err != nil {
		t.Fatal(err)
	}
	defer asker.Close()

	deadline := time.Now().Add(8 * time.Second)
	var found bool
	for time.Now().Before(deadline) {
		peers, err := asker.Search(ctx, "agent")
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range peers {
			if p.ID == asker.ID() {
				t.Fatal("client listed itself")
			}
			if p.ID != srv.Node.ID() {
				continue
			}
			found = true
			if p.Name != "kagent" {
				t.Fatalf("name %q", p.Name)
			}
			if len(p.Types) != 1 || p.Types[0] != "agent" {
				t.Fatalf("types %q", p.Types)
			}
			if len(p.Addrs) == 0 {
				t.Fatal("no addrs")
			}
		}
		if found {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !found {
		t.Fatalf("search missed %s", srv.Node.ID())
	}

	peers, err := asker.Search(ctx, "echo")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range peers {
		if p.ID == srv.Node.ID() {
			t.Fatal("agent matched type echo")
		}
	}
}

func TestAnnouncePassedThrough(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const ann = "/ip4/203.0.113.5/tcp/4001/p2p/announce"
	srv, err := startServer(ctx, serveConfig{Echo: true, Announce: []string{ann}})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	key, err := loadKey("")
	if err != nil {
		t.Fatal(err)
	}
	asker, err := startClient(ctx, key, srv.Node.Addrs())
	if err != nil {
		t.Fatal(err)
	}
	defer asker.Close()
	peer, err := findAgent(ctx, asker)
	if err != nil {
		t.Fatal(err)
	}
	if peer.ID != srv.Node.ID() {
		t.Fatalf("found %s", peer.ID)
	}
	if len(peer.Addrs) == 0 || peer.Addrs[0] != ann {
		t.Fatalf("addrs %q", peer.Addrs)
	}
}

func TestAnnounceAddsPeerID(t *testing.T) {
	key, err := loadKey("")
	if err != nil {
		t.Fatal(err)
	}
	got, err := completeAnnounce(key, []string{"/ip4/203.0.113.5/tcp/4001"})
	if err != nil {
		t.Fatal(err)
	}
	pid, err := key.Public().PeerID()
	if err != nil {
		t.Fatal(err)
	}
	want := "/ip4/203.0.113.5/tcp/4001/p2p/" + pid.String()
	if len(got) != 1 || got[0] != want {
		t.Fatalf("announce %q", got)
	}
}

func TestServeSeedAndAskID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	seed, err := startServer(ctx, serveConfig{Echo: true, Public: true})
	if err != nil {
		t.Fatal(err)
	}
	defer seed.Close()
	if len(seed.Node.Addrs()) == 0 {
		t.Fatal("seed has no address")
	}
	other, err := startServer(ctx, serveConfig{Echo: true, Seeds: seed.Node.Addrs()})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	other.Node.Handle(func(_ context.Context, c kether.Call) (kether.Result, error) {
		return kether.Result{Body: []byte("from-other")}, nil
	})
	body, err := callAgent(ctx, "", seed.Node.Addrs(), other.Node.ID(), "ping")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "from-other" {
		t.Fatalf("body %q", body)
	}
}

func TestWriteReady(t *testing.T) {
	var buf bytes.Buffer
	if err := writeReady(&buf, "keth1example", []string{"/ip4/127.0.0.1/tcp/1/p2p/peer"}); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "id keth1example\n") || !strings.Contains(got, "addr /ip4/127.0.0.1/tcp/1/p2p/peer\n") {
		t.Fatalf("output %q", got)
	}
}
