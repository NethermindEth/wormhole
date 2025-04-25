package aztec

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// processBlocks continuously checks for and processes new blocks
func (w *Watcher) processBlocks(ctx context.Context) error {
	w.logger.Info("Starting Aztec event processor with reorg handling")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			time.Sleep(w.config.LogProcessingInterval)

			// Get the latest block number and hash
			latestBlock, err := w.blockFetcher.FetchLatestBlockNumber(ctx)
			if err != nil {
				w.logger.Error("Failed to fetch latest block number", zap.Error(err))
				continue
			}

			// Check for new blocks to process
			if w.lastBlockNumber >= latestBlock {
				w.logger.Debug("No new blocks to process",
					zap.Int("latest", latestBlock),
					zap.Int("lastProcessed", w.lastBlockNumber))
				continue
			}

			// Process blocks one by one, checking for reorgs
			if err := w.syncChain(ctx, latestBlock); err != nil {
				w.logger.Error("Error syncing chain", zap.Error(err))
				// Continue instead of returning to maintain service
			}
		}
	}
}

// runBlockPruner periodically prunes old blocks
func (w *Watcher) runBlockPruner(ctx context.Context) error {
	w.logger.Info("Starting Aztec block pruner")

	// Run pruning every hour or as configured
	pruneInterval := 1 * time.Hour
	if w.config.BlockPruneInterval > 0 {
		pruneInterval = w.config.BlockPruneInterval
	}

	ticker := time.NewTicker(pruneInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			w.pruneProcessedBlocks(ctx)
		}
	}
}

// syncChain syncs the watcher's state with the blockchain, handling reorgs
func (w *Watcher) syncChain(ctx context.Context, targetHeight int) error {
	// Start from the latest processed block + 1, or config.StartBlock if none
	nextBlockToProcess := w.lastBlockNumber + 1

	w.logger.Debug("Syncing chain",
		zap.Int("currentHeight", w.lastBlockNumber),
		zap.Int("targetHeight", targetHeight))

	// First, check for reorgs by validating our known chain
	reorgDetected, commonAncestor := w.detectReorg(ctx)

	if reorgDetected {
		// Handle the reorg - rollback to common ancestor
		w.handleChainReorg(ctx, commonAncestor)

		// Update next block to process after rollback
		nextBlockToProcess = commonAncestor + 1
	}

	// Now process new blocks from where we left off (or after rollback)
	for blockNum := nextBlockToProcess; blockNum <= targetHeight; blockNum++ {
		if err := w.processBlockWithReorgHandling(ctx, blockNum); err != nil {
			w.logger.Error("Failed to process block",
				zap.Int("blockNumber", blockNum),
				zap.Error(err))
			// Stop processing if we hit an error
			return err
		}

		// Update the last processed block number
		w.lastBlockNumber = blockNum
	}

	return nil
}

// processBlockWithReorgHandling processes a single block with awareness of reorgs
func (w *Watcher) processBlockWithReorgHandling(ctx context.Context, blockNumber int) error {
	// Fetch full block info including hash and parent hash
	blockInfo, err := w.blockFetcher.FetchBlockInfo(ctx, blockNumber)
	if err != nil {
		return fmt.Errorf("failed to fetch block info: %v", err)
	}

	// Add validation for empty timestamp
	if blockInfo.Timestamp == 0 {
		// Use current time as a fallback
		w.logger.Warn("Block has empty or zero timestamp, using current time",
			zap.Int("blockNumber", blockNumber))
		blockInfo.Timestamp = uint64(time.Now().Unix())
	}

	// Verify this block connects to our chain
	if blockNumber > w.config.StartBlock {
		parentOk := w.verifyBlockParent(blockInfo, blockNumber)
		if !parentOk {
			return fmt.Errorf("block %d parent hash doesn't match our chain", blockNumber)
		}
	}

	// Process the block's logs
	observations, err := w.processBlockLogs(ctx, blockNumber, blockInfo)
	if err != nil {
		return fmt.Errorf("failed to process block logs: %v", err)
	}

	// Create a new processed block record
	processedBlock := &ProcessedBlock{
		Number:       blockNumber,
		Hash:         blockInfo.BlockHash,
		ParentHash:   blockInfo.ParentHash,
		Observations: observations,
		Timestamp:    blockInfo.Timestamp,
		IsCanonical:  true,
	}

	// Add this block to our tracking
	w.appendProcessedBlock(processedBlock)

	return nil
}

// verifyBlockParent ensures a block connects to our known chain
func (w *Watcher) verifyBlockParent(blockInfo BlockInfo, blockNumber int) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	// For the genesis block (block 0) and the first block, no parent verification is needed
	if blockNumber <= 1 || len(w.processedBlocks) == 0 {
		return true
	}

	// Get the expected parent block
	parentNumber := blockNumber - 1

	// Find the parent block in our processed blocks
	var parentBlock *ProcessedBlock
	for i := len(w.processedBlocks) - 1; i >= 0; i-- {
		if w.processedBlocks[i].Number == parentNumber && w.processedBlocks[i].IsCanonical {
			parentBlock = w.processedBlocks[i]
			break
		}
	}

	// If we don't have the parent, we can't verify
	if parentBlock == nil {
		w.logger.Warn("Cannot verify block parent, parent not in our history",
			zap.Int("blockNumber", blockNumber),
			zap.Int("parentNumber", parentNumber))
		return true // Assume it's OK and continue
	}

	// Verify the parent hash matches
	parentHashMatches := blockInfo.ParentHash == parentBlock.Hash

	if !parentHashMatches {
		w.logger.Warn("Block parent hash mismatch detected",
			zap.Int("blockNumber", blockNumber),
			zap.String("expectedParentHash", parentBlock.Hash),
			zap.String("actualParentHash", blockInfo.ParentHash))
	}

	return parentHashMatches
}

// appendProcessedBlock adds a block to our tracking
func (w *Watcher) appendProcessedBlock(block *ProcessedBlock) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.processedBlocks = append(w.processedBlocks, block)

	// Log at appropriate level based on observations
	if len(block.Observations) > 0 {
		w.logger.Info("Processed block with observations",
			zap.Int("blockNumber", block.Number),
			zap.String("blockHash", block.Hash),
			zap.Int("observationCount", len(block.Observations)))
	} else {
		w.logger.Debug("Processed block with no observations",
			zap.Int("blockNumber", block.Number),
			zap.String("blockHash", block.Hash))
	}
}

// processBlockLogs processes the logs in a single block
func (w *Watcher) processBlockLogs(ctx context.Context, blockNumber int, blockInfo BlockInfo) ([]*ObservationRecord, error) {
	logs, err := w.blockFetcher.FetchPublicLogs(ctx, blockNumber, blockNumber+1)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch public logs: %v", err)
	}

	observations := make([]*ObservationRecord, 0)

	// Only log if there are actually logs to process
	if len(logs) > 0 {
		w.logger.Info("Processing logs",
			zap.Int("count", len(logs)),
			zap.Int("blockNumber", blockNumber))
	}

	// Process each log
	for _, log := range logs {
		obs, err := w.processLog(ctx, log, blockInfo)
		if err != nil {
			w.logger.Error("Failed to process log",
				zap.Int("block", log.ID.BlockNumber),
				zap.Error(err))
			// Continue with other logs
			continue
		}

		if obs != nil {
			observations = append(observations, obs)
		}
	}

	return observations, nil
}
