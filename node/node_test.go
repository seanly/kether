package node

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/seanly/kether/frame"
	"github.com/seanly/kether/id"
	"github.com/seanly/kether/pay"
)

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func mustKey(t *testing.T) *id.PrivateKey {
	t.Helper()
	k, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func startSeed(t *testing.T, ctx context.Context, access Access, price uint64, ln *pay.MemLightning) *Node {
	t.Helper()
	cfg := Config{
		Key: mustKey(t),
		Record: Record{
			Name: "echo", Types: []string{"echo"}, Access: access,
		},
		TTL: time.Hour,
	}
	if price > 0 {
		cfg.Record.Pay = []PayMethod{{Rail: "lightning", Amount: price}}
		cfg.Payee = ln
	}
	n, err := Start(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Close() })
	n.Handle(func(_ context.Context, c Call) (Result, error) {
		return Result{Body: append([]byte(nil), c.Body...)}, nil
	})
	return n
}

func startClient(t *testing.T, ctx context.Context, seeds []string, ln *pay.MemLightning) *Node {
	t.Helper()
	n, err := Start(ctx, Config{
		Key:        mustKey(t),
		Seeds:      seeds,
		Record:     Record{Name: "client", Types: []string{"client"}, Access: AccessPublic},
		Payer:      ln,
		MaxPayMsat: 100_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Close() })
	return n
}

