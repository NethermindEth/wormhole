package client

import (
	"context"

	"github.com/certusone/wormhole/node/pkg/watchers/aztec/aztec-reorgs/adapters/rpc2aztec"
	"github.com/certusone/wormhole/node/pkg/watchers/aztec/aztec-reorgs/aztec"
	"github.com/certusone/wormhole/node/pkg/watchers/aztec/aztec-reorgs/rpc"
)

type API interface {
	L2Tips(context.Context) (*rpc.L2Tips, error)
	Blocks(context.Context, uint64, uint64, bool) ([]rpc.Block, error)
}

type Aztec struct {
	api API
}

func New(api API) *Aztec {
	return &Aztec{
		api: api,
	}
}

func (a *Aztec) L2Tips(ctx context.Context) (*aztec.L2Tips, error) {
	got, err := a.api.L2Tips(ctx)
	if err != nil {
		return nil, err
	}
	out := rpc2aztec.L2Tips(*got)
	return &out, nil
}

func (a *Aztec) Blocks(ctx context.Context, start, limit uint64, proven bool) ([]aztec.Block, error) {
	rpcBlocks, err := a.api.Blocks(ctx, start, limit, proven)
	if err != nil {
		return nil, err
	}
	blocks := make([]aztec.Block, 0, len(rpcBlocks))
	for _, rpcBlock := range rpcBlocks {
		blocks = append(blocks, rpc2aztec.Block(rpcBlock))
	}
	return blocks, nil
}
