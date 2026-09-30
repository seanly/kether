package node

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/seanly/kether/auth"
	"github.com/seanly/kether/frame"
	"github.com/seanly/kether/id"
	"github.com/seanly/kether/pay"
	"github.com/seanly/kether/record"
)

type Session struct {
	node      *Node
	s         network.Stream
	remote    Peer
	remotePub *id.Public
	mu        sync.Mutex
	next      atomic.Uint64
}

type Request struct {
	Type  string
	Body  []byte
	Grant Grant
}

func (n *Node) Connect(ctx context.Context, agentID string) (*Session, error) {
	rec, err := n.lookup(ctx, agentID)
	if err != nil {
		return nil, err
	}
	pid, err := peer.Decode(rec.PeerID)
	if err != nil {
		return nil, err
	}
	pub, err := id.ParseXOnly(rec.Pubkey)
	if err != nil {
		return nil, err
	}
	info, err := n.dialInfo(ctx, pid)
	if err != nil {
		return nil, err
	}
	if err := n.host.Connect(ctx, info); err != nil {
		return nil, err
	}
	s, err := n.host.NewStream(ctx, pid, protoID)
	if err != nil {
		return nil, err
	}
	sess := &Session{node: n, s: s, remote: peerFrom(rec), remotePub: pub}
	if err := sess.handshakeClient(ctx); err != nil {
		s.Close()
		return nil, err
	}
	return sess, nil
}

// dialInfo asks the DHT for the peer, then falls back to addresses already
// learned from a seed dial. Direct addresses are tried before circuit addresses.
func (n *Node) dialInfo(ctx context.Context, pid peer.ID) (peer.AddrInfo, error) {
	info, ferr := n.dht.FindPeer(ctx, pid)
	if ferr != nil || len(info.Addrs) == 0 {
		info = n.host.Peerstore().PeerInfo(pid)
	}
	if len(info.Addrs) == 0 {
		if ferr == nil {
			ferr = errors.New("kether: peer has no address")
		}
		return peer.AddrInfo{}, ferr
	}
	info.ID = pid
	info.Addrs = orderMultiaddrs(info.Addrs)
	return info, nil
}

func orderMultiaddrs(in []ma.Multiaddr) []ma.Multiaddr {
	raw := make([]string, len(in))
	for i, addr := range in {
		raw[i] = addr.String()
	}
	raw = orderDialAddrs(raw)
	out := make([]ma.Multiaddr, 0, len(raw))
	for _, addr := range raw {
		m, err := ma.NewMultiaddr(addr)
		if err != nil {
			continue
		}
		out = append(out, m)
	}
	return out
}

func (s *Session) Close() error { return s.s.Close() }

func (n *Node) onStream(s network.Stream) {
	defer s.Close()
	_ = s.SetDeadline(time.Now().Add(30 * time.Second))
	callerID, err := n.handshakeServer(s)
	if err != nil {
		return
	}
	callerPub, err := xonlyFromConn(s)
	if err != nil {
		return
	}
	for {
		env, err := frame.Read(s)
		if err != nil {
			return
		}
		n.serveInvoke(s, callerPub, callerID, env)
	}
}

func (n *Node) handshakeServer(s network.Stream) (string, error) {
	nonceS := make([]byte, 32)
	if _, err := rand.Read(nonceS); err != nil {
		return "", err
	}
	if err := frame.Write(s, &frame.Envelope{Kind: frame.KindHello, Hello: frame.Hello{AgentID: n.agentID, Nonce: nonceS}}); err != nil {
		return "", err
	}
	env, err := frame.Read(s)
	if err != nil {
		return "", err
	}
	if env.Kind != frame.KindHello {
		return "", n.fail(s, 0, frame.CodeFrame, "expected hello")
	}
	pub, err := pubFromConn(s)
	if err != nil {
		return "", n.fail(s, 0, frame.CodeVerify, "peer mismatch")
	}
	nonceC := env.Hello.Nonce
	msg := helloMaterial(n.agentID, nonceS, nonceC)
	if !id.Verify(pub, id.DomainHello, msg, env.Hello.Signature) {
		return "", n.fail(s, 0, frame.CodeVerify, "bad hello signature")
	}
	lookCtx, cancel := context.WithTimeout(n.ctx, 5*time.Second)
	defer cancel()
	rec, err := n.lookup(lookCtx, env.Hello.AgentID)
	if err != nil || rec.PeerID != s.Conn().RemotePeer().String() {
		return "", n.fail(s, 0, frame.CodeVerify, "peer mismatch")
	}
	sig, err := id.Sign(n.key, id.DomainHello, helloMaterial(env.Hello.AgentID, nonceS, nonceC))
	if err != nil {
		return "", err
	}
	if err := frame.Write(s, &frame.Envelope{Kind: frame.KindHelloAck, Ack: frame.HelloAck{Signature: sig}}); err != nil {
		return "", err
	}
	return env.Hello.AgentID, nil
}

