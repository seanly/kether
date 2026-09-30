package main

import (
	"bytes"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/seanly/kether"
	"github.com/seanly/kether/id"
)

func TestEchoTwoNodes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv, err := startServer(ctx, serveConfig{Echo: true})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	body, err := callAgent(ctx, "", []string{hostPort(t, srv.Node)}, "", "ping")
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
	asker, err := startClient(ctx, key, []string{hostPort(t, srv.Node)})
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
			if _, err := id.ParseUUID(p.ID); err != nil {
				t.Fatal(err)
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

func TestServeSeedAndAskID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	seed, err := startServer(ctx, serveConfig{Echo: true})
	if err != nil {
		t.Fatal(err)
	}
	defer seed.Close()
	hp := hostPort(t, seed.Node)
	other, err := startServer(ctx, serveConfig{Echo: true, Seeds: []string{hp}})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	other.Node.Handle(func(_ context.Context, c kether.Call) (kether.Result, error) {
		return kether.Result{Body: []byte("from-other")}, nil
	})
	body, err := callAgent(ctx, "", []string{hp}, other.Node.ID(), "ping")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "from-other" {
		t.Fatalf("body %q", body)
	}
}

func TestWriteReady(t *testing.T) {
	var buf bytes.Buffer
	const uuid = "11111111-1111-4111-8111-111111111111"
	if err := writeReady(&buf, uuid); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if got != "uuid "+uuid+"\n" {
		t.Fatalf("output %q", got)
	}
	if strings.Contains(got, "addr ") {
		t.Fatalf("output %q", got)
	}
}

func hostPort(t *testing.T, n *kether.Node) string {
	t.Helper()
	for _, raw := range n.Addrs() {
		parts := strings.Split(raw, "/")
		if len(parts) >= 5 && parts[1] == "ip4" && parts[3] == "tcp" {
			return net.JoinHostPort(parts[2], parts[4])
		}
	}
	t.Fatal("no tcp address")
	return ""
}
