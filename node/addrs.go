package node

import "strings"

const maxCircuit = 2

func isCircuit(addr string) bool {
	return strings.Contains(addr, "/p2p-circuit")
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