func waitSearch(t *testing.T, ctx context.Context, n *Node, typ, want string) Peer {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		peers, err := n.Search(ctx, typ)
		if err == nil {
			for _, p := range peers {
				if p.ID == want {
					return p
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("did not find %s", want)
	return Peer{}
}

func TestUnreachableSeed(t *testing.T) {
	ctx := testCtx(t)
	k := mustKey(t)
	pid, err := k.Public().PeerID()
	if err != nil {
		t.Fatal(err)
	}
	_, err = Start(ctx, Config{
		Key:    mustKey(t),
		Seeds:  []string{"/ip4/127.0.0.1/tcp/1/p2p/" + pid.String()},
		Record: Record{Types: []string{"echo"}, Access: AccessPublic},
	})
	if err == nil {
		t.Fatal("expected seed failure")
	}
}

func TestRecordTooBig(t *testing.T) {
	ctx := testCtx(t)
	_, err := Start(ctx, Config{
		Key: mustKey(t),
		Record: Record{
			Types: []string{"echo"},
			Pay:   []PayMethod{{Rail: "lightning", Hint: strings.Repeat("h", 9000), Amount: 1}},
		},
		Payee: pay.NewMemLightning(),
	})
	if err == nil {
		t.Fatal("expected size error")
	}
}

func TestDiscoverAndInvoke(t *testing.T) {
	ctx := testCtx(t)
	ln := pay.NewMemLightning()
	a := startSeed(t, ctx, AccessPublic, 1000, ln)
	b := startClient(t, ctx, a.Addrs(), ln)
	p := waitSearch(t, ctx, b, "echo", a.ID())
	got, err := b.Resolve(ctx, a.ID())
	if err != nil || got.ID != p.ID {
		t.Fatal(err)
	}
	bad, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	rec := a.current()
	blob, err := rec.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	blob[len(blob)-1] ^= 0xff
	if err := b.dht.PutValue(ctx, "/kether/agent/1/"+a.ID(), blob); err == nil {
		t.Fatal("expected forged record to be rejected")
	}
	_ = bad
	sess, err := b.Connect(ctx, a.ID())
	if err != nil {
		t.Fatal(err)
	}
	res, err := sess.Invoke(ctx, Request{Type: "echo", Body: []byte("hi")})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Body) != "hi" {
		t.Fatalf("body %q", res.Body)
	}
	res, err = sess.Invoke(ctx, Request{Type: "echo", Body: []byte("again")})
	if err != nil || string(res.Body) != "again" {
		t.Fatalf("reuse %v %q", err, res.Body)
	}
}

func TestGrantOnly(t *testing.T) {
	ctx := testCtx(t)
	ln := pay.NewMemLightning()
	a := startSeed(t, ctx, AccessGrantOnly, 1000, ln)
	b := startClient(t, ctx, a.Addrs(), ln)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		peers, err := b.Search(ctx, "echo")
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range peers {
			if p.ID == a.ID() {
				t.Fatal("grant_only agent was searchable")
			}
		}
		if _, err := b.Resolve(ctx, a.ID()); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := b.Resolve(ctx, a.ID()); err != nil {
		t.Fatal(err)
	}
	sess, err := b.Connect(ctx, a.ID())
	if err != nil {
		t.Fatal(err)
	}
	_, err = sess.Invoke(ctx, Request{Type: "echo", Body: []byte("x")})
	var ce *CallError
	if !asCall(err, &ce) || ce.Code != frame.CodeUnauth {
		t.Fatalf("err %#v", err)
	}
	g, err := a.Grant(b.ID(), Scope{Types: []string{"echo"}, Until: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	res, err := sess.Invoke(ctx, Request{Type: "echo", Body: []byte("ok"), Grant: g})
	if err != nil || string(res.Body) != "ok" {
		t.Fatalf("%v %q", err, res.Body)
	}
}

func TestPaymentGatesHandler(t *testing.T) {
	ctx := testCtx(t)
	ln := pay.NewMemLightning()
	var calls atomic.Int32
	a := startSeed(t, ctx, AccessPublic, 1000, ln)
	a.Handle(func(_ context.Context, c Call) (Result, error) {
		calls.Add(1)
		return Result{Body: c.Body}, nil
	})
	b, err := Start(ctx, Config{
		Key: mustKey(t), Seeds: a.Addrs(),
		Record:     Record{Types: []string{"client"}},
		Payer:      ln,
		MaxPayMsat: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	waitSearch(t, ctx, b, "echo", a.ID())
	sess, err := b.Connect(ctx, a.ID())
	if err != nil {
		t.Fatal(err)
	}
	_, err = sess.Invoke(ctx, Request{Type: "echo", Body: []byte("nope")})
	if err == nil {
		t.Fatal("expected payment cap")
	}
	if calls.Load() != 0 {
		t.Fatal("handler ran without payment")
	}
}

func TestBadHello(t *testing.T) {
	ctx := testCtx(t)
	a := startSeed(t, ctx, AccessPublic, 0, nil)
	b := startClient(t, ctx, a.Addrs(), nil)
	s, err := b.host.NewStream(ctx, a.host.ID(), protoID)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_ = s.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := frame.Read(s); err != nil {
		t.Fatal(err)
	}
	if err := frame.Write(s, &frame.Envelope{Kind: frame.KindHello, Hello: frame.Hello{
		AgentID: b.ID(), Nonce: make([]byte, 32), Signature: make([]byte, 64),
	}}); err != nil {
		t.Fatal(err)
	}
	env, err := frame.Read(s)
	if err != nil {
		t.Fatal(err)
	}
	if env.Kind != frame.KindError || env.Error.Code != frame.CodeVerify {
		t.Fatalf("%+v", env.Error)
	}
}

func TestPeerMismatch(t *testing.T) {
	ctx := testCtx(t)
	a := startSeed(t, ctx, AccessPublic, 0, nil)
	c := startClient(t, ctx, a.Addrs(), nil)
	other := startSeed(t, ctx, AccessPublic, 0, nil)
	s, err := c.host.NewStream(ctx, a.host.ID(), protoID)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_ = s.SetDeadline(time.Now().Add(5 * time.Second))
	envHello, err := frame.Read(s)
	if err != nil {
		t.Fatal(err)
	}
	nonceC := make([]byte, 32)
	sig, err := id.Sign(c.key, id.DomainHello, helloMaterial(envHello.Hello.AgentID, envHello.Hello.Nonce, nonceC))
	if err != nil {
		t.Fatal(err)
	}
	if err := frame.Write(s, &frame.Envelope{Kind: frame.KindHello, Hello: frame.Hello{
		AgentID: other.ID(), Nonce: nonceC, Signature: sig,
	}}); err != nil {
		t.Fatal(err)
	}
	env, err := frame.Read(s)
	if err != nil {
		t.Fatal(err)
	}
	if env.Kind != frame.KindError || env.Error.Code != frame.CodeVerify {
		t.Fatalf("code %d %s", env.Error.Code, env.Error.Message)
	}
}

func TestCloseStopsRefresh(t *testing.T) {
	ctx := testCtx(t)
	n, err := Start(ctx, Config{
		Key:    mustKey(t),
		Record: Record{Types: []string{"echo"}},
		TTL:    2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_ = n.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("close did not return")
	}
}

func TestNonce(t *testing.T) {
	n := &Node{nonces: map[string]time.Time{}}
	nonce := []byte("0123456789abcdef")
	if err := n.rememberNonce("abc", nonce, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := n.rememberNonce("abc", nonce, time.Now()); err == nil {
		t.Fatal("expected duplicate")
	}
}

func TestBitcoinGate(t *testing.T) {
	ctx := testCtx(t)
	chain := pay.NewMemChain(1)
	var calls atomic.Int32
	a, err := Start(ctx, Config{
		Key: mustKey(t),
		Record: Record{
			Types: []string{"echo"}, Access: AccessPublic,
			Pay: []PayMethod{{Rail: "bitcoin", Amount: 1000}},
		},
		Payee: chain,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	a.Handle(func(_ context.Context, c Call) (Result, error) {
		calls.Add(1)
		return Result{Body: c.Body}, nil
	})
	low := payerFunc(func(ch pay.Challenge) ([]byte, error) {
		chain.Fund(ch.Invoice, ch.Amount, 0)
		return []byte("tx"), nil
	})
	b, err := Start(ctx, Config{
		Key: mustKey(t), Seeds: a.Addrs(),
		Record: Record{Types: []string{"client"}},
		Payer:  low,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	waitSearch(t, ctx, b, "echo", a.ID())
	sess, err := b.Connect(ctx, a.ID())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Invoke(ctx, Request{Type: "echo", Body: []byte("x")}); err == nil {
		t.Fatal("expected unconfirmed payment to fail")
	}
	if calls.Load() != 0 {
		t.Fatal("handler ran")
	}
	_ = sess.Close()
	b.cfg.Payer = chain
	sess, err = b.Connect(ctx, a.ID())
	if err != nil {
		t.Fatal(err)
	}
	res, err := sess.Invoke(ctx, Request{Type: "echo", Body: []byte("paid")})
	if err != nil || string(res.Body) != "paid" || calls.Load() != 1 {
		t.Fatalf("paid %v %q calls %d", err, res.Body, calls.Load())
	}
}

type payerFunc func(pay.Challenge) ([]byte, error)

func (f payerFunc) Pay(_ context.Context, ch pay.Challenge) ([]byte, error) { return f(ch) }

func asCall(err error, target **CallError) bool {
	if err == nil {
		return false
	}
	ce, ok := err.(*CallError)
	if ok {
		*target = ce
	}
	return ok
}

var _ network.Stream
