package pay

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
)

// MemChain is an in-memory UTXO set. Tests and CI use it instead of a public chain.
type MemChain struct {
	mu       sync.Mutex
	required int
	utxo     map[string]*coin
}

type coin struct {
	sats  uint64
	confs int
	used  bool
	want  uint64
}

func NewMemChain(requiredConfs int) *MemChain {
	if requiredConfs <= 0 {
		requiredConfs = 1
	}
	return &MemChain{required: requiredConfs, utxo: map[string]*coin{}}
}

func (c *MemChain) Invoice(_ context.Context, req InvoiceRequest) (Challenge, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return Challenge{}, err
	}
	sum := sha256.Sum256(append(append([]byte{}, req.RequestHash...), entropy[:]...))
	addr := "kethbc1" + hex.EncodeToString(sum[:16])
	c.mu.Lock()
	c.utxo[addr] = &coin{want: req.Amount}
	c.mu.Unlock()
	return Challenge{
		Rail:        "bitcoin",
		Amount:      req.Amount,
		Invoice:     addr,
		RequestHash: append([]byte(nil), req.RequestHash...),
		ExpiresAt:   req.ExpiresAt,
	}, nil
}

// Fund records a payment. confs is the confirmation count visible to Verify.
func (c *MemChain) Fund(addr string, sats uint64, confs int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	rec := c.utxo[addr]
	if rec == nil {
		rec = &coin{}
		c.utxo[addr] = rec
	}
	rec.sats = sats
	rec.confs = confs
}

func (c *MemChain) Pay(_ context.Context, ch Challenge) ([]byte, error) {
	c.Fund(ch.Invoice, ch.Amount, c.required)
	sum := sha256.Sum256([]byte(ch.Invoice))
	return sum[:], nil
}

func (c *MemChain) Verify(_ context.Context, ch Challenge, _ []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	rec := c.utxo[ch.Invoice]
	if rec == nil {
		return errors.New("bitcoin: unknown address")
	}
	if rec.used {
		return errors.New("bitcoin: address already used")
	}
	if rec.confs < c.required {
		return errors.New("bitcoin: unconfirmed")
	}
	if rec.sats < ch.Amount {
		return errors.New("bitcoin: amount")
	}
	rec.used = true
	return nil
}
