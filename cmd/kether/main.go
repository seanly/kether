// Command kether is the public relay. It joins the private DHT, answers
// AutoNAT dial-back, and serves circuit relay reservations. Agents dial it
// with host:port.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/seanly/kether"
	"github.com/seanly/kether/id"
)

const typeRelay = "relay"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		if errors.Is(err, errUsage) {
			fmt.Fprintln(os.Stderr, usage)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

const usage = `kether [--key path] [--seed host:port]`

var errUsage = errors.New("usage")

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

type config struct {
	Key    string
	Listen []string
	Seeds  []string
}

func run(ctx context.Context, args []string, w io.Writer) error {
	fs := flag.NewFlagSet("kether", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var key string
	var seeds stringList
	fs.StringVar(&key, "key", "", "private key file (0600); created if missing")
	fs.Var(&seeds, "seed", "another relay host:port (repeatable)")
	if err := fs.Parse(args); err != nil {
		return errUsage
	}
	if fs.NArg() != 0 {
		return errUsage
	}
	node, err := start(ctx, config{Key: key, Seeds: seeds})
	if err != nil {
		return err
	}
	defer node.Close()
	if err := writeReady(w); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

func publicListen() []string {
	return []string{"/ip4/0.0.0.0/tcp/4001", "/ip4/0.0.0.0/udp/4001/quic-v1"}
}

func writeReady(w io.Writer) error {
	_, err := fmt.Fprintln(w, "listen 0.0.0.0:4001")
	return err
}

func start(ctx context.Context, cfg config) (*kether.Node, error) {
	key, err := loadKey(cfg.Key)
	if err != nil {
		return nil, err
	}
	listen := cfg.Listen
	if len(listen) == 0 {
		listen = publicListen()
	}
	return kether.Start(ctx, kether.Config{
		Key:          key,
		Listen:       listen,
		Seeds:        cfg.Seeds,
		Reachability: kether.ReachabilityPublic,
		Record: kether.Record{
			Name:   "relay",
			Types:  []string{typeRelay},
			Access: kether.AccessPublic,
		},
	})
}

func loadKey(path string) (*id.PrivateKey, error) {
	if path == "" {
		return id.Generate()
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		key, err := id.Generate()
		if err != nil {
			return nil, err
		}
		if err := key.Save(path); err != nil {
			return nil, err
		}
		return key, nil
	}
	return id.Load(path)
}
