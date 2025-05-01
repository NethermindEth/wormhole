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
	"github.com/consensys/gnark-crypto/ecc/grumpkin/fp"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/wormhole-foundation/wormhole/sdk/vaa"
	"go.uber.org/zap"
)

type Watcher struct {
	rpcURL          string
	finalized       aztec.Block
	blockTime       time.Duration
	chainID         vaa.ChainID
	contractAddress fp.Element
	msgChan         chan *common.MessagePublication
	log             *zap.Logger
	readiness.Component
}

func NewWatcher(
	chainID vaa.ChainID,
	rpcURL string,
	finalized aztec.Block,
	blockTime time.Duration,
	msgChan chan *common.MessagePublication,
	log *zap.Logger,
) *Watcher {
	return &Watcher{
		rpcURL:    rpcURL,
		finalized: finalized,
		msgChan:   msgChan,
		blockTime: blockTime,
		Component: common.MustConvertChainIdToReadinessSyncing(chainID),
		log:       log,
	}
}

func (w *Watcher) Run(ctx context.Context) error {
	rpcClient, err := rpc.DialContext(ctx, w.rpcURL) // TODO we need DialOptions for retries. Need to upgrade go-ethereum.
	if err != nil {
		return fmt.Errorf("dial context: %v", err)
	}
	c := collector.New(client.New(aztecrpcclient.New(rpcClient)), w.finalized, w.contractAddress, w.chainID)
	// TODO cross-check chain id?

	ticker := time.NewTicker(w.blockTime)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			msgs, err := c.Update(ctx)
			if err != nil {
				var e collector.FinalizedReorgError
				if errors.Is(err, &e) {
					return fmt.Errorf("collector: %v", &e)
				}
				w.log.Warn("Failed to collect messages, retrying...", zap.Error(err))
				continue
			}
			for _, msg := range msgs {
				w.msgChan <- &msg
			}
		}
	}
}
