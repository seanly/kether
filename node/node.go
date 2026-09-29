package node

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	kaddht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	"github.com/seanly/kether/auth"
	"github.com/seanly/kether/dht"
	"github.com/seanly/kether/id"
	"github.com/seanly/kether/pay"
	"github.com/seanly/kether/record"
)

const protoID = protocol.ID("/kether/1.0.0")

type Access uint32

const (
	AccessPublic    Access = 0
	AccessGrantOnly Access = 1
)

type PayMethod struct {
	Rail   string
	Hint   string
	Amount uint64
}

// Record is the record published at start. Addresses come from the host when empty.
type Record struct {
	Name   string
	Types  []string
	Access Access
	Pay    []PayMethod
	Addrs  []string
}

type Config struct {
	Key        *id.PrivateKey
	Seeds      []string
	Listen     []string
	Record     Record
	Payee      pay.Payee
	Payer      pay.Payer
	MaxPayMsat uint64
	TTL        time.Duration
}

type Peer struct {
	ID        string
	Name      string
	Types     []string
	Access    Access
	Addrs     []string
	Pay       []PayMethod
	Seq       uint64
	ExpiresAt time.Time
}

type Scope struct {
	Types     []string
	NotBefore time.Time
	Until     time.Time
	MaxCalls  uint64
	MaxAmount uint64
}

type Grant []byte

type Call struct {
	Caller  string
	Grant   Grant
	Receipt []byte
	Rail    string
	Type    string
	Body    []byte
}

type Result struct {
	Body []byte
}

type CallError struct {
	Code    uint32
	Message string
}

func (e *CallError) Error() string {
	return fmt.Sprintf("kether: call %d: %s", e.Code, e.Message)
}

type Handler func(context.Context, Call) (Result, error)

type Node struct {
	cfg     Config
	key     *id.PrivateKey
	agentID string
	host    host.Host
	dht     *kaddht.IpfsDHT
	ctx     context.Context
	cancel  context.CancelFunc
	exited  chan struct{}

	mu      sync.RWMutex
	rec     *record.Record
	seq     uint64
	handler Handler
	checker auth.Checker
	nonces  map[string]time.Time
}

