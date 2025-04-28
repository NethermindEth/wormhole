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

	// Start from our most recent block and walk backwards
	for i := len(w.processedBlocks) - 1; i >= 0; i-- {
		block := w.processedBlocks[i]

		// Check if this block is still part of the chain
		blockInfo, err := w.blockFetcher.FetchBlock(ctx, block.Number)
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

		// Check if this block is in the chain
		blockInfo, err := w.blockFetcher.FetchBlock(ctx, block.Number)
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
	return w.config.StartBlock
}

// handleChainReorg processes a detected chain reorganization
func (w *Watcher) handleChainReorg(ctx context.Context, commonAncestor int) {
	// Initial context check
	if ctx.Err() != nil {
		w.logger.Warn("Chain reorg handling cancelled by context", zap.Error(ctx.Err()))
		return
	}

	w.logger.Info("Handling chain reorganization",
		zap.Int("commonAncestor", commonAncestor),
		zap.Int("lastProcessedBlock", w.lastBlockNumber),
		zap.Int("reorgDepth", w.lastBlockNumber-commonAncestor))

	// Remove all blocks after the common ancestor
	var blocksToRemove []*ProcessedBlock
	var keptBlocks []*ProcessedBlock

	// Use a non-blocking select to check context periodically without set intervals
	for _, block := range w.processedBlocks {
		select {
		case <-ctx.Done():
			w.logger.Warn("Chain reorg handling cancelled by context during block processing",
				zap.Error(ctx.Err()))
			return
		default:
			// Continue with normal processing (non-blocking)
		}

		if block.Number > commonAncestor {
			blocksToRemove = append(blocksToRemove, block)

			// Handle observations in removed blocks
			for _, obs := range block.Observations {
				if obs.IsPublished && !obs.IsInvalidated {
					// Only handle for immediate messages
					if obs.LogParameters.ConsistencyLevel < 2 {
						obs.IsInvalidated = true
						obs.InvalidationTime = time.Now()

						w.logger.Info("Invalidating observation from removed block")
					} else {
						w.logger.Error("Found published finality-requiring message in removed block - this should not happen",
							zap.String("id", obs.ID),
							zap.Int("blockNumber", obs.BlockNumber),
							zap.Uint8("consistencyLevel", obs.LogParameters.ConsistencyLevel))
					}
				}
			}
		} else {
			keptBlocks = append(keptBlocks, block)
		}
	}

	// Final context check before making changes
	if ctx.Err() != nil {
		w.logger.Warn("Chain reorg handling cancelled before finalizing changes",
			zap.Error(ctx.Err()))
		return
	}

	// Remove blocks from hash map
	for _, block := range blocksToRemove {
		delete(w.blocksByHash, block.Hash)
	}

	// Update the processed blocks slice
	w.processedBlocks = keptBlocks

	// Update the lastBlockNumber to the common ancestor
	w.lastBlockNumber = commonAncestor

	w.logger.Info("Chain reorganization handling completed",
		zap.Int("commonAncestor", commonAncestor),
		zap.Int("removedBlocks", len(blocksToRemove)),
		zap.Int("keptBlocks", len(keptBlocks)))
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

	w.logger.Debug("Attempting to prune blocks",
		zap.Int("finalizedBlockNumber", finalizedBlock.Number),
		zap.String("finalizedBlockHash", finalizedBlock.Hash))

	// Log information about our current block storage
	w.logger.Debug("Current block storage state",
		zap.Int("processedBlocksCount", len(w.processedBlocks)),
		zap.Int("blocksByHashCount", len(w.blocksByHash)))

	// Refetch the block to get its archive hash, since node_getL2Tips does not return archives.
	// Archive is what we have as a reference and what we store as blockHash,
	// since node_getBlock returns only archives and not actual blockHashes
	refetchedBlock, err := w.blockFetcher.FetchBlock(ctx, finalizedBlock.Number)
	if err != nil {
		w.logger.Warn("Failed to refetch finalized block, keeping all blocks",
			zap.Error(err))
		return
	}

	w.logger.Debug("Refetched finalized block",
		zap.Int("blockNumber", finalizedBlock.Number),
		zap.String("refetchedHash", refetchedBlock.BlockHash),
		zap.String("reportedHash", finalizedBlock.Hash),
		zap.Bool("hashesMatch", refetchedBlock.BlockHash == finalizedBlock.Hash))

	// Use the refetched hash instead of the reported hash
	finalizedProcessedBlock, exists := w.blocksByHash[refetchedBlock.BlockHash]

	if !exists {
		w.logger.Debug("Refetched finalized block hash not found in our processed blocks",
			zap.String("refetchedHash", refetchedBlock.BlockHash))

		// Log all hashes we have in our map
		w.logger.Debug("All hashes in our blocksByHash map:")
		for hash, block := range w.blocksByHash {
			w.logger.Debug("Hash in map",
				zap.String("storedHash", hash),
				zap.Int("blockNumber", block.Number))
		}

		// Log all blocks in our processedBlocks slice
		w.logger.Debug("All blocks in our processedBlocks slice:")
		for i, block := range w.processedBlocks {
			w.logger.Debug("Block in slice",
				zap.Int("index", i),
				zap.Int("blockNumber", block.Number),
				zap.String("blockHash", block.Hash))
		}
		return
	}

	w.logger.Debug("Found finalized block in our history",
		zap.Int("blockNumber", finalizedProcessedBlock.Number),
		zap.String("blockHash", finalizedProcessedBlock.Hash))

	// Find the index of this block in our array
	finalizedBlockIdx := -1
	for i, block := range w.processedBlocks {
		if block.Hash == refetchedBlock.BlockHash {
			finalizedBlockIdx = i
			break
		}
	}

	// Keep the finalized block and all blocks after it
	if finalizedBlockIdx > 0 {
		prunedBlocks := w.processedBlocks[:finalizedBlockIdx]

		// Remove entries from the hash map for blocks being pruned
		for _, block := range prunedBlocks {
			delete(w.blocksByHash, block.Hash)
		}

		// Update the slice
		w.processedBlocks = w.processedBlocks[finalizedBlockIdx:]

		w.logger.Info("Pruned processed blocks",
			zap.Int("prunedCount", finalizedBlockIdx),
			zap.Int("remainingCount", len(w.processedBlocks)),
			zap.Int("oldestRemainingBlock", w.processedBlocks[0].Number),
			zap.String("oldestRemainingHash", w.processedBlocks[0].Hash))
	} else {
		w.logger.Debug("No blocks to prune, finalized block is the oldest or only block",
			zap.Int("finalizedBlockIdx", finalizedBlockIdx))
	}
}
