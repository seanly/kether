package pay

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"hash"
	"time"
)

// Challenge is the server's demand for payment, bound to one invoke.
type Challenge struct {
	Rail        string
	Amount      uint64
	Invoice     string
	RequestHash []byte
	ExpiresAt   time.Time
	Signature   []byte
}

// InvoiceRequest is what the node asks a Payee to bill.
type InvoiceRequest struct {
	Rail        string
	Amount      uint64
	RequestHash []byte
	ExpiresAt   time.Time
}

// Payee creates and checks invoices for the local agent.
type Payee interface {
	Invoice(ctx context.Context, req InvoiceRequest) (Challenge, error)
	Verify(ctx context.Context, ch Challenge, receipt []byte) error
}

// Payer settles a challenge and returns the rail-specific receipt.
type Payer interface {
	Pay(ctx context.Context, ch Challenge) ([]byte, error)
}

// RequestHash binds an invoke to the two agent IDs. Fields are length-prefixed.
func RequestHash(typ string, body []byte, caller, callee string, nonce []byte) []byte {
	h := sha256.New()
	writeStr(h, typ)
	writeBytes(h, body)
	writeStr(h, caller)
	writeStr(h, callee)
	_, _ = h.Write(nonce)
	return h.Sum(nil)
}

func writeStr(h hash.Hash, s string) {
	var l [2]byte
	binary.BigEndian.PutUint16(l[:], uint16(len(s)))
	_, _ = h.Write(l[:])
	_, _ = h.Write([]byte(s))
}

func writeBytes(h hash.Hash, b []byte) {
	var l [4]byte
	binary.BigEndian.PutUint32(l[:], uint32(len(b)))
	_, _ = h.Write(l[:])
	_, _ = h.Write(b)
}
