package watcher

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/certusone/wormhole/node/pkg/common"
	"github.com/certusone/wormhole/node/pkg/readiness"
	"github.com/certusone/wormhole/node/pkg/watchers/aztec/aztec-reorgs/aztec"
	"github.com/certusone/wormhole/node/pkg/watchers/aztec/aztec-reorgs/aztec/client"
	"github.com/certusone/wormhole/node/pkg/watchers/aztec/aztec-reorgs/collector"
	aztecrpcclient "github.com/certusone/wormhole/node/pkg/watchers/aztec/aztec-reorgs/rpc/client"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/wormhole-foundation/wormhole/sdk/vaa"
)

type Watcher struct {
	rpcURL    string
	finalized aztec.Block
	blockTime time.Duration
	msgChan   chan *common.MessagePublication
	readiness.Component
}

func NewWatcher(
	chainID vaa.ChainID, // May be hard coded instead of passed in.
	rpcURL string,
	finalized aztec.Block,
	blockTime time.Duration,
	msgChan chan *common.MessagePublication,
) *Watcher {
	return &Watcher{
		rpcURL:    rpcURL,
		finalized: finalized,
		msgChan:   msgChan,
		blockTime: blockTime,
		Component: common.MustConvertChainIdToReadinessSyncing(chainID),
	}
}

func (w *Watcher) Run(ctx context.Context) error {
	rpcClient, err := rpc.DialContext(ctx, w.rpcURL) // TODO we need DialOptions
	if err != nil {
		return fmt.Errorf("dial context: %v", err)
	}
	c := collector.New(client.New(aztecrpcclient.New(rpcClient)), w.finalized)

	ticker := time.NewTicker(w.blockTime)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			_, err := c.Update(ctx)
			if err != nil {
				var e collector.FinalizedReorgError
				if errors.Is(err, &e) {
					return fmt.Errorf("collector: %v", &e)
				}
				continue // TODO: log
			}
			// TODO handle update
		}
	}
}
