package node

import "testing"

func TestChooseAddrs(t *testing.T) {
	circuitA := "/ip4/203.0.113.10/tcp/1/p2p-circuit"
	circuitB := "/ip4/203.0.113.11/tcp/1/p2p-circuit"
	circuitC := "/ip4/203.0.113.12/tcp/1/p2p-circuit"
	op := "/ip4/198.51.100.8/tcp/9"

	got := chooseAddrs(ReachabilityAuto, nil, []string{
		"/ip4/0.0.0.0/tcp/1",
		"/ip6/::/tcp/1",
		"/ip4/127.0.0.1/tcp/2",
	}, nil)
	if len(got) != 1 || got[0] != "/ip4/127.0.0.1/tcp/2" {
		t.Fatalf("auto filter %q", got)
	}

	got = chooseAddrs(ReachabilityPrivate, nil, []string{
		"/ip4/127.0.0.1/tcp/2",
		"/ip4/192.168.1.9/tcp/3",
		circuitA,
	}, nil)
	if len(got) != 1 || got[0] != circuitA {
		t.Fatalf("private filter %q", got)
	}

	got = chooseAddrs(ReachabilityPrivate, nil, []string{"/ip4/127.0.0.1/tcp/2"}, nil)
	if len(got) != 1 || got[0] != "/ip4/127.0.0.1/tcp/2" {
		t.Fatalf("private without circuit %q", got)
	}

	host := []string{"/ip4/127.0.0.1/tcp/2", circuitA}
	got = chooseAddrs(ReachabilityAuto, []string{op}, host, nil)
	if got[0] != op {
		t.Fatalf("operator not first: %q", got)
	}
	if len(got) < 2 || got[len(got)-1] != circuitA {
		t.Fatalf("circuit not last: %q", got)
	}

	var many []string
	for _, host := range []string{
		"/ip4/198.51.100.1/tcp/1",
		"/ip4/198.51.100.2/tcp/1",
		"/ip4/198.51.100.3/tcp/1",
		"/ip4/198.51.100.4/tcp/1",
		"/ip4/198.51.100.5/tcp/1",
		"/ip4/198.51.100.6/tcp/1",
		"/ip4/198.51.100.7/tcp/1",
		"/ip4/198.51.100.8/tcp/1",
		"/ip4/198.51.100.9/tcp/1",
	} {
		many = append(many, host)
	}
	got = chooseAddrs(ReachabilityAuto, []string{op}, many, nil)
	if len(got) != maxAddrs {
		t.Fatalf("len %d", len(got))
	}
	if got[0] != op {
		t.Fatalf("truncated head %q", got[0])
	}

	got = chooseAddrs(ReachabilityAuto, nil, []string{circuitA, circuitB, circuitC}, nil)
	if len(got) != maxCircuit || got[0] != circuitA || got[1] != circuitB {
		t.Fatalf("circuit cap %q", got)
	}

	prev := []string{"/ip4/198.51.100.20/tcp/1"}
	got = chooseAddrs(ReachabilityAuto, nil, []string{"/ip4/0.0.0.0/tcp/1"}, prev)
	if len(got) != 1 || got[0] != prev[0] {
		t.Fatalf("kept previous %q", got)
	}
}

func TestUnchangedAddrsDoNotBumpSeq(t *testing.T) {
	ctx := testCtx(t)
	n := startSeed(t, ctx, AccessPublic, 0, nil)
	defer n.Close()
	if n.current().Seq != 1 {
		t.Fatalf("seq %d", n.current().Seq)
	}
	n.republishIfChanged()
	if n.current().Seq != 1 {
		t.Fatalf("seq bumped to %d", n.current().Seq)
	}
}
