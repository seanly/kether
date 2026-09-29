// Command kagent is a CLI agent. serve joins the kether network and answers
// calls with devkit, or echoes the body when --echo is set. ask dials a seed,
// searches for type "agent", and prints the reply.
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
	"time"

	"github.com/seanly/dmr-devkit/devkit"
	"github.com/seanly/kether"
	"github.com/seanly/kether/id"
)

const (
	typeAgent  = "agent"
	typeClient = "client"
	searchFor  = 15 * time.Second
	askFor     = 30 * time.Second
)

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

const usage = `kagent serve [--key path] [--listen addr] [--announce addr] [--seed addr] [--public] [--echo]
kagent ask --seed addr [--id agent] [--key path] <prompt>`

var errUsage = errors.New("usage")

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

type serveConfig struct {
	Key      string
	Listen   []string
	Announce []string
	Seeds    []string
	Public   bool
	Echo     bool
	OnAddrs  func([]string)
}

func run(ctx context.Context, args []string, w io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "serve":
		return runServe(ctx, args[1:], w)
	case "ask":
		return runAsk(ctx, args[1:], w)
	case "-h", "--help", "help":
		return errUsage
	default:
		return errUsage
	}
}

func runServe(ctx context.Context, args []string, w io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var key string
	var listen, announce, seeds stringList
	var echo, public bool
	fs.StringVar(&key, "key", "", "private key file (0600); created if missing")
	fs.Var(&listen, "listen", "libp2p listen multiaddr (repeatable)")
	fs.Var(&announce, "announce", "address published in the record (repeatable)")
	fs.Var(&seeds, "seed", "seed multiaddr (repeatable)")
	fs.BoolVar(&public, "public", false, "run as a public seed, AutoNAT dial-back, and relay")
	fs.BoolVar(&echo, "echo", false, "return the request body unchanged")
	if err := fs.Parse(args); err != nil {
		return errUsage
	}
	if fs.NArg() != 0 {
		return errUsage
	}
	srv, err := startServer(ctx, serveConfig{
		Key: key, Listen: listen, Announce: announce, Seeds: seeds,
		Public: public, Echo: echo,
		OnAddrs: func(addrs []string) {
			for _, addr := range addrs {
				fmt.Fprintf(w, "addr %s\n", addr)
			}
		},
	})
	if err != nil {
		return err
	}
	defer srv.Close()
	if err := writeReady(w, srv.Node.ID(), readyAddrs(srv.Node)); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

func readyAddrs(n *kether.Node) []string {
	if addrs := n.PublishedAddrs(); len(addrs) > 0 {
		return addrs
	}
	return n.Addrs()
}

func completeAnnounce(key *id.PrivateKey, addrs []string) ([]string, error) {
	pid, err := key.Public().PeerID()
	if err != nil {
		return nil, err
	}
	suffix := "/p2p/" + pid.String()
	out := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		if !strings.Contains(addr, "/p2p/") {
			addr += suffix
		}
		out = append(out, addr)
	}
	return out, nil
}

func writeReady(w io.Writer, agentID string, addrs []string) error {
	if _, err := fmt.Fprintf(w, "id %s\n", agentID); err != nil {
		return err
	}
	for _, a := range addrs {
		if _, err := fmt.Fprintf(w, "addr %s\n", a); err != nil {
			return err
		}
	}
	return nil
}

func runAsk(ctx context.Context, args []string, w io.Writer) error {
	fs := flag.NewFlagSet("ask", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var key, agentID string
	var seeds stringList
	fs.StringVar(&key, "key", "", "private key file (0600); created if missing")
	fs.StringVar(&agentID, "id", "", "agent id to call; searched by type when empty")
	fs.Var(&seeds, "seed", "seed multiaddr (repeatable)")
	if err := fs.Parse(args); err != nil {
		return errUsage
	}
	if len(seeds) == 0 || fs.NArg() == 0 {
		return errUsage
	}
	prompt := strings.Join(fs.Args(), " ")
	callCtx, cancel := context.WithTimeout(ctx, askFor)
	defer cancel()
	body, err := callAgent(callCtx, key, seeds, agentID, prompt)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(body))
	return err
}

