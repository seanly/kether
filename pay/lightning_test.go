package pay_test

import (
	"context"
	"testing"
	"time"

	"github.com/seanly/kether/pay"
)

func TestLightningPreimage(t *testing.T) {
	ln := pay.NewMemLightning()
	ctx := context.Background()
	hash := []byte("request-hash-32-bytes-padding!!")
	ch, err := ln.Invoice(ctx, pay.InvoiceRequest{
		Rail: "lightning", Amount: 1000, RequestHash: hash, ExpiresAt: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := ln.Pay(ctx, ch)
	if err != nil {
		t.Fatal(err)
	}
	if err := ln.Verify(ctx, ch, receipt); err != nil {
		t.Fatal(err)
	}
	if err := ln.Verify(ctx, ch, receipt); err == nil {
		t.Fatal("expected second verify to fail")
	}
	ch2, err := ln.Invoice(ctx, pay.InvoiceRequest{
		Rail: "lightning", Amount: 1000, RequestHash: hash, ExpiresAt: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	bad := append([]byte(nil), receipt...)
	bad[0] ^= 0xff
	if err := ln.Verify(ctx, ch2, bad); err == nil {
		t.Fatal("expected bad preimage")
	}
	pre, err := ln.Pay(ctx, ch2)
	if err != nil {
		t.Fatal(err)
	}
	ch2.RequestHash = []byte("other")
	if err := ln.Verify(ctx, ch2, pre); err == nil {
		t.Fatal("expected description hash mismatch")
	}
}
