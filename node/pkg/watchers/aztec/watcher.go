package aztec

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/certusone/wormhole/node/pkg/common"
	"github.com/wormhole-foundation/wormhole/sdk/vaa"
	"go.uber.org/zap"
)

// Watcher handles the processing of blocks and logs
type Watcher struct {
	config             Config
	blockFetcher       BlockFetcher
	l1Verifier         L1Verifier
	observationManager ObservationManager
	msgC               chan<- *common.MessagePublication
	logger             *zap.Logger

	// Chain tracking for reorg detection
	mu              sync.Mutex
	processedBlocks []*ProcessedBlock
	lastBlockNumber int
	reorgDepth      int // Track deepest reorg for metrics
}

// ProcessedBlock stores information about a processed block
type ProcessedBlock struct {
	Number       int
	Hash         string
	ParentHash   string
	Observations []*ObservationRecord
	Timestamp    uint64
	IsCanonical  bool
}

// ObservationRecord represents a message that has been observed and possibly published
type ObservationRecord struct {
	ID               string
	BlockNumber      int
	LogParameters    LogParameters
	Payload          []byte
	BlockInfo        BlockInfo
	IsPublished      bool
	PublishedTime    time.Time
	IsInvalidated    bool // Set to true if this observation was invalidated by a reorg
	InvalidationTime time.Time
}

// NewWatcher creates a new Watcher
func NewWatcher(
	config Config,
	blockFetcher BlockFetcher,
	l1Verifier L1Verifier,
	observationManager ObservationManager,
	msgC chan<- *common.MessagePublication,
	logger *zap.Logger,
) *Watcher {
	return &Watcher{
		config:             config,
		blockFetcher:       blockFetcher,
		l1Verifier:         l1Verifier,
		observationManager: observationManager,
		msgC:               msgC,
		logger:             logger,
		processedBlocks:    make([]*ProcessedBlock, 0),
		lastBlockNumber:    config.StartBlock - 1, // Will process StartBlock first
		reorgDepth:         0,
	}
}

// Run starts the watcher and processes blocks
func (w *Watcher) Run(ctx context.Context) error {
	w.logger.Info("Starting Aztec watcher",
		zap.String("rpc", w.config.RpcURL),
		zap.String("contract", w.config.ContractAddress))

	// Create an error channel
	errC := make(chan error)
	defer close(errC)

	// Start the block processing goroutine
	common.RunWithScissors(ctx, errC, "aztec_events", func(ctx context.Context) error {
		return w.processBlocks(ctx)
	})

	// Start the finality checker goroutine
	common.RunWithScissors(ctx, errC, "aztec_finality_checker", func(ctx context.Context) error {
		return w.checkFinality(ctx)
	})

	// Wait for context cancellation or error
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errC:
		return err
	}
}

// GetProcessedBlockByNumber returns a processed block by its number
func (w *Watcher) GetProcessedBlockByNumber(blockNumber int) *ProcessedBlock {
	w.mu.Lock()
	defer w.mu.Unlock()

	for i := len(w.processedBlocks) - 1; i >= 0; i-- {
		if w.processedBlocks[i].Number == blockNumber {
			return w.processedBlocks[i]
		}
	}
	return nil
}

// GetLatestCanonicalBlock returns the latest block in the canonical chain
func (w *Watcher) GetLatestCanonicalBlock() *ProcessedBlock {
	w.mu.Lock()
	defer w.mu.Unlock()

	for i := len(w.processedBlocks) - 1; i >= 0; i-- {
		if w.processedBlocks[i].IsCanonical {
			return w.processedBlocks[i]
		}
	}
	return nil
}

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

