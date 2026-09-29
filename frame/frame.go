package frame

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/seanly/kether/internal/pbuf"
	"google.golang.org/protobuf/encoding/protowire"
)

const MaxFrame = 1 << 20

const (
	CodeVerify   uint32 = 1
	CodeUnauth   uint32 = 2
	CodeNeedPay  uint32 = 3
	CodeBadPay   uint32 = 4
	CodeNoType   uint32 = 5
	CodeExpired  uint32 = 6
	CodeBusiness uint32 = 7
	CodeFrame    uint32 = 8
)

type Kind int

const (
	KindNone Kind = iota
	KindHello
	KindHelloAck
	KindInvoke
	KindChallenge
	KindProof
	KindResult
	KindError
)

type Envelope struct {
	CorrID    uint64
	Kind      Kind
	Hello     Hello
	Ack       HelloAck
	Invoke    Invoke
	Challenge Challenge
	Proof     Proof
	Result    Result
	Error     Error
}

type Hello struct {
	AgentID   string
	Nonce     []byte
	Signature []byte
}

type HelloAck struct {
	Signature []byte
}

type Invoke struct {
	Type   string
	Body   []byte
	Grant  []byte
	Nonce  []byte
	SentAt int64
}

type Challenge struct {
	Rail        string
	Amount      uint64
	Invoice     string
	RequestHash []byte
	ExpiresAt   int64
	Signature   []byte
}

type Proof struct {
	Rail    string
	Receipt []byte
}

type Result struct {
	Body []byte
}

type Error struct {
	Code    uint32
	Message string
}

func Write(w io.Writer, env *Envelope) error {
	body, err := env.Marshal()
	if err != nil {
		return err
	}
	if len(body) > MaxFrame {
		return errors.New("frame: too large")
	}
	buf := make([]byte, 4+len(body))
	binary.BigEndian.PutUint32(buf[:4], uint32(len(body)))
	copy(buf[4:], body)
	_, err = w.Write(buf)
	return err
}