type server struct {
	Node *kether.Node
	kit  *devkit.Kit
}

func (s *server) Close() {
	if s == nil {
		return
	}
	if s.Node != nil {
		_ = s.Node.Close()
	}
	if s.kit != nil {
		_ = s.kit.Close(context.Background())
	}
}

func startServer(ctx context.Context, cfg serveConfig) (*server, error) {
	key, err := loadKey(cfg.Key)
	if err != nil {
		return nil, err
	}
	srv := &server{}
	if !cfg.Echo {
		kit, err := buildKit(ctx)
		if err != nil {
			return nil, err
		}
		srv.kit = kit
	}
	announce, err := completeAnnounce(key, cfg.Announce)
	if err != nil {
		srv.Close()
		return nil, err
	}
	reach := kether.ReachabilityAuto
	if cfg.Public {
		reach = kether.ReachabilityPublic
	}
	node, err := kether.Start(ctx, kether.Config{
		Key:          key,
		Listen:       cfg.Listen,
		Seeds:        cfg.Seeds,
		Reachability: reach,
		OnAddrs:      cfg.OnAddrs,
		Record: kether.Record{
			Name:   "kagent",
			Types:  []string{typeAgent},
			Access: kether.AccessPublic,
			Addrs:  announce,
		},
	})
	if err != nil {
		srv.Close()
		return nil, err
	}
	srv.Node = node
	if cfg.Echo {
		node.Handle(func(_ context.Context, c kether.Call) (kether.Result, error) {
			return kether.Result{Body: append([]byte(nil), c.Body...)}, nil
		})
		return srv, nil
	}
	kit := srv.kit
	node.Handle(func(ctx context.Context, c kether.Call) (kether.Result, error) {
		res, err := kit.Agent.Run(ctx, devkit.DefaultTapeName, string(c.Body), 0)
		if err != nil {
			return kether.Result{}, err
		}
		return kether.Result{Body: []byte(res.Output)}, nil
	})
	return srv, nil
}

func buildKit(ctx context.Context) (*devkit.Kit, error) {
	opts := devkit.EnvOptions()
	if opts.APIKey == "" || opts.Model == "" {
		return nil, errors.New("AI_MODEL and AI_API_KEY are required")
	}
	opts.SystemPromptExtra = "You are a kether agent. A remote caller sent this prompt. Keep the answer short."
	return devkit.Build(ctx, opts)
}

func startClient(ctx context.Context, key *id.PrivateKey, seeds []string) (*kether.Node, error) {
	return kether.Start(ctx, kether.Config{
		Key:   key,
		Seeds: seeds,
		Record: kether.Record{
			Types:  []string{typeClient},
			Access: kether.AccessPublic,
		},
	})
}

func callAgent(ctx context.Context, keyPath string, seeds []string, agentID, prompt string) ([]byte, error) {
	key, err := loadKey(keyPath)
	if err != nil {
		return nil, err
	}
	node, err := startClient(ctx, key, seeds)
	if err != nil {
		return nil, err
	}
	defer node.Close()
	var peer kether.Peer
	if agentID != "" {
		peer, err = node.Resolve(ctx, agentID)
	} else {
		peer, err = findAgent(ctx, node)
	}
	if err != nil {
		return nil, err
	}
	sess, err := node.Connect(ctx, peer.ID)
	if err != nil {
		return nil, err
	}
	defer sess.Close()
	res, err := sess.Invoke(ctx, kether.Request{Type: typeAgent, Body: []byte(prompt)})
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

func findAgent(ctx context.Context, n *kether.Node) (kether.Peer, error) {
	deadline := time.Now().Add(searchFor)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	var last error
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return kether.Peer{}, err
		}
		peers, err := n.Search(ctx, typeAgent)
		if err != nil {
			last = err
		}
		for _, p := range peers {
			if p.ID != "" && p.ID != n.ID() {
				return p, nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if last != nil {
		return kether.Peer{}, last
	}
	return kether.Peer{}, errors.New("kagent: no agent found")
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
