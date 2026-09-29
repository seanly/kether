// Package pbuf writes a small subset of protobuf wire format in field-number order.
package pbuf

import (
	"fmt"

	"google.golang.org/protobuf/encoding/protowire"
)

func AppendVarint(b []byte, field protowire.Number, v uint64) []byte {
	b = protowire.AppendTag(b, field, protowire.VarintType)
	return protowire.AppendVarint(b, v)
}

func AppendBytes(b []byte, field protowire.Number, v []byte) []byte {
	b = protowire.AppendTag(b, field, protowire.BytesType)
	return protowire.AppendBytes(b, v)
}

func AppendString(b []byte, field protowire.Number, s string) []byte {
	return AppendBytes(b, field, []byte(s))
}

// Field is one protobuf tag consumed from a buffer.
type Field struct {
	Num     protowire.Number
	Varint  uint64
	Bytes   []byte
	IsBytes bool
}

func Consume(b []byte) (Field, []byte, error) {
	num, typ, n := protowire.ConsumeTag(b)
	if n < 0 {
		return Field{}, nil, fmt.Errorf("pbuf: tag: %w", protowire.ParseError(n))
	}
	b = b[n:]
	switch typ {
	case protowire.VarintType:
		v, n := protowire.ConsumeVarint(b)
		if n < 0 {
			return Field{}, nil, fmt.Errorf("pbuf: varint: %w", protowire.ParseError(n))
		}
		return Field{Num: num, Varint: v}, b[n:], nil
	case protowire.BytesType:
		v, n := protowire.ConsumeBytes(b)
		if n < 0 {
			return Field{}, nil, fmt.Errorf("pbuf: bytes: %w", protowire.ParseError(n))
		}
		out := append([]byte(nil), v...)
		return Field{Num: num, Bytes: out, IsBytes: true}, b[n:], nil
	default:
		return Field{}, nil, fmt.Errorf("pbuf: unsupported wire type %d", typ)
	}
}
