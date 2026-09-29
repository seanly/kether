package kether

import (
	"context"

	"github.com/seanly/kether/node"
	"github.com/seanly/kether/pay"
)

type (
	Node      = node.Node
	Config    = node.Config
	Record    = node.Record
	PayMethod = node.PayMethod
	Access    = node.Access
	Peer      = node.Peer
	Scope     = node.Scope
	Grant     = node.Grant
	Session   = node.Session
	Request   = node.Request
	Call      = node.Call
	Result    = node.Result
	CallError = node.CallError
	Payee     = pay.Payee
	Payer     = pay.Payer
	Challenge = pay.Challenge
)

const (
	AccessPublic    = node.AccessPublic
	AccessGrantOnly = node.AccessGrantOnly
)

func Start(ctx context.Context, cfg Config) (*Node, error) {
	return node.Start(ctx, cfg)
}
