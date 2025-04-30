package collector

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/certusone/wormhole/node/pkg/watchers/aztec/aztec-reorgs/aztec"
	"github.com/consensys/gnark-crypto/ecc/grumpkin/fp"
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
	local View
	c     Client
}

type Client interface {
	L2Tips(ctx context.Context) (*aztec.L2Tips, error)
	Blocks(ctx context.Context, start, limit uint64, proven bool) ([]aztec.Block, error)
}

// New creates a new collector. It will process all blocks after finalized.
func New(c Client, finalized aztec.Block) *Collector {
	return &Collector{
		local: View{
			Finalized: finalized,
			Blocks:    make([]aztec.Block, 0),
		},
		c: c,
	}
}

type Message struct {
	TxHash    fp.Element
	BlockTime time.Time
	Log       aztec.PublicLog
}

type Update struct {
	Finalized   []Message
	Unfinalized []Message
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

// assumes end.Number >= start.Number
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

func (w *Collector) Update(ctx context.Context) (*Update, error) {
	remote, err := w.remoteView(ctx)
	if err != nil {
		return nil, err
	}

	// The algorithm for reconciling the local and remote views depends on their relative positions.
	// The remote view can be:
	//
	//   1. behind the local view: the remote finalized height is less than the local finalized height.
	//   2. ahead of the local view: the remote height is greater than the local latest height.
	//   3. within the local view: neither ahead nor behind.

	var unfinalized []Message
	if remote.IsBehind(w.local) {
		return nil, FinalizedReorgError{
			e: ReorgError{
				height: w.local.Finalized.Header.GlobalVariables.BlockNumber,
			},
		}
	} else if remote.IsAhead(w.local) {
		// Process all blocks in the range (local finalized head, remote finalized head].
		// We only process them as unfinalized blocks in order to "catch up", reducing the problem to case 3 above.
		latestLocalHeader := w.local.LatestBlock().Header
		blocks, err := w.blocks(ctx, latestLocalHeader.ToTip(), remote.Finalized.Header.ToTip())
		if err != nil {
			var e ReorgError
			if errors.Is(err, &e) {
				err = FinalizedReorgError{
					e: e,
				}
			}
			return nil, err
		}
		unfinalized = append(unfinalized, w.processUnfinalizedBlocks(blocks[1:])...)
	}

	// Process newly finalized blocks to bring w.local.Finalized up to remote.Finalized.

	remoteFinalizedBlockIndex := remote.Finalized.Header.GlobalVariables.BlockNumber - w.local.Finalized.Header.GlobalVariables.BlockNumber
	if !remote.Finalized.Equal(w.local.Blocks[remoteFinalizedBlockIndex]) {
		return nil, FinalizedReorgError{
			e: ReorgError{
				height: w.local.Finalized.Header.GlobalVariables.BlockNumber,
			},
		}
	}
	finalized := processBlocks(consistencyLevelFinalized, w.local.Blocks[:remoteFinalizedBlockIndex+1])
	w.local.Blocks = w.local.Blocks[remoteFinalizedBlockIndex+1:]
	w.local.Finalized = remote.Finalized

	// Now that the finalized heads match, process remote.Blocks.

	// We may have already processed some of remote.Blocks if they are contained in w.local.Blocks.
	// Find the unprocessed portion of remote.Blocks.
	// This is equivalent to finding the latest block in remote.Blocks that is not in w.local.Blocks.
	unprocessedIndex := 0
	for i, block := range remote.Blocks {
		if i == len(w.local.Blocks) {
			break
		} else if !block.Equal(w.local.Blocks[i]) {
			break
		}
		unprocessedIndex++
	}

	return &Update{
		Unfinalized: append(unfinalized, w.processUnfinalizedBlocks(remote.Blocks[unprocessedIndex:])...),
		Finalized:   finalized,
	}, nil
}

func (c *Collector) processUnfinalizedBlocks(blocks []aztec.Block) []Message {
	c.local.Blocks = append(c.local.Blocks, blocks...)
	return processBlocks(consistencyLevelUnfinalized, blocks)
}

type consistencyLevel byte

const (
	consistencyLevelUnfinalized = iota
	consistencyLevelFinalized
)

func processBlocks(_ consistencyLevel, blocks []aztec.Block) []Message {
	var msgs []Message
	// Since most logs will not match the one we're interested in, this could be done more efficiently in parallel.
	// Because there will not be many TxEffects per block on testnet, we keep it sequential for now.
	for _, block := range blocks {
		for _, txEffect := range block.Body.TxEffects {
			for _, log := range txEffect.PublicLogs {
				// TODO parse logs based on consistency level
				// Will need to change the Message struct
				msgs = append(msgs, Message{
					TxHash:    txEffect.TxHash,
					BlockTime: block.Header.GlobalVariables.Timestamp,
					Log:       log,
				})
			}
		}
	}
	return msgs
}
