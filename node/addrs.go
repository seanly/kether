package node

import (
	"strings"

	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

const (
	maxAddrs   = 8
	maxCircuit = 2
)

func isCircuit(addr string) bool {
	return strings.Contains(addr, "/p2p-circuit")
}

func dropUnspecified(addr string) bool {
	m, err := ma.NewMultiaddr(addr)
	if err != nil {
		return false
	}
	return manet.IsIPUnspecified(m)
}

func isLoopbackOrPrivate(addr string) bool {
	m, err := ma.NewMultiaddr(addr)
	if err != nil {
		return false
	}
	return manet.IsIPLoopback(m) || manet.IsPrivateAddr(m)
}

// chooseAddrs builds the address list written into a signed record.
// Operator addresses come first. At most two circuit addresses are kept.
// An empty filtered set keeps the previous list so a record is never published
// with no addresses.
func chooseAddrs(reach Reachability, operator, host, previous []string) []string {
	filtered := filterHost(reach, host)
	if len(operator) == 0 && len(filtered) == 0 {
		if len(previous) > 0 {
			return append([]string(nil), previous...)
		}
		return capList(host)
	}
	return mergeAddrs(operator, filtered)
}

func filterHost(reach Reachability, host []string) []string {
	var direct, circuit []string
	for _, addr := range host {
		if dropUnspecified(addr) {
			continue
		}
		if isCircuit(addr) {
			circuit = append(circuit, addr)
			continue
		}
		direct = append(direct, addr)
	}
	if reach == ReachabilityPrivate && len(circuit) > 0 {
		var kept []string
		for _, addr := range direct {
			if isLoopbackOrPrivate(addr) {
				continue
			}
			kept = append(kept, addr)
		}
		direct = kept
	}
	return append(direct, circuit...)
}

func mergeAddrs(operator, host []string) []string {
	var opDirect, hostDirect, opCircuit, hostCircuit []string
	split := func(addrs []string, direct, circuit *[]string) {
		for _, addr := range addrs {
			if isCircuit(addr) {
				*circuit = append(*circuit, addr)
			} else {
				*direct = append(*direct, addr)
			}
		}
	}
	split(operator, &opDirect, &opCircuit)
	split(host, &hostDirect, &hostCircuit)
	var out []string
	seen := map[string]struct{}{}
	circuits := 0
	add := func(addr string) {
		if _, ok := seen[addr]; ok {
			return
		}
		if len(out) >= maxAddrs {
			return
		}
		if isCircuit(addr) {
			if circuits >= maxCircuit {
				return
			}
			circuits++
		}
		seen[addr] = struct{}{}
		out = append(out, addr)
	}
	for _, group := range [][]string{opDirect, hostDirect, opCircuit, hostCircuit} {
		for _, addr := range group {
			add(addr)
		}
	}
	return out
}

func capList(addrs []string) []string {
	if len(addrs) > maxAddrs {
		addrs = addrs[:maxAddrs]
	}
	return append([]string(nil), addrs...)
}

func sameAddrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, addr := range a {
		seen[addr]++
	}
	for _, addr := range b {
		seen[addr]--
		if seen[addr] < 0 {
			return false
		}
	}
	return true
}

func orderDialAddrs(in []string) []string {
	var direct, circuit []string
	for _, addr := range in {
		if isCircuit(addr) {
			circuit = append(circuit, addr)
		} else {
			direct = append(direct, addr)
		}
	}
	return append(direct, circuit...)
}
