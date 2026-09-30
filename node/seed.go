package node

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/multiformats/go-multiaddr"
	mss "github.com/multiformats/go-multistream"

	"github.com/seanly/kether/id"
)

// normalizeSeeds turns host:port into a multiaddr that includes the peer ID
// learned from a Noise handshake. Multiaddrs that already name a peer are kept.
func normalizeSeeds(ctx context.Context, key *id.PrivateKey, seeds []string) ([]string, error) {
	var out []string
	var last error
	for _, seed := range seeds {
		norm, err := normalizeSeed(ctx, key, seed)
		if err != nil {
			last = err
			continue
		}
		out = append(out, norm)
	}
	if len(out) == 0 {
		if last == nil {
			last = fmt.Errorf("no seeds")
		}
		return nil, fmt.Errorf("kether: no reachable seed: %w", last)
	}
	return out, nil
}

func normalizeSeed(ctx context.Context, key *id.PrivateKey, seed string) (string, error) {
	if strings.HasPrefix(seed, "/") {
		ai, err := peer.AddrInfoFromString(seed)
		if err != nil {
			return "", fmt.Errorf("kether: seed: %w", err)
		}
		if ai.ID == "" {
			return "", fmt.Errorf("kether: seed: missing peer id")
		}
		return seed, nil
	}
	maddr, err := hostPortMultiaddr(seed)
	if err != nil {
		return "", err
	}
	pid, err := probePeerID(ctx, key, seed)
	if err != nil {
		return "", err
	}
	return maddr.String() + "/p2p/" + pid.String(), nil
}

func hostPortMultiaddr(hostport string) (multiaddr.Multiaddr, error) {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return nil, fmt.Errorf("kether: seed: %w", err)
	}
	ip := net.ParseIP(host)
	var raw string
	switch {
	case ip == nil:
		raw = fmt.Sprintf("/dns4/%s/tcp/%s", host, port)
	case ip.To4() != nil:
		raw = fmt.Sprintf("/ip4/%s/tcp/%s", ip.String(), port)
	default:
		raw = fmt.Sprintf("/ip6/%s/tcp/%s", ip.String(), port)
	}
	m, err := multiaddr.NewMultiaddr(raw)
	if err != nil {
		return nil, fmt.Errorf("kether: seed: %w", err)
	}
	return m, nil
}

// probePeerID dials TCP and reads the remote libp2p key from the Noise handshake.
func probePeerID(ctx context.Context, key *id.PrivateKey, hostport string) (peer.ID, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", hostport)
	if err != nil {
		return "", fmt.Errorf("kether: seed: %w", err)
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	if err := mss.SelectProtoOrFail(string(noise.ID), conn); err != nil {
		return "", fmt.Errorf("kether: seed: %w", err)
	}
	priv, err := key.Libp2p()
	if err != nil {
		return "", err
	}
	tpt, err := noise.New(noise.ID, priv, nil)
	if err != nil {
		return "", err
	}
	st, err := tpt.WithSessionOptions(noise.DisablePeerIDCheck())
	if err != nil {
		return "", err
	}
	secure, err := st.SecureOutbound(ctx, conn, "")
	if err != nil {
		return "", fmt.Errorf("kether: seed: %w", err)
	}
	defer secure.Close()
	pid := secure.RemotePeer()
	if pid == "" {
		return "", fmt.Errorf("kether: seed: empty peer id")
	}
	return pid, nil
}
