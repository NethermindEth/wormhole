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

func (r *ReorgError) Error() string {
	return fmt.Sprintf("reorg detected at height %d", r.height)
}

type FinalizedReorgError struct {
	e ReorgError
}

func (r *FinalizedReorgError) Error() string {
	return fmt.Sprintf("finalized %s", &r.e)
}

func (r *FinalizedReorgError) Unwrap() error {
	return &r.e
}

// assumes end.Number >= start.Number
func (w *Collector) blocks(ctx context.Context, start, end aztec.Tip) ([]aztec.Block, error) {
	limit := end.Number - start.Number + 1
	blocks, err := w.c.Blocks(ctx, start.Number, limit, false)
	if err != nil {
		return nil, fmt.Errorf("get blocks: %v", err)
	} else if uint64(len(blocks)) != limit {
		return nil, &ReorgError{
			height: end.Number,
		}
	} else if firstHash := blocks[0].Header.Hash(); !firstHash.Equal(&start.Hash) {
		return nil, &ReorgError{
			height: start.Number,
		}
	} else if lastHash := blocks[len(blocks)-1].Header.Hash(); !lastHash.Equal(&end.Hash) {
		return nil, &ReorgError{
			height: end.Number,
		}
	}
	return blocks, nil
}

func (w *Collector) Update(ctx context.Context) (*Update, error) {
	l2Tips, err := w.c.L2Tips(ctx)
	if err != nil {
		return nil, fmt.Errorf("get l2 tips: %v", err)
	}

	remoteBlocks, err := w.blocks(ctx, l2Tips.Finalized, l2Tips.Latest)
	if err != nil {
		var e ReorgError
		if errors.Is(err, &e) && e.height == l2Tips.Finalized.Number {
			err = &FinalizedReorgError{
				e: e,
			}
		}
		return nil, err
	}
	remote := View{
		Finalized: remoteBlocks[0],
		Blocks:    remoteBlocks[1:],
	}

	// The algorithm for reconciling the local and remote views depends on their relative positions.
	// The remote view can be:
	//
	//   1. behind the local view: the remote finalized height is less than the local finalized height.
	//   2. ahead of the local view: the remote height is greater than the local latest height.
	//   3. within the local view: neither ahead nor behind.

	var unfinalized, finalized []Message
	if remote.IsBehind(w.local) {
		return nil, &FinalizedReorgError{
			e: ReorgError{
				height: w.local.Finalized.Header.GlobalVariables.BlockNumber,
			},
		}
	} else if remote.IsAhead(w.local) {
		// Process all blocks in the range (local finalized head, remote finalized head].
		latestLocalHeader := w.local.LatestBlock().Header
		blocks, err := w.blocks(ctx, latestLocalHeader.ToTip(), remote.Finalized.Header.ToTip())
		if err != nil {
			var e ReorgError
			if errors.Is(err, &e) {
				err = &FinalizedReorgError{
					e: e,
				}
			}
			return nil, err
		}
		blocks = blocks[1:] // Remove local latest block.
		unfinalized = append(unfinalized, processBlocks(consistencyLevelUnfinalized, blocks)...)
		w.local.Blocks = append(w.local.Blocks, blocks...)
		finalized = append(finalized, processBlocks(consistencyLevelFinalized, w.local.Blocks)...)
		w.local.Finalized = remote.Finalized
		w.local.Blocks = nil
	} else {
		remoteFinalizedBlockIndex := remote.Finalized.Header.GlobalVariables.BlockNumber - w.local.Finalized.Header.GlobalVariables.BlockNumber
		if !remote.Finalized.Equal(w.local.Blocks[remoteFinalizedBlockIndex]) {
			return nil, &FinalizedReorgError{
				e: ReorgError{
					height: w.local.Finalized.Header.GlobalVariables.BlockNumber,
				},
			}
		}
		finalized = append(finalized, processBlocks(consistencyLevelFinalized, w.local.Blocks[:remoteFinalizedBlockIndex+1])...)
		w.local.Finalized = remote.Finalized
		w.local.Blocks = w.local.Blocks[remoteFinalizedBlockIndex+1:]
		// TODO reconcile local and remote unfinalized blocks. There could have been a reorg (no worries since they're unfinalized, just need to deal with it).
	}
	// Process remaining unfinalized blocks.

	unfinalized = append(unfinalized, processBlocks(consistencyLevelUnfinalized, remote.Blocks)...)
	w.local.Blocks = append(w.local.Blocks, remote.Blocks...)
	return &Update{
		Unfinalized: unfinalized,
		Finalized:   finalized,
	}, nil
}

type consistencyLevel byte

const (
	consistencyLevelUnfinalized = iota
	consistencyLevelFinalized
)

func processBlocks(_ consistencyLevel, blocks []aztec.Block) []Message {
	var msgs []Message
	// TODO since most logs will not match the one we're interested in,
	// this iteration could be done in parallel with locks.
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
