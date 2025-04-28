package aztec

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// processFinality checks all pending observations for finality
func (w *Watcher) processFinality(ctx context.Context) error {
	// Start with logging the pending observations
	w.observationManager.LogPendingObservations()

	// Get all pending observations
	pendingObservations := w.observationManager.GetPendingObservations()
	if len(pendingObservations) == 0 {
		w.logger.Debug("No pending observations")
		return nil
	}

	w.logger.Debug("Processing pending observations", zap.Int("count", len(pendingObservations)))

	// Get the latest finalized block
	finalizedBlock, err := w.l1Verifier.GetFinalizedBlock(ctx)
	if err != nil {
		// Handle the error locally instead of returning it to prevent goroutine exit
		w.logger.Warn("Failed to get finalized block, will retry later",
			zap.Error(err))
		// Continue with the next iteration rather than exiting
		return nil
	}

	w.logger.Debug("Checking against finalized block", zap.Int("finalized_block", finalizedBlock.Number))

	// Process each pending observation
	var toPublish []string

	for id, observation := range pendingObservations {
		// Increment attempt count
		observation.AttemptCount++

		// First, verify this block is still in the chain
		isValid := w.isBlockInChain(ctx, observation.AztecBlockNum)
		if !isValid {
			w.logger.Warn("Observation is for a block no longer in the chain",
				zap.String("id", id),
				zap.Int("aztec_block", observation.AztecBlockNum))

			// Remove this observation - it's from a reorged block
			toPublish = append(toPublish, id)
			continue
		}

		// Check if the block is finalized
		if observation.AztecBlockNum <= finalizedBlock.Number {
			w.logger.Info("Aztec block is now finalized",
				zap.String("id", id),
				zap.Int("aztec_block", observation.AztecBlockNum),
				zap.Int("finalized_block", finalizedBlock.Number))

			// Calculate finality time for metrics
			finalityTime := time.Since(observation.SubmitTime).Seconds()
			w.observationManager.RecordFinalityTime(finalityTime)

			// Publish the observation
			observationID := fmt.Sprintf("%d-%s-%d",
				observation.AztecBlockNum,
				observation.Params.SenderAddress.String(),
				observation.Params.Sequence)

			if err := w.publishObservation(
				ctx,
				observation.Params,
				observation.Payload,
				observation.BlockInfo,
				observationID); err != nil {
				w.logger.Error("Failed to publish finalized observation", zap.Error(err))
				continue
			}

			toPublish = append(toPublish, id)
		} else {
			// Not yet finalized - only log at debug level
			blocksLeft := observation.AztecBlockNum - finalizedBlock.Number
			w.logger.Debug("Aztec block not yet finalized",
				zap.String("id", id),
				zap.Int("aztec_block", observation.AztecBlockNum),
				zap.Int("finalized_block", finalizedBlock.Number),
				zap.Int("blocks_left", blocksLeft),
				zap.Duration("waiting_for", time.Since(observation.SubmitTime)))
		}
	}

	// Remove published observations from the map
	for _, id := range toPublish {
		w.observationManager.RemoveObservation(id)
	}

	// Update metrics
	w.observationManager.UpdateMetrics()

	if len(toPublish) > 0 {
		w.logger.Info("Processed finalized observations",
			zap.Int("published", len(toPublish)),
			zap.Int("remaining", len(pendingObservations)-len(toPublish)))
	}

	return nil
}

// GetProcessedBlockByNumber returns a processed block by its number
func (w *Watcher) GetProcessedBlockByNumber(blockNumber int) *ProcessedBlock {
	for i := len(w.processedBlocks) - 1; i >= 0; i-- {
		if w.processedBlocks[i].Number == blockNumber {
			return w.processedBlocks[i]
		}
	}
	return nil
}

// isBlockInChain verifies if a block is still part of the chain
func (w *Watcher) isBlockInChain(ctx context.Context, blockNumber int) bool {
	// First check our local cache of processed blocks
	cachedBlock := w.GetProcessedBlockByNumber(blockNumber)
	if cachedBlock != nil {
		return true
	}

	// If not in our cache, query the blockchain
	blockInfo, err := w.blockFetcher.FetchBlock(ctx, blockNumber)
	if err != nil {
		w.logger.Warn("Failed to fetch block info for chain check",
			zap.Int("blockNumber", blockNumber),
			zap.Error(err))
		return false
	}

	// Check if this block's hash matches what's currently at this height
	storedBlock := w.GetProcessedBlockByNumber(blockNumber)
	if storedBlock != nil {
		return blockInfo.BlockHash == storedBlock.Hash
	}

	// If we don't have it stored, assume it's valid
	return true
}