// detectReorg checks if a reorganization has occurred
func (w *Watcher) detectReorg(ctx context.Context) (bool, int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// If we haven't processed any blocks yet, there's no reorg to detect
	if len(w.processedBlocks) == 0 {
		return false, 0
	}

	// Start from our most recent canonical block and work backwards
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
	w.mu.Lock()
	defer w.mu.Unlock()

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

			// Invalidate all observations in this block
			for _, obs := range block.Observations {
				if obs.IsPublished && !obs.IsInvalidated {
					obs.IsInvalidated = true
					obs.InvalidationTime = time.Now()

					// Log the invalidation
					w.logger.Info("Invalidating previously published observation due to reorg",
						zap.String("id", obs.ID),
						zap.Int("blockNumber", obs.BlockNumber),
						zap.Stringer("emitter", obs.LogParameters.SenderAddress),
						zap.Uint64("sequence", obs.LogParameters.Sequence))

					// Publish a reobservation notice
					w.publishReobservationNotice(obs)
				}
			}
		}
	}

	// Update the lastBlockNumber to the common ancestor
	w.lastBlockNumber = commonAncestor

	// Clean up and retain only the last N blocks for efficiency
	w.pruneProcessedBlocks()
}

// publishReobservationNotice sends a special message indicating an observation was invalidated
func (w *Watcher) publishReobservationNotice(obs *ObservationRecord) {
	// Convert transaction hash to byte array for txID
	txID, err := hex.DecodeString(strings.TrimPrefix(obs.BlockInfo.TxHash, "0x"))
	if err != nil {
		w.logger.Error("Failed to decode transaction hash", zap.Error(err))
		txID = []byte{0x0}
	}

	// Create a reobservation message publication
	observation := &common.MessagePublication{
		TxID:             txID,
		Timestamp:        time.Unix(int64(obs.BlockInfo.Timestamp), 0),
		Nonce:            obs.LogParameters.Nonce,
		Sequence:         obs.LogParameters.Sequence,
		EmitterChain:     w.config.ChainID,
		EmitterAddress:   obs.LogParameters.SenderAddress,
		Payload:          nil,  // Empty payload for invalidation notice
		ConsistencyLevel: 0,    // Always immediate for invalidation notices
		IsReobservation:  true, // Mark this as a reobservation/invalidation
	}

	w.logger.Info("Publishing invalidation notice for reorged message",
		zap.Stringer("emitter", observation.EmitterAddress),
		zap.Uint64("sequence", observation.Sequence),
		zap.Time("originalTime", obs.PublishedTime))

	// Send to the message channel
	w.msgC <- observation
}

