package collector

import (
	"context"
	"errors"
	"fmt"

	"github.com/certusone/wormhole/node/pkg/common"
	"github.com/certusone/wormhole/node/pkg/watchers/aztec/aztec-reorgs/aztec"
	"github.com/consensys/gnark-crypto/ecc/grumpkin/fp"
	"github.com/wormhole-foundation/wormhole/sdk/vaa"
)

type View struct {
	// Finalized is the most recent finalized block.
	Finalized aztec.Block
	// Blocks contains the unfinalized chain.
	Blocks []aztec.Block
}

func (v View) LatestBlock() aztec.Block {
	if len(v.Blocks) > 0 {
		return v.Blocks[len(v.Blocks)-1]
	}
	return v.Finalized
}

func (remote View) IsBehind(local View) bool {
	return remote.Finalized.Header.GlobalVariables.BlockNumber < local.Finalized.Header.GlobalVariables.BlockNumber
}

func (remote View) IsAhead(local View) bool {
	return remote.Finalized.Header.GlobalVariables.BlockNumber > remote.LatestBlock().Header.GlobalVariables.BlockNumber
}

type Collector struct {
	local           View
	c               Client
	contractAddress fp.Element
	chainID         vaa.ChainID
}

type Client interface {
	L2Tips(ctx context.Context) (*aztec.L2Tips, error)
	Blocks(ctx context.Context, start, limit uint64, proven bool) ([]aztec.Block, error)
}

// New creates a new collector. It will process all blocks after finalized.
func New(c Client, finalized aztec.Block, contractAddress vaa.Address) *Collector {
	return &Collector{
		local: View{
			Finalized: finalized,
			Blocks:    make([]aztec.Block, 0),
		},
		c:               c,
		contractAddress: contractAddress,
	}
}

type ReorgError struct {
	height uint64
}

func (r ReorgError) Error() string { // TODO can this not be a pointer method?
	return fmt.Sprintf("reorg detected at height %d", r.height)
}

type FinalizedReorgError struct {
	e ReorgError
}

func (r FinalizedReorgError) Error() string { // TODO can this not be a pointer method?
	return fmt.Sprintf("reorg detected at finalized height %d", r.e.height)
}

func (r FinalizedReorgError) Unwrap() error { // TODO can this not be a pointer method?
	return &r.e
}

// blocks assumes end.Number >= start.Number.
func (w *Collector) blocks(ctx context.Context, start, end aztec.Tip) ([]aztec.Block, error) {
	limit := end.Number - start.Number + 1
	blocks, err := w.c.Blocks(ctx, start.Number, limit, false)
	if err != nil {
		return nil, fmt.Errorf("get blocks: %v", err)
	} else if uint64(len(blocks)) != limit {
		return nil, ReorgError{
			height: end.Number,
		}
	} else if firstHash := blocks[0].Header.Hash(); !firstHash.Equal(&start.Hash) {
		return nil, ReorgError{
			height: start.Number,
		}
	} else if lastHash := blocks[len(blocks)-1].Header.Hash(); !lastHash.Equal(&end.Hash) {
		return nil, ReorgError{
			height: end.Number,
		}
	}
	return blocks, nil
}

func (c *Collector) remoteView(ctx context.Context) (*View, error) {
	l2Tips, err := c.c.L2Tips(ctx)
	if err != nil {
		return nil, fmt.Errorf("get l2 tips: %v", err)
	}
	remoteBlocks, err := c.blocks(ctx, l2Tips.Finalized, l2Tips.Latest)
	if err != nil {
		var e ReorgError
		if errors.Is(err, &e) && e.height == l2Tips.Finalized.Number {
			err = FinalizedReorgError{
				e: e,
			}
		}
		return nil, err
	}
	return &View{
		Finalized: remoteBlocks[0],
		Blocks:    remoteBlocks[1:],
	}, nil
}

