package aztec

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
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
	lastProcessedBlock int
	logger             *zap.Logger
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
		lastProcessedBlock: config.StartBlock,
		logger:             logger,
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

// processBlocks continuously checks for and processes new blocks
func (w *Watcher) processBlocks(ctx context.Context) error {
	w.logger.Info("Starting Aztec event processor")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			time.Sleep(w.config.LogProcessingInterval)

			// Try to process new blocks
			if err := w.processSingleBlock(ctx); err != nil {
				w.logger.Error("Error processing blocks", zap.Error(err))
				// Continue instead of returning to maintain service
			}
		}
	}
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

// processSingleBlock processes the next available block if any
func (w *Watcher) processSingleBlock(ctx context.Context) error {
	// Get the latest block number
	latestBlock, err := w.blockFetcher.FetchLatestBlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch latest block number: %w", err)
	}

	// Only process if there are new blocks
	if w.lastProcessedBlock >= latestBlock {
		w.logger.Debug("No new blocks to process",
			zap.Int("latest", latestBlock),
			zap.Int("lastProcessed", w.lastProcessedBlock))
		return nil
	}

	// Process the next block
	nextBlock := w.lastProcessedBlock + 1
	w.logger.Info("Processing new block", zap.Int("blockNumber", nextBlock))

	if err := w.processBlockContents(ctx, nextBlock); err != nil {
		w.logger.Error("Failed to process block",
			zap.Int("blockNumber", nextBlock),
			zap.Error(err))
		return nil // Continue in the next iteration
	}

	// Update the last processed block
	w.lastProcessedBlock = nextBlock
	return nil
}

// processBlockContents processes the logs in a single block
func (w *Watcher) processBlockContents(ctx context.Context, blockNumber int) error {
	logs, err := w.blockFetcher.FetchPublicLogs(ctx, blockNumber, blockNumber+1)
	if err != nil {
		return fmt.Errorf("failed to fetch public logs: %w", err)
	}

	w.logger.Info("Processing logs",
		zap.Int("count", len(logs)),
		zap.Int("blockNumber", blockNumber))

	// Process each log
	for _, log := range logs {
		if err := w.processLog(ctx, log); err != nil {
			w.logger.Error("Failed to process log",
				zap.Int("block", log.ID.BlockNumber),
				zap.Error(err))
			// Continue with other logs
		}
	}

	return nil
}

// processLog handles an individual log entry
func (w *Watcher) processLog(ctx context.Context, extLog ExtendedPublicLog) error {
	// Log basic info
	w.logger.Info("Log found",
		zap.Int("block", extLog.ID.BlockNumber),
		zap.String("contract", extLog.Log.ContractAddress))

	// Skip empty logs
	if len(extLog.Log.Log) == 0 {
		return nil
	}

	// Extract event parameters
	params, err := w.parseLogParameters(extLog.Log.Log)
	if err != nil {
		return fmt.Errorf("failed to parse log parameters: %w", err)
	}

	// Create message payload
	payload := w.createPayload(extLog.Log.Log[4:])

	// Get block info for transaction ID and timestamp
	blockInfo, err := w.blockFetcher.FetchBlockInfo(ctx, extLog.ID.BlockNumber)
	if err != nil {
		w.logger.Warn("Failed to get block info, using defaults", zap.Error(err))
		blockInfo = BlockInfo{
			TxHash:    "0x0000000000000000000000000000000000000000000000000000000000000000",
			Timestamp: uint64(time.Now().Unix()),
		}
	}

	// Check consistency level and handle accordingly
	switch params.ConsistencyLevel {
	case 0, 1:
		// No finality check needed, publish immediately
		return w.publishObservation(params, payload, blockInfo)
	case 2:
		// Requires finality, queue for later processing
		w.observationManager.QueueObservation(params, payload, blockInfo, extLog.ID.BlockNumber)
		return nil
	default:
		w.logger.Warn("Unknown consistency level, treating as immediate publish",
			zap.Uint8("level", params.ConsistencyLevel))
		return w.publishObservation(params, payload, blockInfo)
	}
}

