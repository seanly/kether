package node

import "testing"

func TestOrderDialAddrs(t *testing.T) {
	circuit := "/ip4/203.0.113.10/tcp/4001/p2p/relay/p2p-circuit"
	direct := "/ip4/198.51.100.20/tcp/1"
	got := orderDialAddrs([]string{circuit, direct})
	if len(got) != 2 || got[0] != direct || got[1] != circuit {
		t.Fatalf("order %q", got)
	}
}
