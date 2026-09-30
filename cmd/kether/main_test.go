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

func TestWriteReady(t *testing.T) {
	var buf bytes.Buffer
	if err := writeReady(&buf); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "listen 0.0.0.0:4001\n" {
		t.Fatalf("output %q", buf.String())
	}
}

func TestRejectsExtraArgs(t *testing.T) {
	err := run(context.Background(), []string{"extra"}, &bytes.Buffer{})
	if err != errUsage {
		t.Fatalf("err %v", err)
	}
}

func TestRelayIsNotAnAgent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	relay, err := start(ctx, config{Listen: []string{"/ip4/127.0.0.1/tcp/0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	hp := hostPort(t, relay)

	second, err := start(ctx, config{
		Listen: []string{"/ip4/127.0.0.1/tcp/0"},
		Seeds:  []string{hp},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	agentKey, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	agent, err := kether.Start(ctx, kether.Config{
		Key:   agentKey,
		Seeds: []string{hp},
		Record: kether.Record{
			Name: "echo", Types: []string{"agent"}, Access: kether.AccessPublic,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()

	seekerKey, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	seeker, err := kether.Start(ctx, kether.Config{
		Key:   seekerKey,
		Seeds: []string{hp},
		Record: kether.Record{
			Types: []string{"client"}, Access: kether.AccessPublic,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer seeker.Close()

	deadline := time.Now().Add(15 * time.Second)
	var sawAgent, sawRelay bool
	for time.Now().Before(deadline) && !(sawAgent && sawRelay) {
		agents, err := seeker.Search(ctx, "agent")
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range agents {
			if p.ID == relay.ID() || p.ID == second.ID() {
				t.Fatalf("relay listed as agent %s", p.ID)
			}
			if p.ID == agent.ID() {
				sawAgent = true
			}
		}
		relays, err := seeker.Search(ctx, "relay")
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range relays {
			if p.ID == agent.ID() {
				t.Fatal("agent listed as relay")
			}
			if p.ID == relay.ID() {
				sawRelay = true
				if len(p.Types) != 1 || p.Types[0] != "relay" {
					t.Fatalf("types %q", p.Types)
				}
			}
		}
		if sawAgent && sawRelay {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !sawAgent || !sawRelay {
		t.Fatalf("agent %v relay %v", sawAgent, sawRelay)
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
