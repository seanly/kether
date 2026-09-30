package node

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/control"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

// libp2p only advertises circuit addresses for public relay addresses.
// Rewriting the loopback listen address to this name lets a local test
// reserve a slot and still dial the relay.
const relayName = "libp2p.internal"

type internalResolver struct{}

func (internalResolver) ResolveDNSAddr(context.Context, peer.ID, ma.Multiaddr, int, int) ([]ma.Multiaddr, error) {
	return nil, nil
}

func (internalResolver) ResolveDNSComponent(_ context.Context, m ma.Multiaddr, _ int) ([]ma.Multiaddr, error) {
	out := strings.ReplaceAll(m.String(), "/dns4/"+relayName, "/ip4/127.0.0.1")
	out = strings.ReplaceAll(out, "/dns/"+relayName, "/ip4/127.0.0.1")
	parsed, err := ma.NewMultiaddr(out)
	if err != nil {
		return nil, err
	}
	return []ma.Multiaddr{parsed}, nil
}

func rewriteLoopback(addrs []ma.Multiaddr) []ma.Multiaddr {
	out := make([]ma.Multiaddr, len(addrs))
	for i, addr := range addrs {
		s := addr.String()
		if strings.HasPrefix(s, "/ip4/127.0.0.1/") {
			out[i] = ma.StringCast("/dns4/" + relayName + strings.TrimPrefix(s, "/ip4/127.0.0.1"))
			continue
		}
		out[i] = addr
	}
	return out
}

type directGate struct{ block peer.ID }

func (g *directGate) InterceptPeerDial(peer.ID) bool { return true }

func (g *directGate) InterceptAddrDial(p peer.ID, addr ma.Multiaddr) bool {
	return p != g.block || strings.Contains(addr.String(), "/p2p-circuit")
}

func (g *directGate) InterceptAccept(network.ConnMultiaddrs) bool { return true }

func (g *directGate) InterceptSecured(_ network.Direction, p peer.ID, c network.ConnMultiaddrs) bool {
	return p != g.block || strings.Contains(c.RemoteMultiaddr().String(), "/p2p-circuit")
}

func (g *directGate) InterceptUpgraded(network.Conn) (bool, control.DisconnectReason) {
	return true, 0
}

func TestRelayHandshake(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	var block peer.ID
	hostOptHook = func(cfg Config) []libp2p.Option {
		opts := []libp2p.Option{libp2p.MultiaddrResolver(internalResolver{})}
		switch cfg.Reachability {
		case ReachabilityPublic:
			opts = append(opts, libp2p.AddrsFactory(rewriteLoopback))
		case ReachabilityPrivate:
			opts = append(opts, libp2p.ConnectionGater(&directGate{block: block}))
		}
		return opts
	}
	t.Cleanup(func() { hostOptHook = nil })

	a, err := Start(ctx, Config{
		Key:          mustKey(t),
		Listen:       []string{"/ip4/127.0.0.1/tcp/0"},
		Reachability: ReachabilityPublic,
		Record:       Record{Types: []string{"echo"}, Access: AccessPublic},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	bKey := mustKey(t)
	cKey := mustKey(t)
	bID, err := bKey.Public().PeerID()
	if err != nil {
		t.Fatal(err)
	}
	cID, err := cKey.Public().PeerID()
	if err != nil {
		t.Fatal(err)
	}
	block = cID
	b, err := Start(ctx, Config{
		Key:          bKey,
		Seeds:        a.Addrs(),
		Listen:       []string{"/ip4/127.0.0.1/tcp/0"},
		Reachability: ReachabilityPrivate,
		Record:       Record{Types: []string{"echo"}, Access: AccessPublic},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	block = bID
	c, err := Start(ctx, Config{
		Key:          cKey,
		Seeds:        a.Addrs(),
		Listen:       []string{"/ip4/127.0.0.1/tcp/0"},
		Reachability: ReachabilityPrivate,
		Record:       Record{Types: []string{"echo"}, Access: AccessPublic},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	blob, err := b.current().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), "/ip4/") || strings.Contains(string(blob), "p2p-circuit") {
		t.Fatal("record contains a multiaddr")
	}

	deadline := time.Now().Add(20 * time.Second)
	var sess *Session
	for time.Now().Before(deadline) {
		s, err := c.Connect(ctx, b.ID())
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		remote := s.s.Conn().RemoteMultiaddr().String()
		if strings.Contains(remote, "/p2p-circuit") {
			sess = s
			break
		}
		_ = s.Close()
		time.Sleep(50 * time.Millisecond)
	}
	if sess == nil {
		t.Fatal("no circuit handshake")
	}
	defer sess.Close()
}
