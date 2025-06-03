package aztec

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/wormhole-foundation/wormhole/sdk/vaa"
	"go.uber.org/zap"
)

// Global metrics variables
var (
	messagesConfirmedMetric *prometheus.CounterVec
	metricsInitialized      sync.Once
)

// initMetrics initializes the metrics only once
func initMetrics() {
	metricsInitialized.Do(func() {
		messagesConfirmedMetric = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "wormhole_aztec_observations_confirmed_total",
				Help: "Total number of verified observations found for the chain",
			}, []string{"chain_name"})
	})
}

// ObservationManager handles storage and lifecycle of pending observations
type ObservationManager interface {
	IncrementMessagesConfirmed()
}

// observationManager is the implementation of ObservationManager
type observationManager struct {
	networkID string
	logger    *zap.Logger
	metrics   observationMetrics
}

// observationMetrics holds the Prometheus metrics for the observation manager
type observationMetrics struct {
	messagesConfirmed *prometheus.CounterVec
}

// NewObservationManager creates a new observation manager
func NewObservationManager(networkID string, logger *zap.Logger) ObservationManager {
	// Initialize metrics if not already done
	initMetrics()

	// Use the global metrics
	metrics := observationMetrics{
		messagesConfirmed: messagesConfirmedMetric,
	}

	return &observationManager{
		networkID: networkID,
		logger:    logger,
		metrics:   metrics,
	}
}

// IncrementMessagesConfirmed increases the counter for confirmed messages
func (m *observationManager) IncrementMessagesConfirmed() {
	m.metrics.messagesConfirmed.WithLabelValues(m.networkID).Inc()
	m.logger.Info("Incremented messages confirmed counter")
}

// processLog handles an individual log entry
func (w *Watcher) processLog(ctx context.Context, extLog ExtendedPublicLog, blockInfo BlockInfo) error {
	// Check for context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue processing
	}

	// Skip empty logs
	if len(extLog.Log.Fields) == 0 {
		return nil
	}

	// Extract event parameters
	params, err := w.parseLogParameters(extLog.Log.Fields)
	if err != nil {
		return fmt.Errorf("failed to parse log parameters: %v", err)
	}

	// Create message payload
	rawPayload := w.createPayload(extLog.Log.Fields)

	w.logDetailedPayload(extLog.Log.Fields, rawPayload)

	// Extract structured data from the payload
	arbitrumAddress, arbitrumChainID, amount, _, err := w.extractPayloadData(rawPayload)
	if err != nil {
		w.logger.Warn("Failed to extract payload data", zap.Error(err))
		// Continue with empty values for these fields
	} else {
		// Add the extracted values to the parameters
		params.ArbitrumAddress = arbitrumAddress
		params.ArbitrumChainID = arbitrumChainID
		params.Amount = amount // Add the amount to the parameters
	}

	// Create a unique ID for this observation
	observationID := CreateObservationID(params.SenderAddress.String(), params.Sequence, extLog.ID.BlockNumber)

	// Log relevant information about the message
	w.logger.Info("Processing message",
		zap.Stringer("emitter", params.SenderAddress),
		zap.Uint64("sequence", params.Sequence),
		zap.Uint8("consistencyLevel", params.ConsistencyLevel),
		zap.String("arbitrumAddress", fmt.Sprintf("0x%x", params.ArbitrumAddress)),
		zap.Uint16("arbitrumChainID", params.ArbitrumChainID),
		zap.Uint64("amount", params.Amount), // Add this line to log the amount
		zap.Int("payloadLength", len(rawPayload)))

	// Check for context cancellation before proceeding
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue processing
	}

	// Since we're processing finalized blocks, we can publish immediately
	// regardless of the original consistency level
	if err := w.publishObservation(ctx, params, rawPayload, blockInfo, observationID); err != nil {
		return fmt.Errorf("failed to publish observation: %v", err)
	}

	return nil
}

// extractPayloadData parses the structured payload to extract key information
// extractPayloadData parses the structured payload to extract key information
func (w *Watcher) extractPayloadData(payload []byte) ([]byte, uint16, uint64, []byte, error) {
	if len(payload) < 93 { // Need at least 3 full 31-byte arrays
		return nil, 0, 0, nil, fmt.Errorf("payload too short, expected at least 93 bytes, got %d", len(payload))
	}

	// Each array is 31 bytes long for address and chain ID
	const arraySize = 31

	// Extract Arbitrum address (first 20 bytes of first array)
	arbitrumAddress := make([]byte, 20)
	copy(arbitrumAddress, payload[:20])

	// Extract Arbitrum chain ID (first 2 bytes of second array)
	chainIDLower := uint16(payload[arraySize])
	chainIDUpper := uint16(payload[arraySize+1])
	arbitrumChainID := (chainIDUpper << 8) | chainIDLower

	// The amount is now a 32-byte value starting at position 2*arraySize
	// Extract it as a full uint256
	amount := uint64(0)
	if len(payload) >= 2*arraySize+32 { // Make sure we have 32 bytes for amount
		// Read the full 32 bytes as a big-endian integer
		// For simplicity, we'll just read the first 8 bytes (uint64)
		// In a real implementation, you might need to handle the full uint256
		for i := 0; i < 8 && i < 32; i++ {
			pos := 2*arraySize + i
			if pos < len(payload) {
				amount = (amount << 8) | uint64(payload[pos])
			}
		}
	}

	// The verification data now starts after the amount (which is 32 bytes)
	verificationDataStart := 2*arraySize + 32
	verificationDataLength := len(payload) - verificationDataStart
	verificationData := make([]byte, verificationDataLength)

	if verificationDataLength > 0 {
		copy(verificationData, payload[verificationDataStart:])
	}

	// Log what we've extracted
	w.logger.Info("Extracted payload data",
		zap.String("arbitrumAddress", fmt.Sprintf("0x%x", arbitrumAddress)),
		zap.Uint16("arbitrumChainID", arbitrumChainID),
		zap.Uint64("amount", amount),
		zap.Int("verificationDataLength", verificationDataLength))

	return arbitrumAddress, arbitrumChainID, amount, verificationData, nil
}

