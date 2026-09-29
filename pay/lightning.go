package pay

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
)

// MemLightning is an in-process Lightning network for tests and the local demo.
// Invoice strings do not contain the preimage.
type MemLightning struct {
	mu  sync.Mutex
	inv map[string]*lnInv
}

type lnInv struct {
	preimage []byte
	hash     string
	desc     string
	amount   uint64
	used     bool
}

func NewMemLightning() *MemLightning {
	return &MemLightning{inv: map[string]*lnInv{}}
}

func (m *MemLightning) Invoice(_ context.Context, req InvoiceRequest) (Challenge, error) {
	pre := make([]byte, 32)
	if _, err := rand.Read(pre); err != nil {
		return Challenge{}, err
	}
	sum := sha256.Sum256(pre)
	desc := hex.EncodeToString(req.RequestHash)
	invoice := fmt.Sprintf("lnbc%d-h-%s-d-%s", req.Amount, hex.EncodeToString(sum[:]), desc)
	m.mu.Lock()
	m.inv[invoice] = &lnInv{
		preimage: append([]byte(nil), pre...),
		hash:     hex.EncodeToString(sum[:]),
		desc:     desc,
		amount:   req.Amount,
	}
	m.mu.Unlock()
	return Challenge{
		Rail:        "lightning",
		Amount:      req.Amount,
		Invoice:     invoice,
		RequestHash: append([]byte(nil), req.RequestHash...),
		ExpiresAt:   req.ExpiresAt,
	}, nil
}

func (m *MemLightning) Pay(_ context.Context, ch Challenge) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec := m.inv[ch.Invoice]
	if rec == nil {
		return nil, errors.New("lightning: unknown invoice")
	}
	if rec.amount != ch.Amount {
		return nil, errors.New("lightning: amount")
	}
	return append([]byte(nil), rec.preimage...), nil
}

func (m *MemLightning) Verify(_ context.Context, ch Challenge, receipt []byte) error {
	sum := sha256.Sum256(receipt)
	m.mu.Lock()
	defer m.mu.Unlock()
	rec := m.inv[ch.Invoice]
	if rec == nil {
		return errors.New("lightning: unknown invoice")
	}
	if rec.used {
		return errors.New("lightning: invoice already used")
	}
	if hex.EncodeToString(sum[:]) != rec.hash {
		return errors.New("lightning: preimage")
	}
	if rec.desc != hex.EncodeToString(ch.RequestHash) {
		return errors.New("lightning: description hash")
	}
	if rec.amount != ch.Amount {
		return errors.New("lightning: amount")
	}
	rec.used = true
	return nil
}