func (c *Collector) Update(ctx context.Context) ([]common.MessagePublication, error) {
	remote, err := c.remoteView(ctx)
	if err != nil {
		return nil, err
	}

	// The algorithm for reconciling the local and remote views depends on their relative positions.
	// The remote view can be:
	//
	//   1. behind the local view: the remote finalized height is less than the local finalized height.
	//   2. ahead of the local view: the remote height is greater than the local latest height.
	//   3. within the local view: neither ahead nor behind.

	var msgs []common.MessagePublication
	if remote.IsBehind(c.local) {
		return nil, FinalizedReorgError{
			e: ReorgError{
				height: c.local.Finalized.Header.GlobalVariables.BlockNumber,
			},
		}
	} else if remote.IsAhead(c.local) {
		// Process all blocks in the range (local finalized head, remote finalized head].
		// We only process them as unfinalized blocks in order to "catch up", reducing the problem to case 3 above.
		latestLocalHeader := c.local.LatestBlock().Header
		blocks, err := c.blocks(ctx, latestLocalHeader.ToTip(), remote.Finalized.Header.ToTip())
		if err != nil {
			var e ReorgError
			if errors.Is(err, &e) {
				err = FinalizedReorgError{
					e: e,
				}
			}
			return nil, err
		}
		msgs = append(msgs, c.processUnfinalizedBlocks(blocks[1:])...)
	}

	// Process newly finalized blocks to bring w.local.Finalized up to remote.Finalized.

	remoteFinalizedBlockIndex := remote.Finalized.Header.GlobalVariables.BlockNumber - c.local.Finalized.Header.GlobalVariables.BlockNumber
	if !remote.Finalized.Equal(c.local.Blocks[remoteFinalizedBlockIndex]) {
		return nil, FinalizedReorgError{
			e: ReorgError{
				height: c.local.Finalized.Header.GlobalVariables.BlockNumber,
			},
		}
	}
	msgs = append(msgs, c.processBlocks(consistencyLevelFinalized, c.local.Blocks[:remoteFinalizedBlockIndex+1])...)
	c.local.Blocks = c.local.Blocks[remoteFinalizedBlockIndex+1:]
	c.local.Finalized = remote.Finalized

	// Now that the finalized heads match, process remote.Blocks.

	// We may have already processed some of remote.Blocks if they are contained in w.local.Blocks.
	// Find the unprocessed portion of remote.Blocks.
	// This is equivalent to finding the latest block in remote.Blocks that is not in w.local.Blocks.
	unprocessedIndex := 0
	for i, block := range remote.Blocks {
		if i == len(c.local.Blocks) {
			break
		} else if !block.Equal(c.local.Blocks[i]) {
			break
		}
		unprocessedIndex++
	}

	return append(msgs, c.processUnfinalizedBlocks(remote.Blocks[unprocessedIndex:])...), nil
}

func (c *Collector) processUnfinalizedBlocks(blocks []aztec.Block) []common.MessagePublication {
	c.local.Blocks = append(c.local.Blocks, blocks...)
	return c.processBlocks(consistencyLevelUnfinalized, blocks)
}

type consistencyLevel byte

const (
	consistencyLevelUnfinalized = 200
	consistencyLevelProven      // Unused right now.
	consistencyLevelFinalized
)

func (c *Collector) processBlocks(cLevel consistencyLevel, blocks []aztec.Block) []common.MessagePublication {
	var msgs []common.MessagePublication
	// Since most logs will not match the one we're interested in, this could be done more efficiently in parallel.
	// Because there will not be many TxEffects per block on testnet, we keep it sequential for now.
	for _, block := range blocks {
		blockTimestamp := block.Header.GlobalVariables.Timestamp
		for _, txEffect := range block.Body.TxEffects {
			txID := txEffect.TxHash.Bytes()
			for _, log := range txEffect.PublicLogs {
				logConsistencyLevel := uint8(log[3].Uint64())
				if consistencyLevel(logConsistencyLevel) != cLevel {
					continue
				}
				var payload []byte
				for _, payloadFp := range log[4:] {
					payloadFpBytes := payloadFp.Bytes()
					payload = append(payload, payloadFpBytes[:]...)
				}
				// Parsed based on the format in aztec/contracts/src/main.nr.
				msgs = append(msgs, common.MessagePublication{
					TxID:             txID[:],
					Timestamp:        blockTimestamp,
					Nonce:            uint32(log[2].Uint64()),
					Sequence:         log[1].Uint64(),
					ConsistencyLevel: logConsistencyLevel,
					EmitterChain:     c.chainID,
					EmitterAddress:   vaa.Address(c.contractAddress.Bytes()),
					Payload:          payload,
				})
			}
		}
	}
	return msgs
}
