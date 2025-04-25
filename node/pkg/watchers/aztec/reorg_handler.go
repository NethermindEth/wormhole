package aztec

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// detectReorg checks if a reorganization has occurred
func (w *Watcher) detectReorg(ctx context.Context) (bool, int) {
	// If we haven't processed any blocks yet, there's no reorg to detect
	if len(w.processedBlocks) == 0 {
		return false, 0
	}

	// Start from our most recent canonical block and walk backwards
	for i := len(w.processedBlocks) - 1; i >= 0; i-- {
		block := w.processedBlocks[i]
		if !block.IsCanonical {
			continue // Skip blocks we've already marked as non-canonical
		}

		// Check if this block is still part of the canonical chain
		blockInfo, err := w.blockFetcher.FetchBlockInfo(ctx, block.Number)
		if err != nil {
			w.logger.Warn("Failed to fetch block info for reorg detection",
				zap.Int("blockNumber", block.Number),
				zap.Error(err))
			continue
		}

		// Compare hashes to detect a reorg
		if blockInfo.BlockHash != block.Hash {
			w.logger.Warn("Detected blockchain reorganization",
				zap.Int("blockNumber", block.Number),
				zap.String("storedHash", block.Hash),
				zap.String("currentHash", blockInfo.BlockHash))

			// Find the common ancestor by checking each previous block
			commonAncestor := w.findCommonAncestor(ctx, i)

			// Calculate and log reorg depth for metrics
			reorgDepth := block.Number - commonAncestor
			if reorgDepth > w.reorgDepth {
				w.reorgDepth = reorgDepth
				w.logger.Info("New max reorg depth detected", zap.Int("depth", reorgDepth))
			}

			return true, commonAncestor
		}
	}

	return false, 0
}

// findCommonAncestor walks backwards through our chain to find where it matches the current chain
func (w *Watcher) findCommonAncestor(ctx context.Context, startIdx int) int {
	// Start checking from the block before the one where we detected a discrepancy
	for i := startIdx - 1; i >= 0; i-- {
		block := w.processedBlocks[i]

		// Skip blocks we've already marked as non-canonical
		if !block.IsCanonical {
			continue
		}

		// Check if this block is in the canonical chain
		blockInfo, err := w.blockFetcher.FetchBlockInfo(ctx, block.Number)
		if err != nil {
			w.logger.Warn("Failed to fetch block info while finding common ancestor",
				zap.Int("blockNumber", block.Number),
				zap.Error(err))
			continue
		}

		// If the hashes match, we've found our common ancestor
		if blockInfo.BlockHash == block.Hash {
			w.logger.Info("Found common ancestor during reorg",
				zap.Int("blockNumber", block.Number),
				zap.String("blockHash", block.Hash))
			return block.Number
		}
	}

	// If we couldn't find a common ancestor, start from genesis
	w.logger.Warn("Could not find common ancestor, reverting to genesis block")
	return w.config.StartBlock - 1
}

// handleChainReorg processes a detected chain reorganization
func (w *Watcher) handleChainReorg(ctx context.Context, commonAncestor int) {

	w.logger.Info("Handling chain reorganization",
		zap.Int("commonAncestor", commonAncestor),
		zap.Int("lastProcessedBlock", w.lastBlockNumber),
		zap.Int("reorgDepth", w.lastBlockNumber-commonAncestor))

	// Mark all blocks after the common ancestor as non-canonical
	for i := len(w.processedBlocks) - 1; i >= 0; i-- {
		block := w.processedBlocks[i]

		if block.Number > commonAncestor && block.IsCanonical {
			// Mark this block as non-canonical
			block.IsCanonical = false

			// Update the map entry (no need to delete since we keep track of IsCanonical)
			w.blocksByHash[block.Hash] = block

			// Invalidate all observations in this block
			for _, obs := range block.Observations {
				if obs.IsPublished && !obs.IsInvalidated {
					// Only invalidate and send notices for immediate messages
					if obs.LogParameters.ConsistencyLevel < 2 {
						obs.IsInvalidated = true
						obs.InvalidationTime = time.Now()

						w.logger.Info("Invalidating previously published observation due to reorg")
						w.publishReobservationNotice(obs)
					} else {
						// This should never happen if we're doing things correctly,
						// because consistency level 2 messages should only be published
						// after finality, and finalized blocks cannot be reorganized
						w.logger.Error("Found published finality-requiring message in reorged block - this should not happen",
							zap.String("id", obs.ID),
							zap.Int("blockNumber", obs.BlockNumber),
							zap.Uint8("consistencyLevel", obs.LogParameters.ConsistencyLevel))
					}
				}
			}
		}
	}

	// Update the lastBlockNumber to the common ancestor
	w.lastBlockNumber = commonAncestor

	// Clean up and retain only the blocks after the finalized one
	w.pruneProcessedBlocks(ctx)
}

// pruneProcessedBlocks removes old blocks to save memory while keeping all potentially reorg-able blocks
// We always keep the chain from the last finalized block and frontwards.
func (w *Watcher) pruneProcessedBlocks(ctx context.Context) {
	// First, find out what the latest finalized block is
	finalizedBlock, err := w.l1Verifier.GetFinalizedBlock(ctx)
	if err != nil {
		// If we can't get the finalized block, be conservative and keep everything
		w.logger.Warn("Failed to get finalized block for pruning, keeping all blocks",
			zap.Error(err))
		return
	}

	// Find the index of the finalized block in our processed blocks
	finalizedBlockIdx := -1
	for i, block := range w.processedBlocks {
		if block.Number == finalizedBlock.Number {
			finalizedBlockIdx = i
			break
		}
	}

	// If we can't find the finalized block, keep everything
	if finalizedBlockIdx == -1 {
		w.logger.Debug("Finalized block not found in processed blocks, keeping all blocks")
		return
	}

	// Keep the finalized block and all blocks after it
	if finalizedBlockIdx > 0 {
		// Remove entries from the hash map for blocks being pruned
		for i := 0; i < finalizedBlockIdx; i++ {
			delete(w.blocksByHash, w.processedBlocks[i].Hash)
		}

		// Update the slice
		w.processedBlocks = w.processedBlocks[finalizedBlockIdx:]

		w.logger.Debug("Pruned processed blocks",
			zap.Int("prunedCount", finalizedBlockIdx),
			zap.Int("remainingCount", len(w.processedBlocks)),
			zap.Int("finalizedBlock", finalizedBlock.Number))
	}
}
