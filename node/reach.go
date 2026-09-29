package node

import (
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/host/autorelay"
	relayv2 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
)

// Reachability selects how a node learns and publishes dialable addresses.
type Reachability int

const (
	// ReachabilityAuto keeps today's behavior when Seeds is empty. With seeds,
	// the node probes reachability, reserves a relay slot, and punches holes.
	ReachabilityAuto Reachability = iota
	// ReachabilityPublic is an operator-declared public node: AutoNAT dial-back
	// and a circuit v2 relay service.
	ReachabilityPublic
	// ReachabilityPrivate skips probing and reserves a relay slot immediately.
	ReachabilityPrivate
)

// hostOptHook adds libp2p options for tests. Production leaves it nil.
var hostOptHook func(Config) []libp2p.Option

func hostOptions(cfg Config) []libp2p.Option {
	opts := reachOptions(cfg)
	if hostOptHook != nil {
		opts = append(opts, hostOptHook(cfg)...)
	}
	return opts
}

func reachOptions(cfg Config) []libp2p.Option {
	switch cfg.Reachability {
	case ReachabilityPublic:
		return []libp2p.Option{
			libp2p.ForceReachabilityPublic(),
			libp2p.EnableRelayService(relayv2.WithInfiniteLimits()),
			libp2p.EnableNATService(),
		}
	case ReachabilityPrivate:
		return append([]libp2p.Option{libp2p.ForceReachabilityPrivate()}, relayClientOptions(cfg.Seeds)...)
	default:
		if len(cfg.Seeds) == 0 {
			return nil
		}
		return relayClientOptions(cfg.Seeds)
	}
}

func relayClientOptions(seeds []string) []libp2p.Option {
	opts := []libp2p.Option{
		libp2p.EnableAutoNATv2(),
		libp2p.EnableHolePunching(),
	}
	infos := relayInfos(seeds)
	if len(infos) == 0 {
		return opts
	}
	n := len(infos)
	if n > maxCircuit {
		n = maxCircuit
	}
	return append(opts, libp2p.EnableAutoRelayWithStaticRelays(infos,
		autorelay.WithNumRelays(n),
		autorelay.WithBootDelay(0),
	))
}

func relayInfos(seeds []string) []peer.AddrInfo {
	var out []peer.AddrInfo
	seen := map[peer.ID]struct{}{}
	for _, seed := range seeds {
		ai, err := peer.AddrInfoFromString(seed)
		if err != nil || ai.ID == "" {
			continue
		}
		if _, ok := seen[ai.ID]; ok {
			continue
		}
		seen[ai.ID] = struct{}{}
		out = append(out, *ai)
	}
	return out
}