func (s *Session) handshakeClient(ctx context.Context) error {
	if dl, ok := ctx.Deadline(); ok {
		_ = s.s.SetDeadline(dl)
	}
	env, err := frame.Read(s.s)
	if err != nil {
		return err
	}
	if env.Kind != frame.KindHello {
		return errors.New("kether: expected hello")
	}
	if env.Hello.AgentID != s.remote.ID || s.s.Conn().RemotePeer().String() != s.remotePeerID() {
		return errors.New("kether: peer mismatch")
	}
	pub := s.remotePub
	nonceC := make([]byte, 32)
	if _, err := rand.Read(nonceC); err != nil {
		return err
	}
	sig, err := id.Sign(s.node.key, id.DomainHello, helloMaterial(env.Hello.AgentID, env.Hello.Nonce, nonceC))
	if err != nil {
		return err
	}
	if err := frame.Write(s.s, &frame.Envelope{Kind: frame.KindHello, Hello: frame.Hello{
		AgentID: s.node.agentID, Nonce: nonceC, Signature: sig,
	}}); err != nil {
		return err
	}
	ack, err := frame.Read(s.s)
	if err != nil {
		return err
	}
	if ack.Kind != frame.KindHelloAck {
		return errors.New("kether: expected hello ack")
	}
	if !id.Verify(pub, id.DomainHello, helloMaterial(s.node.agentID, env.Hello.Nonce, nonceC), ack.Ack.Signature) {
		return errors.New("kether: bad hello ack")
	}
	return nil
}

func helloMaterial(remote string, nonceS, nonceC []byte) []byte {
	buf := make([]byte, 2+len(remote)+len(nonceS)+len(nonceC))
	binary.BigEndian.PutUint16(buf[:2], uint16(len(remote)))
	copy(buf[2:], remote)
	copy(buf[2+len(remote):], nonceS)
	copy(buf[2+len(remote)+len(nonceS):], nonceC)
	return buf
}

func (s *Session) remotePeerID() string {
	pid, err := s.remotePub.PeerID()
	if err != nil {
		return ""
	}
	return pid.String()
}

func pubFromConn(s network.Stream) (*id.Public, error) {
	pk := s.Conn().RemotePublicKey()
	raw, err := pk.Raw()
	if err != nil {
		return nil, err
	}
	return id.ParseCompressed(raw)
}

func xonlyFromConn(s network.Stream) ([]byte, error) {
	pub, err := pubFromConn(s)
	if err != nil {
		return nil, err
	}
	return pub.XOnly(), nil
}

func (n *Node) serveInvoke(s network.Stream, callerPub []byte, callerID string, env *frame.Envelope) {
	if env.Kind != frame.KindInvoke {
		_ = n.fail(s, env.CorrID, frame.CodeFrame, "expected invoke")
		return
	}
	inv := env.Invoke
	rec := n.current()
	if rec == nil || !rec.HasType(inv.Type) {
		_ = n.fail(s, env.CorrID, frame.CodeNoType, "type")
		return
	}
	grant := Grant(nil)
	if rec.Access == record.AccessGrantOnly {
		g, err := auth.Unmarshal(inv.Grant)
		if err != nil || n.checker.Accept(g, callerPub, inv.Type, time.Now()) != nil {
			_ = n.fail(s, env.CorrID, frame.CodeUnauth, "unauthorized")
			return
		}
		grant = Grant(inv.Grant)
	}
	if len(inv.Nonce) != 16 {
		_ = n.fail(s, env.CorrID, frame.CodeFrame, "nonce")
		return
	}
	if err := n.rememberNonce(callerID, inv.Nonce, time.Unix(inv.SentAt, 0)); err != nil {
		code := frame.CodeVerify
		if errors.Is(err, errExpiredNonce) {
			code = frame.CodeExpired
		}
		_ = n.fail(s, env.CorrID, code, "nonce")
		return
	}
	var receipt []byte
	var rail string
	if len(rec.Pay) > 0 {
		method := rec.Pay[0]
		rail = method.Rail
		sum := pay.RequestHash(inv.Type, inv.Body, callerID, n.agentID, inv.Nonce)
		ch, err := n.cfg.Payee.Invoice(context.Background(), pay.InvoiceRequest{
			Rail: rail, Amount: method.Amount, RequestHash: sum, ExpiresAt: time.Now().Add(2 * time.Minute),
		})
		if err != nil {
			_ = n.fail(s, env.CorrID, frame.CodeBusiness, "invoice")
			return
		}
		wire := frame.Challenge{
			Rail: ch.Rail, Amount: ch.Amount, Invoice: ch.Invoice,
			RequestHash: ch.RequestHash, ExpiresAt: ch.ExpiresAt.Unix(),
		}
		sig, err := id.Sign(n.key, id.DomainChallenge, challengeMaterial(wire))
		if err != nil {
			_ = n.fail(s, env.CorrID, frame.CodeBusiness, "sign")
			return
		}
		wire.Signature = sig
		if err := frame.Write(s, &frame.Envelope{CorrID: env.CorrID, Kind: frame.KindChallenge, Challenge: wire}); err != nil {
			return
		}
		proof, err := frame.Read(s)
		if err != nil {
			return
		}
		if proof.Kind != frame.KindProof || proof.CorrID != env.CorrID {
			_ = n.fail(s, env.CorrID, frame.CodeFrame, "proof")
			return
		}
		if err := n.cfg.Payee.Verify(context.Background(), ch, proof.Proof.Receipt); err != nil {
			_ = n.fail(s, env.CorrID, frame.CodeBadPay, "payment")
			return
		}
		receipt = append([]byte(nil), proof.Proof.Receipt...)
	}
	n.mu.RLock()
	h := n.handler
	n.mu.RUnlock()
	if h == nil {
		_ = n.fail(s, env.CorrID, frame.CodeBusiness, "no handler")
		return
	}
	res, err := h(context.Background(), Call{
		Caller: callerID, Grant: grant, Receipt: receipt, Rail: rail, Type: inv.Type, Body: inv.Body,
	})
	if err != nil {
		_ = n.fail(s, env.CorrID, frame.CodeBusiness, "handler")
		return
	}
	_ = frame.Write(s, &frame.Envelope{CorrID: env.CorrID, Kind: frame.KindResult, Result: frame.Result{Body: res.Body}})
}