func Start(ctx context.Context, cfg Config) (*Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cfg.Key == nil {
		return nil, errors.New("kether: key is required")
	}
	if len(cfg.Record.Types) == 0 {
		return nil, errors.New("kether: record types are required")
	}
	if len(cfg.Record.Pay) > 0 && cfg.Payee == nil {
		return nil, errors.New("kether: payee is required when the record has a price")
	}
	if cfg.TTL <= 0 {
		cfg.TTL = time.Hour
	}
	if len(cfg.Listen) == 0 {
		cfg.Listen = []string{"/ip4/127.0.0.1/tcp/0"}
	}
	lp, err := cfg.Key.Libp2p()
	if err != nil {
		return nil, err
	}
	h, err := libp2p.New(
		libp2p.Identity(lp),
		libp2p.ListenAddrStrings(cfg.Listen...),
	)
	if err != nil {
		return nil, err
	}
	kd, err := kaddht.New(ctx, h,
		kaddht.Mode(kaddht.ModeServer),
		kaddht.ProtocolPrefix("/kether"),
		kaddht.DisableAutoRefresh(),
		kaddht.NamespacedValidator("kether", dht.Validator{}),
	)
	if err != nil {
		h.Close()
		return nil, err
	}
	aid, err := cfg.Key.Public().AgentID()
	if err != nil {
		kd.Close()
		h.Close()
		return nil, err
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	n := &Node{
		cfg:     cfg,
		key:     cfg.Key,
		agentID: aid,
		host:    h,
		dht:     kd,
		ctx:     runCtx,
		cancel:  cancel,
		exited:  make(chan struct{}),
		nonces:  map[string]time.Time{},
		seq:     1,
	}
	if err := n.signLocked(time.Now()); err != nil {
		n.shutdown()
		return nil, err
	}
	n.host.SetStreamHandler(protoID, n.onStream)
	if len(cfg.Seeds) > 0 {
		if err := n.dialSeeds(ctx); err != nil {
			n.shutdown()
			return nil, err
		}
	}
	if err := n.publish(ctx); err != nil {
		n.shutdown()
		return nil, err
	}
	go n.refresh()
	return n, nil
}

func (n *Node) signLocked(now time.Time) error {
	addrs := n.cfg.Record.Addrs
	if len(addrs) == 0 {
		addrs = n.Addrs()
	}
	pay := make([]record.PayMethod, len(n.cfg.Record.Pay))
	for i, p := range n.cfg.Record.Pay {
		pay[i] = record.PayMethod{Rail: p.Rail, Hint: p.Hint, Amount: p.Amount}
	}
	rec := &record.Record{
		Seq:       n.seq,
		ExpiresAt: now.Add(n.cfg.TTL).Unix(),
		Name:      n.cfg.Record.Name,
		Types:     append([]string(nil), n.cfg.Record.Types...),
		Access:    uint32(n.cfg.Record.Access),
		Addrs:     addrs,
		Pay:       pay,
	}
	if err := record.Sign(n.key, rec); err != nil {
		return err
	}
	blob, err := rec.Marshal()
	if err != nil {
		return err
	}
	if len(blob) > record.MaxBytes {
		return errors.New("record: larger than 8KiB")
	}
	n.rec = rec
	return nil
}

func (n *Node) publish(ctx context.Context) error {
	n.mu.RLock()
	rec := n.rec
	n.mu.RUnlock()
	blob, err := rec.Marshal()
	if err != nil {
		return err
	}
	aid, err := rec.AgentID()
	if err != nil {
		return err
	}
	if err := loneTable(n.dht.PutValue(ctx, dht.AgentKey(aid), blob)); err != nil {
		return err
	}
	if err := loneTable(n.dht.PutValue(ctx, dht.PeerKey(n.host.ID()), blob)); err != nil {
		return err
	}
	if rec.Access != record.AccessPublic {
		return nil
	}
	for _, typ := range rec.Types {
		c, err := dht.TypeCID(typ)
		if err != nil {
			return err
		}
		if err := loneTable(n.dht.Provide(ctx, c, true)); err != nil {
			return err
		}
	}
	return nil
}

func loneTable(err error) error {
	if err == nil || strings.Contains(err.Error(), "failed to find any peer in table") {
		return nil
	}
	return err
}

func (n *Node) dialSeeds(ctx context.Context) error {
	var last error
	ok := false
	for _, s := range n.cfg.Seeds {
		ai, err := peer.AddrInfoFromString(s)
		if err != nil {
			last = err
			continue
		}
		if err := n.host.Connect(ctx, *ai); err != nil {
			last = err
			continue
		}
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) && n.dht.RoutingTable().Find(ai.ID) == "" {
			time.Sleep(10 * time.Millisecond)
		}
		ok = true
	}
	if !ok {
		if last == nil {
			last = errors.New("no seeds")
		}
		return fmt.Errorf("kether: no reachable seed: %w", last)
	}
	return nil
}

func (n *Node) refresh() {
	defer close(n.exited)
	interval := n.cfg.TTL / 2
	if interval < time.Second {
		interval = time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-n.ctx.Done():
			return
		case <-t.C:
			n.mu.Lock()
			n.seq++
			err := n.signLocked(time.Now())
			n.mu.Unlock()
			if err != nil {
				continue
			}
			_ = n.publish(n.ctx)
		}
	}
}

func (n *Node) Close() error {
	n.cancel()
	<-n.exited
	return n.shutdown()
}

func (n *Node) shutdown() error {
	var err error
	if n.dht != nil {
		err = n.dht.Close()
	}
	if n.host != nil {
		if e := n.host.Close(); err == nil {
			err = e
		}
	}
	return err
}

func (n *Node) ID() string { return n.agentID }

