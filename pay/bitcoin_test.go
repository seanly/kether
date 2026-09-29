package pay_test

import (
	"context"
	"testing"
	"time"

	"github.com/seanly/kether/pay"
)

func TestBitcoinConfirmations(t *testing.T) {
	ctx := context.Background()
	chain := pay.NewMemChain(1)
	ch, err := chain.Invoice(ctx, pay.InvoiceRequest{
		Rail: "bitcoin", Amount: 50_000, RequestHash: []byte("hash"), ExpiresAt: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	chain.Fund(ch.Invoice, ch.Amount, 0)
	if err := chain.Verify(ctx, ch, []byte("tx")); err == nil {
		t.Fatal("expected unconfirmed")
	}
	chain.Fund(ch.Invoice, ch.Amount-1, 1)
	if err := chain.Verify(ctx, ch, []byte("tx")); err == nil {
		t.Fatal("expected low amount")
	}
	chain.Fund(ch.Invoice, ch.Amount, 1)
	if err := chain.Verify(ctx, ch, []byte("tx")); err != nil {
		t.Fatal(err)
	}
	if err := chain.Verify(ctx, ch, []byte("tx")); err == nil {
		t.Fatal("expected address reuse to fail")
	}
}
