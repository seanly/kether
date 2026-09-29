package auth

import (
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/seanly/kether/id"
	"github.com/seanly/kether/internal/pbuf"
)

const skew = 120 * time.Second

var (
	ErrUnauthorized = errors.New("auth: unauthorized")
	ErrExpired      = errors.New("auth: expired")
	ErrCalls        = errors.New("auth: call limit")
)

type Grant struct {
	Version   uint32
	Grantor   []byte
	Grantee   []byte
	Types     []string
	NotBefore int64
	NotAfter  int64
	MaxCalls  uint64
	MaxAmount uint64
	Seq       uint64
	Signature []byte
}

func Sign(priv *id.PrivateKey, g *Grant) error {
	g.Version = 1
	g.Grantor = priv.Public().XOnly()
	if g.Seq == 0 {
		g.Seq = 1
	}
	sig, err := id.Sign(priv, id.DomainGrant, g.marshalBody())
	if err != nil {
		return err
	}
	g.Signature = sig
	return nil
}

func (g *Grant) Verify() error {
	if g.Version != 1 || len(g.Grantor) != 32 || len(g.Grantee) != 32 {
		return ErrUnauthorized
	}
	pub, err := id.ParseXOnly(g.Grantor)
	if err != nil {
		return ErrUnauthorized
	}
	if !id.Verify(pub, id.DomainGrant, g.marshalBody(), g.Signature) {
		return ErrUnauthorized
	}
	return nil
}

func (g *Grant) marshalBody() []byte {
	var b []byte
	b = pbuf.AppendVarint(b, 1, uint64(g.Version))
	b = pbuf.AppendBytes(b, 2, g.Grantor)
	b = pbuf.AppendBytes(b, 3, g.Grantee)
	for _, typ := range g.Types {
		b = pbuf.AppendString(b, 4, typ)
	}
	b = pbuf.AppendVarint(b, 5, uint64(g.NotBefore))
	b = pbuf.AppendVarint(b, 6, uint64(g.NotAfter))
	b = pbuf.AppendVarint(b, 7, g.MaxCalls)
	b = pbuf.AppendVarint(b, 8, g.MaxAmount)
	b = pbuf.AppendVarint(b, 9, g.Seq)
	return b
}

func (g *Grant) Marshal() []byte {
	b := g.marshalBody()
	if len(g.Signature) > 0 {
		b = pbuf.AppendBytes(b, 10, g.Signature)
	}
	return b
}

func Unmarshal(b []byte) (*Grant, error) {
	g := &Grant{}
	for len(b) > 0 {
		f, rest, err := pbuf.Consume(b)
		if err != nil {
			return nil, err
		}
		b = rest
		switch f.Num {
		case 1:
			g.Version = uint32(f.Varint)
		case 2:
			g.Grantor = f.Bytes
		case 3:
			g.Grantee = f.Bytes
		case 4:
			g.Types = append(g.Types, string(f.Bytes))
		case 5:
			g.NotBefore = int64(f.Varint)
		case 6:
			g.NotAfter = int64(f.Varint)
		case 7:
			g.MaxCalls = f.Varint
		case 8:
			g.MaxAmount = f.Varint
		case 9:
			g.Seq = f.Varint
		case 10:
			g.Signature = f.Bytes
		default:
			return nil, errors.New("auth: unknown field")
		}
	}
	return g, nil
}

func (g *Grant) Allows(caller []byte, typ string, now time.Time) error {
	if err := g.Verify(); err != nil {
		return err
	}
	if !bytesEqual(g.Grantee, caller) {
		return ErrUnauthorized
	}
	ok := false
	for _, t := range g.Types {
		if t == typ {
			ok = true
			break
		}
	}
	if !ok {
		return ErrUnauthorized
	}
	start := time.Unix(g.NotBefore, 0).Add(-skew)
	end := time.Unix(g.NotAfter, 0).Add(skew)
	if now.Before(start) || now.After(end) {
		return ErrExpired
	}
	return nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Checker enforces MaxCalls in memory. A new checker starts the count over.
type Checker struct {
	mu   sync.Mutex
	left map[string]uint64
}

func (c *Checker) Accept(g *Grant, caller []byte, typ string, now time.Time) error {
	if err := g.Allows(caller, typ, now); err != nil {
		return err
	}
	if g.MaxCalls == 0 {
		return nil
	}
	key := hex.EncodeToString(g.Signature)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.left == nil {
		c.left = map[string]uint64{}
	}
	left, ok := c.left[key]
	if !ok {
		left = g.MaxCalls
	}
	if left == 0 {
		return ErrCalls
	}
	c.left[key] = left - 1
	return nil
}
