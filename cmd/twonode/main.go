package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/seanly/kether"
	"github.com/seanly/kether/id"
	"github.com/seanly/kether/pay"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := run(ctx, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context, w interface{ Write([]byte) (int, error) }) error {
	ln := pay.NewMemLightning()
	if err := publicPath(ctx, ln, w); err != nil {
		return err
	}
	if err := grantPath(ctx, ln, w); err != nil {
		return err
	}
	_, _ = w.Write([]byte("ok\n"))
	return nil
}

func publicPath(ctx context.Context, ln *pay.MemLightning, w interface{ Write([]byte) (int, error) }) error {
	aKey, err := id.Generate()
	if err != nil {
		return err
	}
	a, err := kether.Start(ctx, kether.Config{
		Key: aKey,
		Record: kether.Record{
			Name: "echo", Types: []string{"echo"}, Access: kether.AccessPublic,
			Pay: []kether.PayMethod{{Rail: "lightning", Amount: 1000}},
		},
		Payee: ln,
	})
	if err != nil {
		return err
	}
	defer a.Close()
	a.Handle(func(_ context.Context, c kether.Call) (kether.Result, error) {
		return kether.Result{Body: c.Body}, nil
	})
	bKey, err := id.Generate()
	if err != nil {
		return err
	}
	b, err := kether.Start(ctx, kether.Config{
		Key: bKey, Seeds: a.Addrs(),
		Record:     kether.Record{Types: []string{"client"}, Access: kether.AccessPublic},
		Payer:      ln,
		MaxPayMsat: 10_000,
	})
	if err != nil {
		return err
	}
	defer b.Close()
	var found kether.Peer
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		peers, err := b.Search(ctx, "echo")
		if err != nil {
			return err
		}
		for _, p := range peers {
			if p.ID == a.ID() {
				found = p
			}
		}
		if found.ID != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if found.ID == "" {
		return fmt.Errorf("search missed %s", a.ID())
	}
	sess, err := b.Connect(ctx, found.ID)
	if err != nil {
		return err
	}
	defer sess.Close()
	res, err := sess.Invoke(ctx, kether.Request{Type: "echo", Body: []byte("hi")})
	if err != nil {
		return err
	}
	if string(res.Body) != "hi" {
		return fmt.Errorf("public body %q", res.Body)
	}
	_, _ = fmt.Fprintf(w, "public %s\n", a.ID())
	return nil
}

func grantPath(ctx context.Context, ln *pay.MemLightning, w interface{ Write([]byte) (int, error) }) error {
	aKey, err := id.Generate()
	if err != nil {
		return err
	}
	a, err := kether.Start(ctx, kether.Config{
		Key: aKey,
		Record: kether.Record{
			Name: "echo", Types: []string{"echo"}, Access: kether.AccessGrantOnly,
			Pay: []kether.PayMethod{{Rail: "lightning", Amount: 1000}},
		},
		Payee: ln,
	})
	if err != nil {
		return err
	}
	defer a.Close()
	a.Handle(func(_ context.Context, c kether.Call) (kether.Result, error) {
		return kether.Result{Body: c.Body}, nil
	})
	bKey, err := id.Generate()
	if err != nil {
		return err
	}
	b, err := kether.Start(ctx, kether.Config{
		Key: bKey, Seeds: a.Addrs(),
		Record:     kether.Record{Types: []string{"client"}},
		Payer:      ln,
		MaxPayMsat: 10_000,
	})
	if err != nil {
		return err
	}
	defer b.Close()
	peers, err := b.Search(ctx, "echo")
	if err != nil {
		return err
	}
	for _, p := range peers {
		if p.ID == a.ID() {
			return fmt.Errorf("grant-only id %s was listed", a.ID())
		}
	}
	if _, err := b.Resolve(ctx, a.ID()); err != nil {
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			if _, err = b.Resolve(ctx, a.ID()); err == nil {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if err != nil {
			return err
		}
	}
	sess, err := b.Connect(ctx, a.ID())
	if err != nil {
		return err
	}
	defer sess.Close()
	_, err = sess.Invoke(ctx, kether.Request{Type: "echo", Body: []byte("no")})
	ce, ok := err.(*kether.CallError)
	if !ok || ce.Code != 2 {
		return fmt.Errorf("missing grant: %v", err)
	}
	g, err := a.Grant(b.ID(), kether.Scope{Types: []string{"echo"}, Until: time.Now().Add(time.Hour)})
	if err != nil {
		return err
	}
	res, err := sess.Invoke(ctx, kether.Request{Type: "echo", Body: []byte("yes"), Grant: g})
	if err != nil {
		return err
	}
	if string(res.Body) != "yes" {
		return fmt.Errorf("grant body %q", res.Body)
	}
	_, _ = fmt.Fprintf(w, "grant %s\n", a.ID())
	return nil
}