func Read(r io.Reader) (*Envelope, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > MaxFrame {
		return nil, errors.New("frame: too large")
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return Unmarshal(buf)
}

func (e *Envelope) Marshal() ([]byte, error) {
	var b []byte
	b = pbuf.AppendVarint(b, 1, e.CorrID)
	var body []byte
	var field protowire.Number
	switch e.Kind {
	case KindHello:
		field = 2
		body = e.Hello.marshal()
	case KindHelloAck:
		field = 3
		body = e.Ack.marshal()
	case KindInvoke:
		field = 4
		body = e.Invoke.marshal()
	case KindChallenge:
		field = 5
		body = e.Challenge.marshal()
	case KindProof:
		field = 6
		body = e.Proof.marshal()
	case KindResult:
		field = 7
		body = pbuf.AppendBytes(nil, 1, e.Result.Body)
	case KindError:
		field = 8
		body = e.Error.marshal()
	default:
		return nil, errors.New("frame: empty envelope")
	}
	b = pbuf.AppendBytes(b, field, body)
	return b, nil
}

type protowireNumber = interface{ ~int32 }

func (h Hello) marshal() []byte {
	var b []byte
	b = pbuf.AppendString(b, 1, h.AgentID)
	b = pbuf.AppendBytes(b, 2, h.Nonce)
	b = pbuf.AppendBytes(b, 3, h.Signature)
	return b
}

func (h HelloAck) marshal() []byte {
	return pbuf.AppendBytes(nil, 1, h.Signature)
}

func (m Invoke) marshal() []byte {
	var b []byte
	b = pbuf.AppendString(b, 1, m.Type)
	b = pbuf.AppendBytes(b, 2, m.Body)
	b = pbuf.AppendBytes(b, 3, m.Grant)
	b = pbuf.AppendBytes(b, 4, m.Nonce)
	b = pbuf.AppendVarint(b, 5, uint64(m.SentAt))
	return b
}

func (c Challenge) marshal() []byte {
	var b []byte
	b = pbuf.AppendString(b, 1, c.Rail)
	b = pbuf.AppendVarint(b, 2, c.Amount)
	b = pbuf.AppendString(b, 3, c.Invoice)
	b = pbuf.AppendBytes(b, 4, c.RequestHash)
	b = pbuf.AppendVarint(b, 5, uint64(c.ExpiresAt))
	b = pbuf.AppendBytes(b, 6, c.Signature)
	return b
}

func (p Proof) marshal() []byte {
	var b []byte
	b = pbuf.AppendString(b, 1, p.Rail)
	b = pbuf.AppendBytes(b, 2, p.Receipt)
	return b
}

func (e Error) marshal() []byte {
	var b []byte
	b = pbuf.AppendVarint(b, 1, uint64(e.Code))
	b = pbuf.AppendString(b, 2, e.Message)
	return b
}

func Unmarshal(b []byte) (*Envelope, error) {
	env := &Envelope{}
	for len(b) > 0 {
		f, rest, err := pbuf.Consume(b)
		if err != nil {
			return nil, err
		}
		b = rest
		switch f.Num {
		case 1:
			env.CorrID = f.Varint
		case 2:
			env.Kind = KindHello
			if err := env.Hello.unmarshal(f.Bytes); err != nil {
				return nil, err
			}
		case 3:
			env.Kind = KindHelloAck
			if err := env.Ack.unmarshal(f.Bytes); err != nil {
				return nil, err
			}
		case 4:
			env.Kind = KindInvoke
			if err := env.Invoke.unmarshal(f.Bytes); err != nil {
				return nil, err
			}
		case 5:
			env.Kind = KindChallenge
			if err := env.Challenge.unmarshal(f.Bytes); err != nil {
				return nil, err
			}
		case 6:
			env.Kind = KindProof
			if err := env.Proof.unmarshal(f.Bytes); err != nil {
				return nil, err
			}
		case 7:
			env.Kind = KindResult
			env.Result.Body = fieldBytes(f.Bytes, 1)
		case 8:
			env.Kind = KindError
			if err := env.Error.unmarshal(f.Bytes); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("frame: field %d", f.Num)
		}
	}
	if env.Kind == KindNone {
		return nil, errors.New("frame: empty envelope")
	}
	return env, nil
}

func (h *Hello) unmarshal(b []byte) error {
	for len(b) > 0 {
		f, rest, err := pbuf.Consume(b)
		if err != nil {
			return err
		}
		b = rest
		switch f.Num {
		case 1:
			h.AgentID = string(f.Bytes)
		case 2:
			h.Nonce = f.Bytes
		case 3:
			h.Signature = f.Bytes
		}
	}
	return nil
}

func (h *HelloAck) unmarshal(b []byte) error {
	for len(b) > 0 {
		f, rest, err := pbuf.Consume(b)
		if err != nil {
			return err
		}
		b = rest
		if f.Num == 1 {
			h.Signature = f.Bytes
		}
	}
	return nil
}

func (m *Invoke) unmarshal(b []byte) error {
	for len(b) > 0 {
		f, rest, err := pbuf.Consume(b)
		if err != nil {
			return err
		}
		b = rest
		switch f.Num {
		case 1:
			m.Type = string(f.Bytes)
		case 2:
			m.Body = f.Bytes
		case 3:
			m.Grant = f.Bytes
		case 4:
			m.Nonce = f.Bytes
		case 5:
			m.SentAt = int64(f.Varint)
		}
	}
	return nil
}

func (c *Challenge) unmarshal(b []byte) error {
	for len(b) > 0 {
		f, rest, err := pbuf.Consume(b)
		if err != nil {
			return err
		}
		b = rest
		switch f.Num {
		case 1:
			c.Rail = string(f.Bytes)
		case 2:
			c.Amount = f.Varint
		case 3:
			c.Invoice = string(f.Bytes)
		case 4:
			c.RequestHash = f.Bytes
		case 5:
			c.ExpiresAt = int64(f.Varint)
		case 6:
			c.Signature = f.Bytes
		}
	}
	return nil
}

func (p *Proof) unmarshal(b []byte) error {
	for len(b) > 0 {
		f, rest, err := pbuf.Consume(b)
		if err != nil {
			return err
		}
		b = rest
		switch f.Num {
		case 1:
			p.Rail = string(f.Bytes)
		case 2:
			p.Receipt = f.Bytes
		}
	}
	return nil
}

func (e *Error) unmarshal(b []byte) error {
	for len(b) > 0 {
		f, rest, err := pbuf.Consume(b)
		if err != nil {
			return err
		}
		b = rest
		switch f.Num {
		case 1:
			e.Code = uint32(f.Varint)
		case 2:
			e.Message = string(f.Bytes)
		}
	}
	return nil
}

func fieldBytes(b []byte, num protowire.Number) []byte {
	for len(b) > 0 {
		f, rest, err := pbuf.Consume(b)
		if err != nil {
			return nil
		}
		b = rest
		if f.Num == num && f.IsBytes {
			return f.Bytes
		}
	}
	return nil
}