// processFinality checks all pending observations for finality
func (w *Watcher) processFinality(ctx context.Context) error {
	// Start with logging the pending observations for visibility
	w.observationManager.LogPendingObservations()

	// Get all pending observations
	pendingObservations := w.observationManager.GetPendingObservations()
	if len(pendingObservations) == 0 {
		w.logger.Info("No pending observations to check")
		return nil
	}

	w.logger.Info("Processing pending observations", zap.Int("count", len(pendingObservations)))

	// Get the latest finalized block
	finalizedBlock, err := w.l1Verifier.GetFinalizedBlock(ctx)
	if err != nil {
		w.logger.Warn("Failed to get finalized block, using timeout-based fallback",
			zap.Error(err))
		// Continue with fallback approach
	} else {
		w.logger.Info("Checking pending observations against finalized block",
			zap.Int("finalized_block", finalizedBlock.Number))
	}

	// Process each pending observation
	var toPublish []string

	for id, observation := range pendingObservations {
		// Increment attempt count
		observation.AttemptCount++

		// Check if it's been waiting too long - force finality after timeout
		if time.Since(observation.SubmitTime) > w.config.FinalityTimeout {
			w.logger.Warn("Forcing finality due to timeout",
				zap.String("id", id),
				zap.Int("aztec_block", observation.AztecBlockNum),
				zap.Duration("waited", time.Since(observation.SubmitTime)))

			if err := w.publishObservation(observation.Params, observation.Payload, observation.BlockInfo); err != nil {
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

				if err := w.publishObservation(observation.Params, observation.Payload, observation.BlockInfo); err != nil {
					w.logger.Error("Failed to publish fallback observation", zap.Error(err))
					continue
				}

				toPublish = append(toPublish, id)
			}
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
			if err := w.publishObservation(observation.Params, observation.Payload, observation.BlockInfo); err != nil {
				w.logger.Error("Failed to publish finalized observation", zap.Error(err))
				continue
			}

			toPublish = append(toPublish, id)
		} else {
			// Not yet finalized
			blocksLeft := observation.AztecBlockNum - finalizedBlock.Number
			w.logger.Info("Aztec block not yet finalized",
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

		// Log the updated pending observations
		w.observationManager.LogPendingObservations()
	}

	return nil
}

// Helper methods

// parseLogParameters extracts parameters from a log entry
func (w *Watcher) parseLogParameters(logEntries []string) (LogParameters, error) {
	if len(logEntries) < 4 {
		return LogParameters{}, fmt.Errorf("log has insufficient entries: %d", len(logEntries))
	}

	// First value is the sender
	sender := logEntries[0]
	var senderAddress vaa.Address
	copy(senderAddress[:], sender)
	w.logger.Info("Sender", zap.String("value", sender))

	// Parse sequence
	sequence, err := ParseHexUint64(logEntries[1])
	if err != nil {
		return LogParameters{}, fmt.Errorf("failed to parse sequence: %w", err)
	}
	w.logger.Info("Sequence", zap.Uint64("value", sequence))

	// Parse nonce
	nonce, err := ParseHexUint64(logEntries[2])
	if err != nil {
		return LogParameters{}, fmt.Errorf("failed to parse nonce: %w", err)
	}
	w.logger.Info("Nonce", zap.Uint64("value", nonce))

	// Parse consistency level
	consistencyLevel, err := ParseHexUint64(logEntries[3])
	if err != nil {
		return LogParameters{}, fmt.Errorf("failed to parse consistencyLevel: %w", err)
	}
	w.logger.Info("ConsistencyLevel", zap.Uint64("value", consistencyLevel))

	return LogParameters{
		SenderAddress:    senderAddress,
		Sequence:         sequence,
		Nonce:            uint32(nonce),
		ConsistencyLevel: uint8(consistencyLevel),
	}, nil
}

// createPayload processes log entries into a byte payload
func (w *Watcher) createPayload(logEntries []string) []byte {
	payload := make([]byte, 0, w.config.PayloadInitialCap)

	for i, entry := range logEntries {
		hexStr := strings.TrimPrefix(entry, "0x")

		// Try to decode as hex
		bytes, err := hex.DecodeString(hexStr)
		if err != nil {
			w.logger.Warn("Failed to decode hex", zap.String("entry", entry), zap.Error(err))
			continue
		}

		// Add to payload
		payload = append(payload, bytes...)

		// Try to interpret as a string for logging
		w.logInterpretedValue(i+4, bytes, entry)
	}

	return payload
}

// logInterpretedValue attempts to interpret bytes as string or number for logging
func (w *Watcher) logInterpretedValue(index int, bytes []byte, rawHex string) {
	// Trim leading null bytes
	startIndex := 0
	for startIndex < len(bytes) && bytes[startIndex] == 0 {
		startIndex++
	}
	trimmedBytes := bytes[startIndex:]

	// Check if it's a printable string
	if str := string(trimmedBytes); IsPrintableString(str) {
		w.logger.Debug("Field as string", zap.Int("index", index), zap.String("value", str))
	} else {
		// Fall back to numeric representation
		w.logger.Debug("Field as number", zap.Int("index", index), zap.String("value", rawHex))
	}
}

// publishObservation creates and publishes a message observation
func (w *Watcher) publishObservation(params LogParameters, payload []byte, blockInfo BlockInfo) error {
	// Convert transaction hash to byte array for txID
	txID, err := hex.DecodeString(strings.TrimPrefix(blockInfo.TxHash, "0x"))
	if err != nil {
		w.logger.Error("Failed to decode transaction hash", zap.Error(err))
		// Fall back to default
		txID = []byte{0x0}
	}

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
		IsReobservation:  false,
	}

	// Increment metrics
	w.observationManager.IncrementMessagesConfirmed()

	// Log the observation
	w.logger.Info("Message observed",
		zap.String("txHash", observation.TxIDString()),
		zap.Time("timestamp", observation.Timestamp),
		zap.Uint32("nonce", observation.Nonce),
		zap.Uint64("sequence", observation.Sequence),
		zap.Stringer("emitter_chain", observation.EmitterChain),
		zap.Stringer("emitter_address", observation.EmitterAddress),
		zap.Uint8("consistencyLevel", observation.ConsistencyLevel))

	// Send to the message channel
	w.msgC <- observation

	return nil
}
