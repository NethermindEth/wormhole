package client

import (
	"context"

	"github.com/certusone/wormhole/node/pkg/watchers/aztec/aztec-reorgs/rpc"
)

type RPCClient interface {
	CallContext(ctx context.Context, out any, method string, params ...any) error
}

type Aztec struct {
	rpcClient RPCClient
}

func New(rpcClient RPCClient) *Aztec {
	return &Aztec{
		rpcClient: rpcClient,
	}
}

func (a *Aztec) L2Tips(ctx context.Context) (*rpc.L2Tips, error) {
	var out rpc.L2Tips
	if err := a.rpcClient.CallContext(ctx, &out, "node_getL2Tips"); err != nil {
		return nil, err
	}
	return &out, nil
}

func (a *Aztec) Blocks(ctx context.Context, start, limit uint64, proven bool) ([]rpc.Block, error) {
	var out []rpc.Block
	if err := a.rpcClient.CallContext(ctx, &out, "node_getBlocks", start, limit, proven); err != nil {
		return nil, err
	}
	return out, nil
}
