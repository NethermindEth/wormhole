package aztec

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/wormhole-foundation/wormhole/sdk/vaa"
	"go.uber.org/zap"
)

// processLog handles an individual log entry
func (w *Watcher) processLog(ctx context.Context, extLog ExtendedPublicLog, blockInfo BlockInfo) (*ObservationRecord, error) {
	// Check for context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		// Continue processing
	}

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

	// Check for context cancellation before proceeding
	select {
	case <-ctx.Done():
		return observation, ctx.Err()
	default:
		// Continue processing
	}

	// Check consistency level and handle accordingly
	switch params.ConsistencyLevel {
	case 0, 1:
		// No finality check needed, publish immediately
		if err := w.publishObservation(ctx, params, payload, blockInfo, observationID); err != nil {
			return observation, fmt.Errorf("failed to publish observation: %v", err)
		}
		observation.IsPublished = true
		observation.PublishedTime = time.Now()
	case 2:
		// Requires finality, queue for later processing
		// Pass context to QueueObservation if the interface supports it, otherwise use as is
		w.observationManager.QueueObservation(params, payload, blockInfo, extLog.ID.BlockNumber)
	default:
		w.logger.Warn("Unknown consistency level, treating as immediate publish",
			zap.Uint8("level", params.ConsistencyLevel))
		if err := w.publishObservation(ctx, params, payload, blockInfo, observationID); err != nil {
			return observation, fmt.Errorf("failed to publish observation: %v", err)
		}
		observation.IsPublished = true
		observation.PublishedTime = time.Now()
	}

	return observation, nil
}

// hasObservationBeenPublished checks if we've already published this observation
func (w *Watcher) hasObservationBeenPublished(id string) bool {
	// Take a snapshot of blocks under mutex
	w.mu.Lock()
	blocksCopy := make([]*ProcessedBlock, len(w.processedBlocks))
	copy(blocksCopy, w.processedBlocks)
	w.mu.Unlock()

	// Check outside the mutex
	for _, block := range blocksCopy {
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

func (w *Watcher) isReobservation(emitter vaa.Address, sequence uint64) bool {
	// Take a snapshot of blocks under mutex
	w.mu.Lock()
	blocksCopy := make([]*ProcessedBlock, len(w.processedBlocks))
	copy(blocksCopy, w.processedBlocks)
	w.mu.Unlock()

	// Check if we've already published a message with this emitter+sequence
	for _, block := range blocksCopy {
		for _, obs := range block.Observations {
			if obs.LogParameters.SenderAddress == emitter &&
				obs.LogParameters.Sequence == sequence &&
				obs.IsPublished {
				w.logger.Info("Detected reobservation of previously published message",
					zap.Stringer("emitter", emitter),
					zap.Uint64("sequence", sequence),
					zap.Bool("was_invalidated", obs.IsInvalidated))
				return true
			}
		}
	}

	return false
}

// recordPublishedObservation records that an observation has been published
func (w *Watcher) recordPublishedObservation(id string, params LogParameters) {
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
