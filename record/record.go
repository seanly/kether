package record

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/seanly/kether/id"
	"github.com/seanly/kether/internal/pbuf"
)

const (
	AccessPublic    uint32 = 0
	AccessGrantOnly uint32 = 1
	MaxBytes               = 8 << 10
	skew                   = 120 * time.Second
)

var typeName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

type PayMethod struct {
	Rail   string
	Hint   string
	Amount uint64
}

const Version = 2

type Record struct {
	Version   uint32
	Pubkey    []byte
	PeerID    string
	UUID      string
	Seq       uint64
	ExpiresAt int64
	Name      string
	Types     []string
	Access    uint32
	Pay       []PayMethod
	Signature []byte
}

func (r *Record) AgentID() (string, error) {
	if _, err := id.ParseUUID(r.UUID); err != nil {
		return "", err
	}
	return r.UUID, nil
}

func (r *Record) ValidateMeta() error {
	if r.Version != Version {
		return errors.New("record: version")
	}
	if _, err := id.ParseUUID(r.UUID); err != nil {
		return errors.New("record: uuid")
	}
	if len(r.Name) > 64 {
		return errors.New("record: name too long")
	}
	if len(r.Types) == 0 || len(r.Types) > 8 {
		return errors.New("record: types")
	}
	seen := map[string]struct{}{}
	for _, typ := range r.Types {
		if !typeName.MatchString(typ) {
			return fmt.Errorf("record: bad type %q", typ)
		}
		if _, ok := seen[typ]; ok {
			return fmt.Errorf("record: duplicate type %q", typ)
		}
		seen[typ] = struct{}{}
	}
	if len(r.Pay) > 4 {
		return errors.New("record: pay")
	}
	return nil
}

func Sign(priv *id.PrivateKey, r *Record) error {
	r.Version = Version
	r.UUID = priv.UUID()
	r.Pubkey = priv.Public().XOnly()
	pid, err := priv.Public().PeerID()
	if err != nil {
		return err
	}
	r.PeerID = pid.String()
	if err := r.ValidateMeta(); err != nil {
		return err
	}
	raw := r.marshalBody()
	sig, err := id.Sign(priv, id.DomainRecord, raw)
	if err != nil {
		return err
	}
	r.Signature = sig
	return nil
}

func (r *Record) Verify(now time.Time) error {
	if r.Version != Version {
		return errors.New("record: version")
	}
	if err := r.ValidateMeta(); err != nil {
		return err
	}
	pub, err := id.ParseXOnly(r.Pubkey)
	if err != nil {
		return err
	}
	pid, err := pub.PeerID()
	if err != nil {
		return err
	}
	if pid.String() != r.PeerID {
		return errors.New("record: peer id mismatch")
	}
	if !now.Before(time.Unix(r.ExpiresAt, 0).Add(skew)) {
		return errors.New("record: expired")
	}
	if !id.Verify(pub, id.DomainRecord, r.marshalBody(), r.Signature) {
		return errors.New("record: bad signature")
	}
	return nil
}

// Prefer keeps the higher sequence. Equal sequences keep the one already held.
func Prefer(current, next *Record) *Record {
	if current == nil || next != nil && next.Seq > current.Seq {
		return next
	}
	return current
}

func (r *Record) Marshal() ([]byte, error) {
	b := r.marshalBody()
	if len(r.Signature) > 0 {
		b = pbuf.AppendBytes(b, 11, r.Signature)
	}
	if len(b) > MaxBytes {
		return nil, errors.New("record: larger than 8KiB")
	}
	return b, nil
}

func (r *Record) marshalBody() []byte {
	var b []byte
	b = pbuf.AppendVarint(b, 1, uint64(r.Version))
	b = pbuf.AppendBytes(b, 2, r.Pubkey)
	b = pbuf.AppendString(b, 3, r.PeerID)
	b = pbuf.AppendVarint(b, 4, r.Seq)
	b = pbuf.AppendVarint(b, 5, uint64(r.ExpiresAt))
	b = pbuf.AppendString(b, 6, r.Name)
	for _, typ := range r.Types {
		b = pbuf.AppendString(b, 7, typ)
	}
	b = pbuf.AppendVarint(b, 8, uint64(r.Access))
	for _, pay := range r.Pay {
		b = pbuf.AppendBytes(b, 10, pay.marshal())
	}
	b = pbuf.AppendString(b, 12, r.UUID)
	return b
}

func (p PayMethod) marshal() []byte {
	var b []byte
	b = pbuf.AppendString(b, 1, p.Rail)
	b = pbuf.AppendString(b, 2, p.Hint)
	b = pbuf.AppendVarint(b, 3, p.Amount)
	return b
}

func Unmarshal(b []byte) (*Record, error) {
	if len(b) > MaxBytes {
		return nil, errors.New("record: larger than 8KiB")
	}
	r := &Record{}
	for len(b) > 0 {
		f, rest, err := pbuf.Consume(b)
		if err != nil {
			return nil, err
		}
		b = rest
		switch f.Num {
		case 1:
			r.Version = uint32(f.Varint)
		case 2:
			r.Pubkey = f.Bytes
		case 3:
			r.PeerID = string(f.Bytes)
		case 4:
			r.Seq = f.Varint
		case 5:
			r.ExpiresAt = int64(f.Varint)
		case 6:
			r.Name = string(f.Bytes)
		case 7:
			r.Types = append(r.Types, string(f.Bytes))
		case 8:
			r.Access = uint32(f.Varint)
		case 10:
			pm, err := unmarshalPay(f.Bytes)
			if err != nil {
				return nil, err
			}
			r.Pay = append(r.Pay, pm)
		case 11:
			r.Signature = f.Bytes
		case 12:
			r.UUID = string(f.Bytes)
		default:
			return nil, fmt.Errorf("record: unknown field %d", f.Num)
		}
	}
	return r, nil
}

func unmarshalPay(b []byte) (PayMethod, error) {
	var p PayMethod
	for len(b) > 0 {
		f, rest, err := pbuf.Consume(b)
		if err != nil {
			return PayMethod{}, err
		}
		b = rest
		switch f.Num {
		case 1:
			p.Rail = string(f.Bytes)
		case 2:
			p.Hint = string(f.Bytes)
		case 3:
			p.Amount = f.Varint
		default:
			return PayMethod{}, fmt.Errorf("record: pay field %d", f.Num)
		}
	}
	return p, nil
}

// HasType reports whether the record advertises typ.
func (r *Record) HasType(typ string) bool {
	for _, t := range r.Types {
		if t == typ {
			return true
		}
	}
	return false
}
