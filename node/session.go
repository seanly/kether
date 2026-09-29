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

	"github.com/seanly/kether/auth"
	"github.com/seanly/kether/frame"
	"github.com/seanly/kether/id"
	"github.com/seanly/kether/pay"
	"github.com/seanly/kether/record"
)

type Session struct {
	node   *Node
	s      network.Stream
	remote Peer
	mu     sync.Mutex
	next   atomic.Uint64
}

type Request struct {
	Type  string
	Body  []byte
	Grant Grant
}

func (n *Node) Connect(ctx context.Context, agentID string) (*Session, error) {
	remote, err := n.Resolve(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if len(remote.Addrs) == 0 {
		return nil, errors.New("kether: record has no address")
	}
	var last error
	var ai *peer.AddrInfo
	for _, addr := range remote.Addrs {
		got, err := peer.AddrInfoFromString(addr)
		if err != nil {
			last = err
			continue
		}
		ai = got
		if err := n.host.Connect(ctx, *ai); err != nil {
			last = err
			ai = nil
			continue
		}
		break
	}
	if ai == nil {
		if last == nil {
			last = errors.New("no address")
		}
		return nil, last
	}
	s, err := n.host.NewStream(ctx, ai.ID, protoID)
	if err != nil {
		return nil, err
	}
	sess := &Session{node: n, s: s, remote: remote}
	if err := sess.handshakeClient(ctx); err != nil {
		s.Close()
		return nil, err
	}
	return sess, nil
}

func (s *Session) Close() error { return s.s.Close() }

func (n *Node) onStream(s network.Stream) {
	defer s.Close()
	_ = s.SetDeadline(time.Now().Add(30 * time.Second))
	if err := n.handshakeServer(s); err != nil {
		return
	}
	callerPub, callerID, err := peerIdentity(s)
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

func (n *Node) handshakeServer(s network.Stream) error {
	nonceS := make([]byte, 32)
	if _, err := rand.Read(nonceS); err != nil {
		return err
	}
	if err := frame.Write(s, &frame.Envelope{Kind: frame.KindHello, Hello: frame.Hello{AgentID: n.agentID, Nonce: nonceS}}); err != nil {
		return err
	}
	env, err := frame.Read(s)
	if err != nil {
		return err
	}
	if env.Kind != frame.KindHello {
		return n.fail(s, 0, frame.CodeFrame, "expected hello")
	}
	pub, pid, err := identityOf(env.Hello.AgentID)
	if err != nil || pid != s.Conn().RemotePeer() {
		return n.fail(s, 0, frame.CodeVerify, "peer mismatch")
	}
	nonceC := env.Hello.Nonce
	msg := helloMaterial(n.agentID, nonceS, nonceC)
	if !id.Verify(pub, id.DomainHello, msg, env.Hello.Signature) {
		return n.fail(s, 0, frame.CodeVerify, "bad hello signature")
	}
	sig, err := id.Sign(n.key, id.DomainHello, helloMaterial(env.Hello.AgentID, nonceS, nonceC))
	if err != nil {
		return err
	}
	return frame.Write(s, &frame.Envelope{Kind: frame.KindHelloAck, Ack: frame.HelloAck{Signature: sig}})
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
	pub, pid, err := identityOf(env.Hello.AgentID)
	if err != nil {
		return err
	}
	if pid != s.s.Conn().RemotePeer() || env.Hello.AgentID != s.remote.ID {
		return errors.New("kether: peer mismatch")
	}
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

func identityOf(agentID string) (*id.Public, peer.ID, error) {
	pub, err := id.ParseAgentID(agentID)
	if err != nil {
		return nil, "", err
	}
	pid, err := pub.PeerID()
	if err != nil {
		return nil, "", err
	}
	return pub, pid, nil
}

func peerIdentity(s network.Stream) ([]byte, string, error) {
	// The remote peer id was checked during handshake. Recover the x-only key
	// from the libp2p public key on the connection.
	pk := s.Conn().RemotePublicKey()
	raw, err := pk.Raw()
	if err != nil {
		return nil, "", err
	}
	pub, err := id.ParseCompressed(raw)
	if err != nil {
		return nil, "", err
	}
	aid, err := pub.AgentID()
	if err != nil {
		return nil, "", err
	}
	return pub.XOnly(), aid, nil
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
			pub, err := id.ParseAgentID(s.remote.ID)
			if err != nil {
				return Result{}, err
			}
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