// pruneProcessedBlocks removes old blocks to save memory
func (w *Watcher) pruneProcessedBlocks() {
	// Keep only the last 100 blocks for memory efficiency
	// This number can be adjusted based on expected reorg depth and memory constraints
	maxBlocksToKeep := 100

	if len(w.processedBlocks) > maxBlocksToKeep {
		pruneTo := len(w.processedBlocks) - maxBlocksToKeep
		w.processedBlocks = w.processedBlocks[pruneTo:]
	}
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

	// For the first block we process, we don't have a parent to verify
	if len(w.processedBlocks) == 0 {
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

// processLog handles an individual log entry
func (w *Watcher) processLog(ctx context.Context, extLog ExtendedPublicLog, blockInfo BlockInfo) (*ObservationRecord, error) {
	// Skip empty logs
	if len(extLog.Log.Log) == 0 {
		return nil, nil
	}

	// Extract event parameters
	params, err := w.parseLogParameters(extLog.Log.Log)
	if err != nil {
		return nil, fmt.Errorf("failed to parse log parameters: %v", err)
	}

	// Create message payload
	payload := w.createPayload(extLog.Log.Log[4:])

	// Create a unique ID for this observation
	observationID := fmt.Sprintf("%d-%s-%d", extLog.ID.BlockNumber, params.SenderAddress.String(), params.Sequence)

	// Create an observation record
	observation := &ObservationRecord{
		ID:            observationID,
		BlockNumber:   extLog.ID.BlockNumber,
		LogParameters: params,
		Payload:       payload,
		BlockInfo:     blockInfo,
		IsPublished:   false,
	}

	// Log relevant information about the message
	w.logger.Info("Processing message",
		zap.Stringer("emitter", params.SenderAddress),
		zap.Uint64("sequence", params.Sequence),
		zap.Uint8("consistencyLevel", params.ConsistencyLevel))

	// Check consistency level and handle accordingly
	switch params.ConsistencyLevel {
	case 0, 1:
		// No finality check needed, publish immediately
		if err := w.publishObservation(params, payload, blockInfo, observationID); err != nil {
			return observation, fmt.Errorf("failed to publish observation: %v", err)
		}
		observation.IsPublished = true
		observation.PublishedTime = time.Now()
	case 2:
		// Requires finality, queue for later processing
		w.observationManager.QueueObservation(params, payload, blockInfo, extLog.ID.BlockNumber)
	default:
		w.logger.Warn("Unknown consistency level, treating as immediate publish",
			zap.Uint8("level", params.ConsistencyLevel))
		if err := w.publishObservation(params, payload, blockInfo, observationID); err != nil {
			return observation, fmt.Errorf("failed to publish observation: %v", err)
		}
		observation.IsPublished = true
		observation.PublishedTime = time.Now()
	}

	return observation, nil
}

// checkFinality periodically checks for finalized observations
func (w *Watcher) checkFinality(ctx context.Context) error {
	w.logger.Info("Starting Aztec finality checker")

	ticker := time.NewTicker(w.config.FinalityCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := w.processFinality(ctx); err != nil {
				w.logger.Error("Error checking finality", zap.Error(err))
			}
		}
	}
}

// processFinality checks all pending observations for finality
func (w *Watcher) processFinality(ctx context.Context) error {
	// Start with logging the pending observations
	w.observationManager.LogPendingObservations()

	// Get all pending observations
	pendingObservations := w.observationManager.GetPendingObservations()
	if len(pendingObservations) == 0 {
		return nil
	}

	w.logger.Debug("Processing pending observations", zap.Int("count", len(pendingObservations)))

	// Get the latest finalized block
	finalizedBlock, err := w.l1Verifier.GetFinalizedBlock(ctx)
	if err != nil {
		w.logger.Warn("Failed to get finalized block, using timeout-based fallback",
			zap.Error(err))
		// Continue with fallback approach
	} else {
		w.logger.Debug("Checking against finalized block", zap.Int("finalized_block", finalizedBlock.Number))
	}

	// Process each pending observation
	var toPublish []string

	for id, observation := range pendingObservations {
		// Increment attempt count
		observation.AttemptCount++

		// First, verify this block is still in the canonical chain
		isCanonical := w.isBlockInCanonicalChain(ctx, observation.AztecBlockNum)
		if !isCanonical {
			w.logger.Warn("Observation is for a block no longer in the canonical chain",
				zap.String("id", id),
				zap.Int("aztec_block", observation.AztecBlockNum))

			// Remove this observation - it's from a reorged block
			toPublish = append(toPublish, id)
			continue
		}

		// Check if it's been waiting too long - force finality after timeout
		if time.Since(observation.SubmitTime) > w.config.FinalityTimeout {
			w.logger.Warn("Forcing finality due to timeout",
				zap.String("id", id),
				zap.Int("aztec_block", observation.AztecBlockNum),
				zap.Duration("waited", time.Since(observation.SubmitTime)))

			observationID := fmt.Sprintf("%d-%s-%d",
				observation.AztecBlockNum,
				observation.Params.SenderAddress.String(),
				observation.Params.Sequence)

			if err := w.publishObservation(
				observation.Params,
				observation.Payload,
				observation.BlockInfo,
				observationID); err != nil {
				w.logger.Error("Failed to publish timeout observation", zap.Error(err))
				continue
			}

			toPublish = append(toPublish, id)
			continue
		}

		// If we couldn't get the finalized block, use time-based fallback
		if err != nil || finalizedBlock == nil {
			// Implement fallback confirmation after 5 minutes
			if time.Since(observation.SubmitTime) > 5*time.Minute {
				w.logger.Info("Using time-based fallback confirmation",
					zap.String("id", id),
					zap.Int("aztec_block", observation.AztecBlockNum),
					zap.Duration("waited", time.Since(observation.SubmitTime)))

				observationID := fmt.Sprintf("%d-%s-%d",
					observation.AztecBlockNum,
					observation.Params.SenderAddress.String(),
					observation.Params.Sequence)

				if err := w.publishObservation(
					observation.Params,
					observation.Payload,
					observation.BlockInfo,
					observationID); err != nil {
					w.logger.Error("Failed to publish fallback observation", zap.Error(err))
					continue
				}

				toPublish = append(toPublish, id)
			}
			continue
		}

		// Check if the block is finalized
		if observation.AztecBlockNum <= finalizedBlock.Number {
			// Verify again that this block is in the canonical chain
			if !w.isBlockInCanonicalChain(ctx, observation.AztecBlockNum) {
				w.logger.Warn("Finalized observation is from a block no longer in canonical chain",
					zap.String("id", id),
					zap.Int("aztec_block", observation.AztecBlockNum))
				toPublish = append(toPublish, id)
				continue
			}

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

// isBlockInCanonicalChain verifies if a block is still part of the canonical chain
func (w *Watcher) isBlockInCanonicalChain(ctx context.Context, blockNumber int) bool {
	// First check our local cache of processed blocks
	cachedBlock := w.GetProcessedBlockByNumber(blockNumber)
	if cachedBlock != nil {
		return cachedBlock.IsCanonical
	}

	// If not in our cache, query the blockchain
	blockInfo, err := w.blockFetcher.FetchBlockInfo(ctx, blockNumber)
	if err != nil {
		w.logger.Warn("Failed to fetch block info for canonical check",
			zap.Int("blockNumber", blockNumber),
			zap.Error(err))
		return false
	}

	// Check if this block's hash matches what's currently at this height
	// We can't fully verify without the entire chain, but this is a start
	storedBlock := w.GetProcessedBlockByNumber(blockNumber)
	if storedBlock != nil {
		return blockInfo.BlockHash == storedBlock.Hash
	}

	// If we don't have it stored, assume it's canonical
	// This is a simplification, but a reasonable one
	return true
}

// publishObservation creates and publishes a message observation
func (w *Watcher) publishObservation(params LogParameters, payload []byte, blockInfo BlockInfo, observationID string) error {
	// Convert transaction hash to byte array for txID
	txID, err := hex.DecodeString(strings.TrimPrefix(blockInfo.TxHash, "0x"))
	if err != nil {
		w.logger.Error("Failed to decode transaction hash", zap.Error(err))
		// Fall back to default
		txID = []byte{0x0}
	}

	// Check if this message has already been published
	if w.hasObservationBeenPublished(observationID) {
		w.logger.Debug("Skipping already published observation",
			zap.String("id", observationID),
			zap.Stringer("emitter", params.SenderAddress),
			zap.Uint64("sequence", params.Sequence))
		return nil
	}

	// Check if we're re-observing a message that was previously invalidated
	isReobservation := w.isReobservation(params.SenderAddress, params.Sequence)

	// Create the observation
	observation := &common.MessagePublication{
		TxID:             txID,
		Timestamp:        time.Unix(int64(blockInfo.Timestamp), 0),
		Nonce:            params.Nonce,
		Sequence:         params.Sequence,
		EmitterChain:     w.config.ChainID,
		EmitterAddress:   params.SenderAddress,
		Payload:          payload,
		ConsistencyLevel: params.ConsistencyLevel,
		IsReobservation:  isReobservation,
	}

	// Increment metrics
	w.observationManager.IncrementMessagesConfirmed()

	// Log the observation
	w.logger.Info("Message observed",
		zap.String("id", observationID),
		zap.String("txHash", observation.TxIDString()),
		zap.Time("timestamp", observation.Timestamp),
		zap.Uint64("sequence", observation.Sequence),
		zap.Stringer("emitter_chain", observation.EmitterChain),
		zap.Stringer("emitter_address", observation.EmitterAddress),
		zap.Bool("is_reobservation", isReobservation))

	// Send to the message channel
	w.msgC <- observation

	// Record that we've published this observation
	w.recordPublishedObservation(observationID, params, blockInfo.BlockHash)

	return nil
}

// hasObservationBeenPublished checks if we've already published this observation
func (w *Watcher) hasObservationBeenPublished(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Check all our processed blocks
	for _, block := range w.processedBlocks {
		for _, obs := range block.Observations {
			// Skip invalidated observations
			if obs.IsInvalidated {
				continue
			}

			// If we find a published observation with this ID, return true
			if obs.ID == id && obs.IsPublished {
				return true
			}
		}
	}

	return false
}

// isReobservation checks if we've re-observing a message we've seen before
func (w *Watcher) isReobservation(emitter vaa.Address, sequence uint64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	// First pass: check if we've already seen this exact sequence/emitter combination
	messageSeenBefore := false
	for _, block := range w.processedBlocks {
		for _, obs := range block.Observations {
			if obs.LogParameters.SenderAddress == emitter && obs.LogParameters.Sequence == sequence {
				// We've seen this message before
				messageSeenBefore = true
				// If observation was already published, this must be a reobservation
				if obs.IsPublished {
					w.logger.Info("Detected reobservation of previously published message",
						zap.Stringer("emitter", emitter),
						zap.Uint64("sequence", sequence),
						zap.Bool("was_invalidated", obs.IsInvalidated))
					return true
				}
			}
		}
	}

	// If we've seen this message before, it's likely a reobservation from a reorg
	if messageSeenBefore {
		w.logger.Info("Detected potential reobservation (message seen before)",
			zap.Stringer("emitter", emitter),
			zap.Uint64("sequence", sequence))
		return true
	}

	return false
}

// recordPublishedObservation records that an observation has been published
func (w *Watcher) recordPublishedObservation(id string, params LogParameters, blockHash string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Find the observation in our blocks and mark it as published
	for _, block := range w.processedBlocks {
		for _, obs := range block.Observations {
			if obs.ID == id {
				obs.IsPublished = true
				obs.PublishedTime = time.Now()
				return
			}
		}
	}

	// If we didn't find it, log a warning
	w.logger.Warn("Could not find observation to mark as published",
		zap.String("id", id),
		zap.Stringer("emitter", params.SenderAddress),
		zap.Uint64("sequence", params.Sequence))
}

// parseLogParameters extracts parameters from a log entry
func (w *Watcher) parseLogParameters(logEntries []string) (LogParameters, error) {
	// Implementation remains the same as original
	if len(logEntries) < 4 {
		return LogParameters{}, fmt.Errorf("log has insufficient entries: %d", len(logEntries))
	}

	// First value is the sender
	sender := logEntries[0]
	var senderAddress vaa.Address
	copy(senderAddress[:], sender)

	// Parse sequence
	sequence, err := ParseHexUint64(logEntries[1])
	if err != nil {
		return LogParameters{}, fmt.Errorf("failed to parse sequence: %v", err)
	}

	// Parse nonce
	nonce, err := ParseHexUint64(logEntries[2])
	if err != nil {
		return LogParameters{}, fmt.Errorf("failed to parse nonce: %v", err)
	}

	// Parse consistency level
	consistencyLevel, err := ParseHexUint64(logEntries[3])
	if err != nil {
		return LogParameters{}, fmt.Errorf("failed to parse consistencyLevel: %v", err)
	}

	return LogParameters{
		SenderAddress:    senderAddress,
		Sequence:         sequence,
		Nonce:            uint32(nonce),
		ConsistencyLevel: uint8(consistencyLevel),
	}, nil
}

// createPayload processes log entries into a byte payload
func (w *Watcher) createPayload(logEntries []string) []byte {
	// Implementation remains the same as original
	payload := make([]byte, 0, w.config.PayloadInitialCap)

	for _, entry := range logEntries {
		hexStr := strings.TrimPrefix(entry, "0x")

		// Try to decode as hex
		bytes, err := hex.DecodeString(hexStr)
		if err != nil {
			w.logger.Debug("Failed to decode hex", zap.String("entry", entry), zap.Error(err))
			continue
		}

		// Add to payload
		payload = append(payload, bytes...)
	}

	return payload
}