func (n *Node) Addrs() []string {
	if n.host == nil {
		return nil
	}
	var out []string
	for _, a := range n.host.Addrs() {
		out = append(out, a.String()+"/p2p/"+n.host.ID().String())
	}
	return out
}

func (n *Node) Handle(fn Handler) {
	n.mu.Lock()
	n.handler = fn
	n.mu.Unlock()
}

func (n *Node) current() *record.Record {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.rec
}

func (n *Node) Resolve(ctx context.Context, agentID string) (Peer, error) {
	if agentID == n.agentID {
		return peerFrom(n.current()), nil
	}
	val, err := n.dht.GetValue(ctx, dht.AgentKey(agentID))
	if err != nil {
		return Peer{}, err
	}
	rec, err := record.Unmarshal(val)
	if err != nil {
		return Peer{}, err
	}
	if err := rec.Verify(time.Now()); err != nil {
		return Peer{}, err
	}
	got, err := rec.AgentID()
	if err != nil || got != agentID {
		return Peer{}, errors.New("kether: record agent id mismatch")
	}
	return peerFrom(rec), nil
}

func (n *Node) Search(ctx context.Context, typ string) ([]Peer, error) {
	c, err := dht.TypeCID(typ)
	if err != nil {
		return nil, err
	}
	infos, err := n.dht.FindProviders(ctx, c)
	if err != nil {
		return nil, err
	}
	var out []Peer
	seen := map[string]struct{}{}
	consider := func(rec *record.Record) {
		if rec == nil || rec.Access != record.AccessPublic || !rec.HasType(typ) {
			return
		}
		if err := rec.Verify(time.Now()); err != nil {
			return
		}
		p := peerFrom(rec)
		if _, ok := seen[p.ID]; ok {
			return
		}
		seen[p.ID] = struct{}{}
		out = append(out, p)
	}
	if self := n.current(); self != nil {
		consider(self)
	}
	for _, info := range infos {
		if info.ID == n.host.ID() {
			continue
		}
		val, err := n.dht.GetValue(ctx, dht.PeerKey(info.ID))
		if err != nil {
			continue
		}
		rec, err := record.Unmarshal(val)
		if err != nil {
			continue
		}
		consider(rec)
	}
	sortPeers(out)
	return out, nil
}

func sortPeers(ps []Peer) {
	for i := 1; i < len(ps); i++ {
		j := i
		for j > 0 && ps[j].Seq > ps[j-1].Seq {
			ps[j], ps[j-1] = ps[j-1], ps[j]
			j--
		}
	}
}

func peerFrom(rec *record.Record) Peer {
	if rec == nil {
		return Peer{}
	}
	aid, _ := rec.AgentID()
	pay := make([]PayMethod, len(rec.Pay))
	for i, p := range rec.Pay {
		pay[i] = PayMethod{Rail: p.Rail, Hint: p.Hint, Amount: p.Amount}
	}
	return Peer{
		ID: aid, Name: rec.Name, Types: append([]string(nil), rec.Types...),
		Access: Access(rec.Access), Addrs: append([]string(nil), rec.Addrs...),
		Pay: pay, Seq: rec.Seq, ExpiresAt: time.Unix(rec.ExpiresAt, 0),
	}
}

func (n *Node) Grant(granteeID string, scope Scope) (Grant, error) {
	pub, err := id.ParseAgentID(granteeID)
	if err != nil {
		return nil, err
	}
	if scope.Until.IsZero() {
		return nil, errors.New("kether: grant until is required")
	}
	notBefore := scope.NotBefore
	if notBefore.IsZero() {
		notBefore = time.Now()
	}
	g := &auth.Grant{
		Grantee:   pub.XOnly(),
		Types:     append([]string(nil), scope.Types...),
		NotBefore: notBefore.Unix(),
		NotAfter:  scope.Until.Unix(),
		MaxCalls:  scope.MaxCalls,
		MaxAmount: scope.MaxAmount,
	}
	if err := auth.Sign(n.key, g); err != nil {
		return nil, err
	}
	return Grant(g.Marshal()), nil
}