func (s *Session) Invoke(ctx context.Context, req Request) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if dl, ok := ctx.Deadline(); ok {
		_ = s.s.SetDeadline(dl)
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return Result{}, err
	}
	corr := s.next.Add(1)
	if err := frame.Write(s.s, &frame.Envelope{CorrID: corr, Kind: frame.KindInvoke, Invoke: frame.Invoke{
		Type: req.Type, Body: req.Body, Grant: []byte(req.Grant), Nonce: nonce, SentAt: time.Now().Unix(),
	}}); err != nil {
		return Result{}, err
	}
	for {
		env, err := frame.Read(s.s)
		if err != nil {
			return Result{}, err
		}
		if env.CorrID != corr && env.Kind != frame.KindError {
			continue
		}
		switch env.Kind {
		case frame.KindChallenge:
			if s.node.cfg.MaxPayMsat > 0 && env.Challenge.Amount > s.node.cfg.MaxPayMsat {
				return Result{}, &CallError{Code: frame.CodeBadPay, Message: "over payment cap"}
			}
			pub := s.remotePub
			wire := env.Challenge
			sig := wire.Signature
			wire.Signature = nil
			if !id.Verify(pub, id.DomainChallenge, challengeMaterial(wire), sig) {
				return Result{}, &CallError{Code: frame.CodeBadPay, Message: "challenge signature"}
			}
			if s.node.cfg.Payer == nil {
				return Result{}, &CallError{Code: frame.CodeNeedPay, Message: "no payer"}
			}
			ch := pay.Challenge{
				Rail: wire.Rail, Amount: wire.Amount, Invoice: wire.Invoice,
				RequestHash: wire.RequestHash, ExpiresAt: time.Unix(wire.ExpiresAt, 0), Signature: sig,
			}
			receipt, err := s.node.cfg.Payer.Pay(ctx, ch)
			if err != nil {
				return Result{}, err
			}
			if err := frame.Write(s.s, &frame.Envelope{CorrID: corr, Kind: frame.KindProof, Proof: frame.Proof{Rail: wire.Rail, Receipt: receipt}}); err != nil {
				return Result{}, err
			}
		case frame.KindResult:
			return Result{Body: env.Result.Body}, nil
		case frame.KindError:
			return Result{}, &CallError{Code: env.Error.Code, Message: env.Error.Message}
		default:
			return Result{}, &CallError{Code: frame.CodeFrame, Message: "unexpected frame"}
		}
	}
}

func challengeMaterial(c frame.Challenge) []byte {
	return []byte(fmt.Sprintf("%s|%d|%s|%s|%d", c.Rail, c.Amount, c.Invoice, hex.EncodeToString(c.RequestHash), c.ExpiresAt))
}

var errExpiredNonce = errors.New("expired nonce")
var errDupNonce = errors.New("duplicate nonce")

func (n *Node) rememberNonce(caller string, nonce []byte, sent time.Time) error {
	now := time.Now()
	if sent.Before(now.Add(-10*time.Minute)) || sent.After(now.Add(10*time.Minute)) {
		return errExpiredNonce
	}
	key := caller + ":" + hex.EncodeToString(nonce)
	n.mu.Lock()
	defer n.mu.Unlock()
	for k, exp := range n.nonces {
		if now.After(exp) {
			delete(n.nonces, k)
		}
	}
	if _, ok := n.nonces[key]; ok {
		return errDupNonce
	}
	n.nonces[key] = sent.Add(10 * time.Minute)
	return nil
}

func (n *Node) fail(w io.Writer, corr uint64, code uint32, msg string) error {
	return frame.Write(w, &frame.Envelope{CorrID: corr, Kind: frame.KindError, Error: frame.Error{Code: code, Message: msg}})
}
