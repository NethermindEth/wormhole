package aztec

import (
	"context"
	"encoding/hex"
	"strings"
	"time"

	"github.com/certusone/wormhole/node/pkg/common"
	"go.uber.org/zap"
)

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

// publishObservation creates and publishes a message observation
func (w *Watcher) publishObservation(ctx context.Context, params LogParameters, payload []byte, blockInfo BlockInfo, observationID string) error {
	// Check for context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue processing
	}

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

	// Check for context cancellation after potentially long operation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue processing
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
	select {
	case w.msgC <- observation:
		// Message sent successfully
	case <-ctx.Done():
		return ctx.Err()
	}

	// Record that we've published this observation
	w.recordPublishedObservation(observationID, params)

	return nil
}