// parseLogParameters extracts parameters from a log entry
func (w *Watcher) parseLogParameters(logEntries []string) (LogParameters, error) {
	if len(logEntries) < 4 {
		return LogParameters{}, fmt.Errorf("log has insufficient entries: %d", len(logEntries))
	}

	// First value is the sender
	senderHex := strings.TrimPrefix(logEntries[0], "0x")
	senderBytes, err := hex.DecodeString(senderHex)
	if err != nil {
		return LogParameters{}, &ErrParsingFailed{
			What: "sender address",
			Err:  err,
		}
	}

	var senderAddress vaa.Address
	copy(senderAddress[:], senderBytes)

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
		Amount:           0, // Initialize with 0, will be set later
	}, nil
}

// createPayload processes log entries that contain field elements into a byte payload
// createPayload processes log entries that contain field elements into a byte payload
func (w *Watcher) createPayload(logEntries []string) []byte {
	payload := make([]byte, 0, w.config.PayloadInitialCap)

	// Skip the first 5 entries which are metadata (sender, sequence, nonce, consistency level, timestamp)
	for i, entry := range logEntries[5:] {
		// Clean up the entry - remove 0x
		entry = strings.TrimPrefix(entry, "0x")

		// Try to decode as hex
		bytes, err := hex.DecodeString(entry)
		if err != nil {
			w.logger.Debug("Failed to decode hex", zap.Error(err))
			continue
		}

		// Remove leading zeros
		for len(bytes) > 0 && bytes[0] == 0 {
			bytes = bytes[1:]
		}

		// Reverse the bytes to correct the order
		for i, j := 0, len(bytes)-1; i < j; i, j = i+1, j-1 {
			bytes[i], bytes[j] = bytes[j], bytes[i]
		}

		// Special handling for amount (the 3rd array, index 2)
		if i == 2 {
			// This is the amount field (third array)
			// Ensure it's padded to 32 bytes
			// First, add the current bytes
			payload = append(payload, bytes...)

			// Then add padding to make it 32 bytes total
			// Don't include Jack after it - move Jack to the next 32-byte chunk
			paddingNeeded := 32 - len(bytes)
			padding := make([]byte, paddingNeeded)
			payload = append(payload, padding...)

			// Continue to next entry - skip the normal append
			continue
		}

		// If this is the entry after the amount (Jack), ensure it starts on a new 32-byte boundary
		if i == 3 {
			// This is where Jack would start
			// Calculate padding needed to align to next 32-byte boundary
			currentLength := len(payload)
			paddingNeeded := (32 - (currentLength % 32)) % 32
			if paddingNeeded > 0 {
				padding := make([]byte, paddingNeeded)
				payload = append(payload, padding...)
			}
		}

		// Add to payload
		payload = append(payload, bytes...)
	}

	// Log the final payload length and hex representation
	w.logger.Debug("Payload created",
		zap.Int("length", len(payload)),
		zap.String("hex", hex.EncodeToString(payload)))

	return payload
}

func (w *Watcher) logDetailedPayload(logEntries []string, rawPayload []byte) {
	// Log the raw log entries first
	w.logger.Info("Raw log entries received",
		zap.Int("entryCount", len(logEntries)))

	for i, entry := range logEntries {
		w.logger.Info(fmt.Sprintf("Log entry %d", i),
			zap.String("raw", entry))
	}

	// Log the raw payload bytes
	w.logger.Info("Raw payload bytes",
		zap.Int("length", len(rawPayload)),
		zap.String("hexDump", hex.Dump(rawPayload)))

	// Log address (first 20 bytes)
	w.logger.Info("Arbitrum address (first 20 bytes)",
		zap.String("hex", fmt.Sprintf("0x%x", rawPayload[:20])))

	// Log chain ID (bytes 31-32)
	chainID := uint16(rawPayload[31]) | (uint16(rawPayload[32]) << 8)
	w.logger.Info("Arbitrum chain ID (bytes 31-32)",
		zap.Uint16("value", chainID),
		zap.String("hex", fmt.Sprintf("0x%x", rawPayload[31:33])))

	// Log amount (bytes 62-93, 32 bytes)
	if len(rawPayload) >= 94 {
		w.logger.Info("Amount (bytes 62-93, 32 bytes)",
			zap.String("hex", fmt.Sprintf("0x%x", rawPayload[62:94])))
	}

	// Log name and other fields
	// The name would now start at byte 94
	if len(rawPayload) >= 94 {
		// Try to extract name
		nameBytes := []byte{}
		for i := 94; i < len(rawPayload); i++ {
			if rawPayload[i] == 0 {
				break
			}
			nameBytes = append(nameBytes, rawPayload[i])
		}
		if len(nameBytes) > 0 {
			w.logger.Info("Name",
				zap.String("value", string(nameBytes)),
				zap.String("hex", fmt.Sprintf("0x%x", nameBytes)))
		}
	}
}

// ParseHexUint64 converts a hex string to uint64
func ParseHexUint64(hexStr string) (uint64, error) {
	// Remove "0x" prefix if present
	hexStr = strings.TrimPrefix(hexStr, "0x")

	// Parse the hex string to uint64
	value, err := strconv.ParseUint(hexStr, 16, 64)
	if err != nil {
		return 0, &ErrParsingFailed{
			What: "hex uint64",
			Err:  err,
		}
	}

	return value, nil
}
