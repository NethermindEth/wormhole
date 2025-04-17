package aztec

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/certusone/wormhole/node/pkg/common"
	gossipv1 "github.com/certusone/wormhole/node/pkg/proto/gossip/v1"
	"github.com/certusone/wormhole/node/pkg/readiness"
	"github.com/certusone/wormhole/node/pkg/supervisor"
	"github.com/certusone/wormhole/node/pkg/watchers"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/wormhole-foundation/wormhole/sdk/vaa"
	"go.uber.org/zap"
)

// Configuration constants
const (
	// Time intervals
	logProcessingInterval = 1 * time.Second
	finalityCheckInterval = 10 * time.Second
	timeoutDuration       = 30 * time.Minute // Force finality after 30 minutes of waiting

	// Processing parameters
	payloadInitialCap = 13

	// Default starting block
	defaultStartBlock = 0

	// Ethereum rollup contract config
	rollupContractAddress = "0x0b306bf915c4d645ff596e518faf3f9669b97016" // Replace with actual Aztec rollup contract address
)

// PendingObservation represents an observation waiting for Ethereum finality
type PendingObservation struct {
	Params        LogParameters
	Payload       []byte
	BlockInfo     BlockInfo
	AztecBlockNum int
	AttemptCount  int
	SubmitTime    time.Time
}

// Watcher monitors the Aztec blockchain for message publications
type Watcher struct {
	// Chain identification
	chainID   vaa.ChainID
	networkID string

	// Connection details
	rpcURL          string
	contractAddress string
	ethRpcURL       string // Ethereum RPC URL for finality checks

	// Communication channels
	msgC     chan<- *common.MessagePublication
	obsvReqC <-chan *gossipv1.ObservationRequest

	// Service state
	readinessSync      readiness.Component
	lastProcessedBlock int

	// Finality handling
	pendingObservations     map[string]*PendingObservation // key is a unique ID combining sender and sequence
	pendingObservationMutex sync.Mutex
}

// metrics for monitoring
var (
	aztecMessagesConfirmed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "wormhole_aztec_observations_confirmed_total",
			Help: "Total number of verified observations found for the chain",
		}, []string{"chain_name"})

	aztecMessagesPending = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "wormhole_aztec_observations_pending",
			Help: "Number of observations waiting for Ethereum finality",
		}, []string{"chain_name"})

	aztecL1FinalityTime = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "wormhole_aztec_l1_finality_time_seconds",
			Help:    "Time in seconds for an Aztec block to be finalized on L1",
			Buckets: prometheus.ExponentialBuckets(10, 2, 10), // From 10s to ~2.8h
		}, []string{"chain_name"})

	aztecL1LookupFailures = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "wormhole_aztec_l1_lookup_failures_total",
			Help: "Number of failures when looking up Aztec blocks in L1",
		}, []string{"chain_name"})
)

// NewWatcher creates a new Aztec watcher
func NewWatcher(
	chainID vaa.ChainID,
	networkID watchers.NetworkID,
	rpcURL string,
	contractAddress string,
	ethRpcURL string,
	msgC chan<- *common.MessagePublication,
	obsvReqC <-chan *gossipv1.ObservationRequest,
) *Watcher {
	return &Watcher{
		chainID:             chainID,
		networkID:           string(networkID),
		rpcURL:              rpcURL,
		contractAddress:     contractAddress,
		ethRpcURL:           ethRpcURL,
		msgC:                msgC,
		obsvReqC:            obsvReqC,
		readinessSync:       common.MustConvertChainIdToReadinessSyncing(chainID),
		lastProcessedBlock:  defaultStartBlock,
		pendingObservations: make(map[string]*PendingObservation),
	}
}

// Run starts the watcher service and handles the main event loop
func (w *Watcher) Run(ctx context.Context) error {
	logger := supervisor.Logger(ctx)
	logger.Info("Starting Aztec watcher",
		zap.String("rpc", w.rpcURL),
		zap.String("eth_rpc", w.ethRpcURL),
		zap.String("contract", w.contractAddress))

	// Create an error channel and ticker
	errC := make(chan error)
	defer close(errC)

	// Signal that basic initialization is complete
	readiness.SetReady(w.readinessSync)

	// Signal to the supervisor that this runnable has finished initialization
	supervisor.Signal(ctx, supervisor.SignalHealthy)

	// Start the single block processing goroutine
	common.RunWithScissors(ctx, errC, "aztec_events", func(ctx context.Context) error {
		logger.Info("Starting Aztec event processor")

		for {
			select {
			case err := <-errC:
				logger.Error("Worker error detected", zap.Error(err))
				return fmt.Errorf("worker died: %w", err)

			case <-ctx.Done():
				logger.Info("Context done, shutting down")
				return ctx.Err()

			default:
				// Wait before processing more blocks
				time.Sleep(logProcessingInterval)

				// Check for and process new blocks
				if err := w.fetchAndProcessBlocks(ctx, logger); err != nil {
					logger.Error("Error processing blocks", zap.Error(err))
					// Continue instead of returning to maintain service
				}
			}
		}
	})

	// Start the Ethereum finality checker goroutine
	common.RunWithScissors(ctx, errC, "aztec_finality_checker", func(ctx context.Context) error {
		logger.Info("Starting Ethereum finality checker")

		ticker := time.NewTicker(finalityCheckInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				if err := w.checkPendingObservations(ctx, logger); err != nil {
					logger.Error("Error checking pending observations", zap.Error(err))
				}
			}
		}
	})

	// Wait for context cancellation or error
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errC:
		return err
	}
}

// fetchAndProcessBlocks checks for new blocks and processes them if found
func (w *Watcher) fetchAndProcessBlocks(ctx context.Context, logger *zap.Logger) error {
	// Get the latest block number
	latestBlock, err := w.fetchLatestBlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("error getting latest block: %w", err)
	}

	// Only process if there are new blocks
	if w.lastProcessedBlock >= latestBlock {
		logger.Debug("No new blocks to process",
			zap.Int("latest", latestBlock),
			zap.Int("lastProcessed", w.lastProcessedBlock))
		return nil
	}

	// Process each block one by one
	nextBlock := w.lastProcessedBlock + 1
	logger.Info("Processing new block", zap.Int("blockNumber", nextBlock))

	// Process the single block
	if err := w.processBlock(ctx, logger, nextBlock); err != nil {
		logger.Error("Failed to process block",
			zap.Int("blockNumber", nextBlock),
			zap.Error(err))
		// We continue processing in the next loop iteration
		return nil
	}

	// Update the last processed block
	w.lastProcessedBlock = nextBlock
	logger.Info("Completed processing block", zap.Int("blockNumber", nextBlock))

	return nil
}

// processBlock handles fetching and processing logs for a single block
func (w *Watcher) processBlock(ctx context.Context, logger *zap.Logger, blockNumber int) error {
	logger.Info("Processing block", zap.Int("blockNumber", blockNumber))

	// Get logs for this specific block
	logs, err := w.fetchPublicLogs(ctx, blockNumber, blockNumber+1)
	if err != nil {
		return fmt.Errorf("failed to fetch logs: %w", err)
	}

	logger.Info("Processing logs",
		zap.Int("count", len(logs)),
		zap.Int("blockNumber", blockNumber))

	// Process each log
	for _, extLog := range logs {
		if err := w.processLog(ctx, logger, extLog); err != nil {
			logger.Error("Failed to process log",
				zap.Int("block", extLog.ID.BlockNumber),
				zap.Error(err))
			// Continue processing other logs
		}
	}

	return nil
}

// processLog handles processing a single log entry
func (w *Watcher) processLog(ctx context.Context, logger *zap.Logger, extLog ExtendedPublicLog) error {
	// Log basic info
	logger.Info("Log found",
		zap.Int("block", extLog.ID.BlockNumber),
		zap.String("contract", extLog.Log.ContractAddress))

	// Skip empty logs
	if len(extLog.Log.Log) == 0 {
		return nil
	}

	// Extract event parameters
	params, err := w.parseLogParameters(logger, extLog.Log.Log)
	if err != nil {
		return fmt.Errorf("failed to parse log parameters: %w", err)
	}

	// Create message payload
	payload := w.createPayload(logger, extLog.Log.Log[4:])

	// Get block info for transaction ID and timestamp
	blockInfo, err := w.fetchBlockInfo(ctx, extLog.ID.BlockNumber)
	if err != nil {
		logger.Warn("Failed to get block info, using defaults", zap.Error(err))
		blockInfo = BlockInfo{
			TxHash:    "0x0000000000000000000000000000000000000000000000000000000000000000",
			Timestamp: uint64(time.Now().Unix()),
		}
	}

	// Check consistency level and handle accordingly
	switch params.ConsistencyLevel {
	case 0, 1:
		// No finality check needed, publish immediately
		return w.publishObservation(logger, params, payload, blockInfo)
	case 2:
		// Requires Ethereum finality, queue for later processing
		return w.queueForFinality(ctx, logger, params, payload, blockInfo, extLog.ID.BlockNumber)
	default:
		logger.Warn("Unknown consistency level, treating as immediate publish",
			zap.Uint8("level", params.ConsistencyLevel))
		return w.publishObservation(logger, params, payload, blockInfo)
	}
}

// queueForFinality adds an observation to the pending queue for Ethereum finality checking
func (w *Watcher) queueForFinality(ctx context.Context, logger *zap.Logger, params LogParameters, payload []byte, blockInfo BlockInfo, blockNumber int) error {
	// Create unique ID for this observation
	observationID := w.createObservationID(params, blockNumber)

	logger.Info("Creating observation ID",
		zap.String("id", observationID),
		zap.Stringer("sender", params.SenderAddress),
		zap.Uint64("sequence", params.Sequence))

	// Create pending observation entry
	pendingObservation := &PendingObservation{
		Params:        params,
		Payload:       payload,
		BlockInfo:     blockInfo,
		AztecBlockNum: blockNumber,
		AttemptCount:  0,
		SubmitTime:    time.Now(),
	}

	// Add to pending map
	w.pendingObservationMutex.Lock()

	// Check if this observation already exists
	if existing, exists := w.pendingObservations[observationID]; exists {
		logger.Warn("Observation already pending, replacing",
			zap.String("id", observationID),
			zap.Int("existing_block", existing.AztecBlockNum),
			zap.Int("new_block", blockNumber))
	}

	w.pendingObservations[observationID] = pendingObservation
	pendingCount := len(w.pendingObservations)

	// Log all pending observations for debugging
	pendingIds := make([]string, 0, pendingCount)
	for id := range w.pendingObservations {
		pendingIds = append(pendingIds, id)
	}

	w.pendingObservationMutex.Unlock()

	// Update metrics
	aztecMessagesPending.WithLabelValues(w.networkID).Set(float64(pendingCount))

	logger.Info("Queued observation for Ethereum finality check",
		zap.String("id", observationID),
		zap.Int("aztec_block", blockNumber),
		zap.Uint64("sequence", params.Sequence),
		zap.Stringer("emitter", params.SenderAddress),
		zap.Int("total_pending", pendingCount),
		zap.Strings("pending_ids", pendingIds))

	// Log the full state of pending observations
	w.logPendingObservationsDetails(logger)

	return nil
}

// checkPendingObservations processes the queue of observations waiting for Ethereum finality
func (w *Watcher) checkPendingObservations(ctx context.Context, logger *zap.Logger) error {
	// Start with a contract check
	w.checkRollupContractExists(ctx, logger)

	// Log the full state of pending observations at the start
	w.logPendingObservationsDetails(logger)

	// Check if Ethereum RPC is available
	isEthRpcAvailable := w.isEthereumRpcAvailable(ctx, logger)
	if !isEthRpcAvailable {
		logger.Warn("Ethereum RPC is not available, using fallback confirmation method")
	}

	// Variable to hold finalized block if we can get it
	var finalizedBlock *EthereumBlock
	var finalizedBlockErr error

	// Only try to get finalized block if Ethereum RPC is available
	if isEthRpcAvailable {
		finalizedBlock, finalizedBlockErr = w.fetchEthereumFinalizedBlock(ctx)
		if finalizedBlockErr != nil {
			logger.Warn("Failed to get Ethereum finalized block, using fallback confirmation method",
				zap.Error(finalizedBlockErr))
		} else {
			logger.Info("Checking pending observations",
				zap.Uint64("finalized_eth_block", finalizedBlock.Number))
		}
	}

	// Create a list of observations to publish
	var toPublish []*PendingObservation
	var toRemove []string

	// Lock the mutex while we iterate
	w.pendingObservationMutex.Lock()
	pendingCount := len(w.pendingObservations)
	w.pendingObservationMutex.Unlock()

	if pendingCount == 0 {
		logger.Info("No pending observations to check")
		return nil // No pending observations to check
	}

	logger.Info("Processing pending observations", zap.Int("count", pendingCount))

	// Process each pending observation
	w.pendingObservationMutex.Lock()
	for id, observation := range w.pendingObservations {
		// Check if it's been waiting too long - force finality after timeout
		if time.Since(observation.SubmitTime) > timeoutDuration {
			logger.Warn("Forcing finality due to timeout",
				zap.String("id", id),
				zap.Int("aztec_block", observation.AztecBlockNum),
				zap.Duration("waited", time.Since(observation.SubmitTime)))

			toPublish = append(toPublish, observation)
			toRemove = append(toRemove, id)
			continue
		}

		// If Ethereum RPC is not available or we couldn't get a finalized block,
		// use a time-based fallback (confirm after waiting a certain period)
		if !isEthRpcAvailable || finalizedBlockErr != nil {
			// Implement fallback confirmation after 5 minutes
			if time.Since(observation.SubmitTime) > 5*time.Minute {
				logger.Info("Using time-based fallback confirmation",
					zap.String("id", id),
					zap.Int("aztec_block", observation.AztecBlockNum),
					zap.Duration("waited", time.Since(observation.SubmitTime)))

				toPublish = append(toPublish, observation)
				toRemove = append(toRemove, id)
			}
			continue
		}

		// Only try the L1 block check if we have a valid finalized block
		if finalizedBlock != nil {
			// Check if the Aztec block has been included in an L1 block
			l1BlockNumber, err := w.getL1BlockForAztecBlock(ctx, logger, observation.AztecBlockNum)
			if err != nil {
				logger.Warn("Failed to get L1 block for Aztec block",
					zap.String("id", id),
					zap.Int("aztec_block", observation.AztecBlockNum),
					zap.Error(err))
				// Update attempt count and continue
				observation.AttemptCount++
				aztecL1LookupFailures.WithLabelValues(w.networkID).Inc()
				continue
			}

			if l1BlockNumber == 0 {
				// Block not yet included in L1
				logger.Info("Aztec block not yet included in L1",
					zap.String("id", id),
					zap.Int("aztec_block", observation.AztecBlockNum),
					zap.Int("attempts", observation.AttemptCount))
				observation.AttemptCount++
				continue
			}

			// Check if L1 block is finalized
			if l1BlockNumber <= finalizedBlock.Number {
				// L1 block is finalized, we can publish the observation
				logger.Info("Aztec block is now finalized on Ethereum L1",
					zap.String("id", id),
					zap.Int("aztec_block", observation.AztecBlockNum),
					zap.Uint64("l1_block", l1BlockNumber),
					zap.Uint64("finalized_block", finalizedBlock.Number))

				// Calculate finality time for metrics
				finalityTime := time.Since(observation.SubmitTime).Seconds()
				aztecL1FinalityTime.WithLabelValues(w.networkID).Observe(finalityTime)

				// Add to publish list
				toPublish = append(toPublish, observation)
				toRemove = append(toRemove, id)
			} else {
				// Not yet finalized
				blocksLeft := l1BlockNumber - finalizedBlock.Number
				logger.Info("Aztec block not yet finalized on L1",
					zap.String("id", id),
					zap.Int("aztec_block", observation.AztecBlockNum),
					zap.Uint64("l1_block", l1BlockNumber),
					zap.Uint64("finalized_block", finalizedBlock.Number),
					zap.Uint64("blocks_left", blocksLeft),
					zap.Duration("waiting_for", time.Since(observation.SubmitTime)))

				observation.AttemptCount++
			}
		}
	}
	w.pendingObservationMutex.Unlock()

	// Publish all finalized observations
	for _, observation := range toPublish {
		if err := w.publishObservation(logger, observation.Params, observation.Payload, observation.BlockInfo); err != nil {
			logger.Error("Failed to publish finalized observation", zap.Error(err))
		}
	}

	// Remove published observations from the map
	if len(toRemove) > 0 {
		w.pendingObservationMutex.Lock()
		for _, id := range toRemove {
			logger.Info("Removing observation from pending map",
				zap.String("id", id),
				zap.Int("aztec_block", w.pendingObservations[id].AztecBlockNum))
			delete(w.pendingObservations, id)
		}
		// Update metrics and log the new state
		pendingCount = len(w.pendingObservations)
		aztecMessagesPending.WithLabelValues(w.networkID).Set(float64(pendingCount))
		w.pendingObservationMutex.Unlock()

		logger.Info("Processed finalized observations",
			zap.Int("published", len(toPublish)),
			zap.Int("remaining", pendingCount))

		// Log the updated pending observations
		w.logPendingObservationsDetails(logger)
	}

	return nil
}

// isEthereumRpcAvailable checks if the Ethereum RPC endpoint is responsive
func (w *Watcher) isEthereumRpcAvailable(ctx context.Context, logger *zap.Logger) bool {
	// Try a basic JSON-RPC call that should work on any Ethereum client
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  "net_version",
		"params":  []any{},
		"id":      1,
	}

	// Send the request
	jsonData, err := json.Marshal(payload)
	if err != nil {
		logger.Warn("Failed to marshal JSON for Ethereum RPC check", zap.Error(err))
		return false
	}

	req, err := http.NewRequestWithContext(ctx, "POST", w.ethRpcURL, bytes.NewBuffer(jsonData))
	if err != nil {
		logger.Warn("Failed to create HTTP request for Ethereum RPC check", zap.Error(err))
		return false
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Warn("Ethereum RPC endpoint is not available",
			zap.String("url", w.ethRpcURL),
			zap.Error(err))
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Warn("Ethereum RPC returned non-OK status",
			zap.Int("status", resp.StatusCode))
		return false
	}

	// Check if we got a valid response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Warn("Failed to read Ethereum RPC response", zap.Error(err))
		return false
	}

	var response struct {
		Result string `json:"result"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		logger.Warn("Failed to parse Ethereum RPC response", zap.Error(err))
		return false
	}

	logger.Info("Ethereum RPC is available",
		zap.String("network_id", response.Result))
	return true
}

// logPendingObservationsDetails logs detailed information about all pending observations
func (w *Watcher) logPendingObservationsDetails(logger *zap.Logger) {
	w.pendingObservationMutex.Lock()
	defer w.pendingObservationMutex.Unlock()

	mapSize := len(w.pendingObservations)
	logger.Info("======= PENDING OBSERVATIONS DETAILS =======",
		zap.Int("total_count", mapSize))

	if mapSize == 0 {
		logger.Info("No pending observations")
		return
	}

	for id, obs := range w.pendingObservations {
		logger.Info("Pending observation",
			zap.String("id", id),
			zap.Int("aztec_block", obs.AztecBlockNum),
			zap.Uint64("sequence", obs.Params.Sequence),
			zap.Stringer("sender", obs.Params.SenderAddress),
			zap.Duration("pending_for", time.Since(obs.SubmitTime)),
			zap.Int("attempts", obs.AttemptCount))
	}
	logger.Info("============================================")
}

// getL1BlockForAztecBlock finds the Ethereum L1 block that includes the Aztec L2 block
func (w *Watcher) getL1BlockForAztecBlock(ctx context.Context, logger *zap.Logger, aztecBlockNum int) (uint64, error) {
	// Enhanced logging
	logger.Info("🔍 Looking for Ethereum L1 inclusion of Aztec block",
		zap.Int("aztec_block_num", aztecBlockNum))

	// Get the block
	block, err := w.fetchBlockDetails(ctx, aztecBlockNum)
	if err != nil {
		logger.Error("❌ Failed to fetch Aztec block details",
			zap.Int("aztec_block", aztecBlockNum),
			zap.Error(err))
		return 0, fmt.Errorf("failed to fetch Aztec block details: %w", err)
	}

	// Extract the block hash
	blockHash := block.Root
	logger.Info("🔍 Retrieved Aztec block hash",
		zap.Int("aztec_block", aztecBlockNum),
		zap.String("block_hash", blockHash))

	// Add details about the block
	logger.Info("🔍 Aztec block details",
		zap.Int("aztec_block", aztecBlockNum),
		zap.String("block_hash", blockHash),
		zap.Int("next_available_leaf_index", block.NextAvailableLeafIndex))

	// Query the rollup contract to find when this block was included in L1
	l1BlockNumber, err := w.findL1InclusionBlock(ctx, logger, blockHash)
	if err != nil {
		logger.Error("❌ Failed to find L1 inclusion block",
			zap.Int("aztec_block", aztecBlockNum),
			zap.String("block_hash", blockHash),
			zap.Error(err))
		return 0, fmt.Errorf("failed to find L1 inclusion block: %w", err)
	}

	return l1BlockNumber, nil
}

// fetchBlockDetails gets detailed information about an Aztec block
func (w *Watcher) fetchBlockDetails(ctx context.Context, blockNumber int) (*BlockArchive, error) {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  "node_getBlock",
		"params":  []any{blockNumber},
		"id":      1,
	}

	// Send the request
	responseBody, err := w.sendJSONRPCRequest(ctx, payload)
	if err != nil {
		return nil, err
	}

	// Parse the response
	var response BlockResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, fmt.Errorf("failed to parse block response: %w", err)
	}

	return &response.Result.Archive, nil
}

// findL1InclusionBlock searches for the Ethereum block that includes the Aztec block
func (w *Watcher) findL1InclusionBlock(ctx context.Context, logger *zap.Logger, blockHash string) (uint64, error) {
	// Log the block hash we're searching for in a more visible format
	logger.Info("🔍 SEARCHING FOR AZTEC BLOCK IN ETHEREUM CHAIN",
		zap.String("aztec_block_hash", blockHash))

	// First, we need to create a filter to get relevant events
	from, to, err := w.getSuitableBlockRange(ctx)
	if err != nil {
		logger.Error("❌ Failed to get block range for Ethereum search", zap.Error(err))
		return 0, fmt.Errorf("failed to get block range: %w", err)
	}

	logger.Info("🔍 Searching Ethereum blocks for Aztec inclusion",
		zap.String("aztec_block_hash", blockHash),
		zap.Uint64("from_block", from),
		zap.Uint64("to_block", to),
		zap.String("rollup_contract", rollupContractAddress))

	// Prepare the filter for the block commitment events
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  "eth_getLogs",
		"params": []any{map[string]any{
			"fromBlock": fmt.Sprintf("0x%x", from),
			"toBlock":   fmt.Sprintf("0x%x", to),
			"address":   rollupContractAddress,
		}},
		"id": 1,
	}

	// Log the actual JSON payload for debugging
	payloadJson, _ := json.Marshal(payload)
	logger.Debug("🔍 eth_getLogs payload", zap.String("payload", string(payloadJson)))

	// Send the request
	jsonData, err := json.Marshal(payload)
	if err != nil {
		logger.Error("❌ Failed to marshal JSON for eth_getLogs", zap.Error(err))
		return 0, fmt.Errorf("error marshaling JSON: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", w.ethRpcURL, bytes.NewBuffer(jsonData))
	if err != nil {
		logger.Error("❌ Failed to create request for eth_getLogs", zap.Error(err))
		return 0, fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Log that we're about to send the request
	logger.Debug("🔍 Sending eth_getLogs request to Ethereum node", zap.String("url", w.ethRpcURL))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Error("❌ Failed to send eth_getLogs request", zap.Error(err))
		return 0, fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Error("❌ Unexpected status code from eth_getLogs",
			zap.Int("status_code", resp.StatusCode))
		return 0, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("❌ Failed to read eth_getLogs response body", zap.Error(err))
		return 0, fmt.Errorf("error reading response: %w", err)
	}

	// Debug the raw response
	logger.Debug("🔍 Raw eth_getLogs response",
		zap.String("response", string(body)))

	// Check if the response contains an error
	var errorCheck struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &errorCheck); err == nil && errorCheck.Error != nil {
		logger.Error("❌ eth_getLogs returned an error",
			zap.Int("code", errorCheck.Error.Code),
			zap.String("message", errorCheck.Error.Message))
		return 0, fmt.Errorf("ethereum RPC error: %s (code: %d)",
			errorCheck.Error.Message, errorCheck.Error.Code)
	}

	// Parse the response to find the event
	var logsResponse struct {
		Result []struct {
			BlockNumber string   `json:"blockNumber"`
			Data        string   `json:"data"`
			Topics      []string `json:"topics"`
		} `json:"result"`
	}

	if err := json.Unmarshal(body, &logsResponse); err != nil {
		logger.Error("❌ Failed to parse logs response", zap.Error(err))
		return 0, fmt.Errorf("failed to parse logs response: %w", err)
	}

	logger.Info("🔍 Received logs from rollup contract",
		zap.Int("log_count", len(logsResponse.Result)),
		zap.String("contract", rollupContractAddress))

	// For debugging, log all logs we received
	for i, log := range logsResponse.Result {
		logger.Debug("🔍 Log entry",
			zap.Int("index", i),
			zap.String("block_number", log.BlockNumber),
			zap.Strings("topics", log.Topics),
			zap.String("data", log.Data))
	}

	// Detailed logging about the hash we're looking for
	logger.Info("🔍 Looking for block hash in logs",
		zap.String("aztec_block_hash", blockHash),
		zap.String("aztec_block_hash_no_prefix", strings.TrimPrefix(blockHash, "0x")))

	// Look through the logs for our block hash
	for _, log := range logsResponse.Result {
		// Enhanced logging for each log we're checking
		topicsStr := strings.Join(log.Topics, ", ")
		logger.Debug("🔍 Checking log for block hash",
			zap.String("block_number", log.BlockNumber),
			zap.String("topics", topicsStr))

		// Check if this log contains our block hash
		containsHash, matchDetails := containsBlockHashDetailed(log.Data, log.Topics, blockHash)
		if containsHash {
			// Found the matching event, get the block number
			blockNum, err := hexToUint64(log.BlockNumber)
			if err != nil {
				logger.Error("❌ Failed to parse block number", zap.Error(err))
				return 0, fmt.Errorf("failed to parse block number: %w", err)
			}

			logger.Info("✅ FOUND L1 inclusion for Aztec block",
				zap.String("aztec_block_hash", blockHash),
				zap.Uint64("l1_block_number", blockNum),
				zap.String("match_location", matchDetails))

			return blockNum, nil
		}
	}

	// If we get here, we haven't found the block inclusion yet
	logger.Info("⏳ Aztec block not yet included in L1",
		zap.String("aztec_block_hash", blockHash),
		zap.Int("logs_checked", len(logsResponse.Result)))

	// IMPORTANT: Check if the rollup contract address is correct
	logger.Warn("⚠️ Possible issue: Check if rollup contract address is correct",
		zap.String("current_contract_address", rollupContractAddress),
		zap.String("suggestion", "Verify this matches the actual Aztec rollup contract"))

	return 0, nil
}

// containsBlockHashDetailed checks if the event data contains the specified block hash
// and returns details on where it was found
func containsBlockHashDetailed(data string, topics []string, blockHash string) (bool, string) {
	// Remove 0x prefix from all strings for consistency
	data = strings.TrimPrefix(data, "0x")
	blockHash = strings.TrimPrefix(blockHash, "0x")

	// Check if the blockHash appears in any of the topics
	for i, topic := range topics {
		topic = strings.TrimPrefix(topic, "0x")
		if topic == blockHash {
			return true, fmt.Sprintf("Found in topic %d", i)
		}
	}

	// Check for the block hash in the data
	if len(data) < 64 {
		return false, ""
	}

	// Scan through the data in 32-byte chunks (64 hex chars)
	for i := 0; i <= len(data)-64; i += 64 {
		chunk := data[i : i+64]
		if chunk == blockHash {
			return true, fmt.Sprintf("Found in data at offset %d", i)
		}
	}

	// For partial matches at the end
	if len(data)%64 >= len(blockHash) {
		endChunk := data[len(data)-(len(data)%64):]
		if endChunk == blockHash {
			return true, "Found in data at end chunk"
		}
	}

	return false, ""
}

// containsBlockHash checks if the event data contains the specified block hash
func containsBlockHash(data string, topics []string, blockHash string) bool {
	// Remove 0x prefix from all strings for consistency
	data = strings.TrimPrefix(data, "0x")
	blockHash = strings.TrimPrefix(blockHash, "0x")

	// Check if the blockHash appears in any of the topics
	// Topics are usually 32 bytes (64 hex chars) each
	for _, topic := range topics {
		topic = strings.TrimPrefix(topic, "0x")
		if topic == blockHash {
			return true
		}
	}

	// Check for the block hash in the data
	// For Aztec, we expect the block hash might be a 32-byte value in the data
	// We need to scan the data for the block hash

	// If data is empty, no match
	if len(data) < 64 {
		return false
	}

	// Scan through the data in 32-byte chunks (64 hex chars)
	for i := 0; i <= len(data)-64; i += 64 {
		chunk := data[i : i+64]
		if chunk == blockHash {
			return true
		}
	}

	// For partial matches at the end
	if len(data)%64 >= len(blockHash) {
		endChunk := data[len(data)-(len(data)%64):]
		if endChunk == blockHash {
			return true
		}
	}

	return false
}

// getSuitableBlockRange returns a reasonable block range to search for L1 inclusions
func (w *Watcher) getSuitableBlockRange(ctx context.Context) (uint64, uint64, error) {
	// Get the latest Ethereum block
	latest, err := w.fetchEthereumLatestBlock(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get latest Ethereum block: %w", err)
	}

	// Calculate a reasonable search window
	// Start from 1000 blocks ago or block 0, whichever is larger
	from := uint64(0)
	if latest.Number > 1000 {
		from = latest.Number - 1000
	}

	return from, latest.Number, nil
}

// fetchEthereumFinalizedBlock gets the latest finalized block from Ethereum
func (w *Watcher) fetchEthereumFinalizedBlock(ctx context.Context) (*EthereumBlock, error) {
	// First try with "finalized" tag which is supported post-merge
	block, err := w.fetchEthereumBlock(ctx, "finalized")
	if err != nil {
		// If that fails, try "safe" which is another post-merge tag
		block, err = w.fetchEthereumBlock(ctx, "safe")
		if err != nil {
			// If both post-merge tags fail, fall back to "latest" and apply a safety offset
			// for test environments or pre-merge clients
			latestBlock, err := w.fetchEthereumBlock(ctx, "latest")
			if err != nil {
				return nil, fmt.Errorf("failed to get Ethereum block with any tag: %w", err)
			}

			// Assume finality after 12 blocks for PoW or test networks (conservative estimate)
			// If the chain is very new, just use block 0 as finalized
			var finalizedNumber uint64
			if latestBlock.Number > 12 {
				finalizedNumber = latestBlock.Number - 12
			} else {
				finalizedNumber = 0
			}

			// Fetch the actual block at that number
			blockNumberHex := fmt.Sprintf("0x%x", finalizedNumber)
			return w.fetchEthereumBlock(ctx, blockNumberHex)
		}
	}
	return block, nil
}

// checkRollupContractExists checks if the rollup contract actually exists
func (w *Watcher) checkRollupContractExists(ctx context.Context, logger *zap.Logger) {
	// Create a payload to check the contract code
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  "eth_getCode",
		"params":  []any{rollupContractAddress, "latest"},
		"id":      1,
	}

	// Send the request
	jsonData, err := json.Marshal(payload)
	if err != nil {
		logger.Error("❌ Failed to marshal JSON for eth_getCode",
			zap.Error(err))
		return
	}

	req, err := http.NewRequestWithContext(ctx, "POST", w.ethRpcURL, bytes.NewBuffer(jsonData))
	if err != nil {
		logger.Error("❌ Failed to create request for eth_getCode",
			zap.Error(err))
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Error("❌ Failed to send eth_getCode request",
			zap.Error(err))
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("❌ Failed to read eth_getCode response",
			zap.Error(err))
		return
	}

	// Check if the contract has code
	var response struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		logger.Error("❌ Failed to parse eth_getCode response",
			zap.Error(err))
		return
	}

	// Check if the contract exists (has code)
	if response.Result == "0x" || response.Result == "" {
		logger.Error("❌ ROLLUP CONTRACT NOT FOUND - NO CODE AT ADDRESS",
			zap.String("contract_address", rollupContractAddress))
	} else {
		codeLength := len(response.Result) - 2 // subtract "0x"
		logger.Info("✅ Rollup contract exists",
			zap.String("contract_address", rollupContractAddress),
			zap.Int("code_length_hex_chars", codeLength))
	}
}

// fetchEthereumLatestBlock gets the latest block from Ethereum
func (w *Watcher) fetchEthereumLatestBlock(ctx context.Context) (*EthereumBlock, error) {
	return w.fetchEthereumBlock(ctx, "latest")
}

// fetchEthereumBlock gets a block from Ethereum by tag or number
func (w *Watcher) fetchEthereumBlock(ctx context.Context, blockTag string) (*EthereumBlock, error) {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  "eth_getBlockByNumber",
		"params":  []any{blockTag, true},
		"id":      1,
	}

	// Send the request
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("error marshaling JSON: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", w.ethRpcURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response: %w", err)
	}

	var response struct {
		Result struct {
			Number    string `json:"number"`
			Hash      string `json:"hash"`
			Timestamp string `json:"timestamp"`
		} `json:"result"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse block response: %w", err)
	}

	// Parse the block number
	blockNumber, err := hexToUint64(response.Result.Number)
	if err != nil {
		return nil, fmt.Errorf("failed to parse block number: %w", err)
	}

	// Parse the timestamp
	timestamp, err := hexToUint64(response.Result.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to parse timestamp: %w", err)
	}

	return &EthereumBlock{
		Number:    blockNumber,
		Hash:      response.Result.Hash,
		Timestamp: timestamp,
	}, nil
}

// createObservationID creates a unique ID for tracking pending observations
func (w *Watcher) createObservationID(params LogParameters, blockNumber int) string {
	return fmt.Sprintf("%s-%d-%d", params.SenderAddress.String(), params.Sequence, blockNumber)
}

// parseLogParameters extracts parameters from a log entry
func (w *Watcher) parseLogParameters(logger *zap.Logger, logEntries []string) (LogParameters, error) {
	if len(logEntries) < 4 {
		return LogParameters{}, fmt.Errorf("log has insufficient entries: %d", len(logEntries))
	}

	// First value is the sender
	sender := logEntries[0]
	var senderAddress vaa.Address
	copy(senderAddress[:], sender)
	logger.Info("Sender", zap.String("value", sender))

	// Parse sequence
	sequence, err := hexToUint64(logEntries[1])
	if err != nil {
		return LogParameters{}, fmt.Errorf("failed to parse sequence: %w", err)
	}
	logger.Info("Sequence", zap.Uint64("value", sequence))

	// Parse nonce
	nonce, err := hexToUint64(logEntries[2])
	if err != nil {
		return LogParameters{}, fmt.Errorf("failed to parse nonce: %w", err)
	}
	logger.Info("Nonce", zap.Uint64("value", nonce))

	// Parse consistency level
	consistencyLevel, err := hexToUint64(logEntries[3])
	if err != nil {
		return LogParameters{}, fmt.Errorf("failed to parse consistencyLevel: %w", err)
	}
	logger.Info("ConsistencyLevel", zap.Uint64("value", consistencyLevel))

	return LogParameters{
		SenderAddress:    senderAddress,
		Sequence:         sequence,
		Nonce:            uint32(nonce),
		ConsistencyLevel: uint8(consistencyLevel),
	}, nil
}

// createPayload processes log entries into a byte payload
func (w *Watcher) createPayload(logger *zap.Logger, logEntries []string) []byte {
	payload := make([]byte, 0, payloadInitialCap)

	for i, entry := range logEntries {
		hexStr := strings.TrimPrefix(entry, "0x")

		// Try to decode as hex
		bytes, err := hex.DecodeString(hexStr)
		if err != nil {
			logger.Warn("Failed to decode hex", zap.String("entry", entry), zap.Error(err))
			continue
		}

		// Add to payload
		payload = append(payload, bytes...)

		// Try to interpret as a string for logging
		w.logInterpretedValue(logger, i+4, bytes, entry)
	}

	return payload
}

// logInterpretedValue attempts to interpret bytes as string or number for logging
func (w *Watcher) logInterpretedValue(logger *zap.Logger, index int, bytes []byte, rawHex string) {
	// Trim leading null bytes
	startIndex := 0
	for startIndex < len(bytes) && bytes[startIndex] == 0 {
		startIndex++
	}
	trimmedBytes := bytes[startIndex:]

	// Check if it's a printable string
	if str := string(trimmedBytes); isPrintableString(str) {
		logger.Debug("Field as string", zap.Int("index", index), zap.String("value", str))
	} else {
		// Fall back to numeric representation
		logger.Debug("Field as number", zap.Int("index", index), zap.String("value", rawHex))
	}
}

// publishObservation creates and publishes a message observation
func (w *Watcher) publishObservation(logger *zap.Logger, params LogParameters, payload []byte, blockInfo BlockInfo) error {
	// Convert transaction hash to byte array for txID
	txID, err := hex.DecodeString(strings.TrimPrefix(blockInfo.TxHash, "0x"))
	if err != nil {
		logger.Error("Failed to decode transaction hash", zap.Error(err))
		// Fall back to default
		txID = []byte{0x0}
	}

	// Create the observation
	observation := &common.MessagePublication{
		TxID:             txID,
		Timestamp:        time.Unix(int64(blockInfo.Timestamp), 0),
		Nonce:            params.Nonce,
		Sequence:         params.Sequence,
		EmitterChain:     w.chainID,
		EmitterAddress:   params.SenderAddress,
		Payload:          payload,
		ConsistencyLevel: params.ConsistencyLevel,
		IsReobservation:  false,
	}

	// Increment metrics
	aztecMessagesConfirmed.WithLabelValues(w.networkID).Inc()

	// Log the observation
	logger.Info("Message observed",
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

// fetchPublicLogs retrieves logs for a specific block range
func (w *Watcher) fetchPublicLogs(ctx context.Context, fromBlock, toBlock int) ([]ExtendedPublicLog, error) {
	// Create log filter parameter
	logFilter := map[string]any{
		"fromBlock": fromBlock,
		"toBlock":   toBlock,
	}

	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  "node_getPublicLogs",
		"params":  []any{logFilter},
		"id":      1,
	}

	// Send the JSON-RPC request
	responseBody, err := w.sendJSONRPCRequest(ctx, payload)
	if err != nil {
		return nil, err
	}

	// Parse the response
	var response JsonRpcResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, fmt.Errorf("failed to parse logs response: %w", err)
	}

	return response.Result.Logs, nil
}

// fetchLatestBlockNumber gets the current height of the blockchain
func (w *Watcher) fetchLatestBlockNumber(ctx context.Context) (int, error) {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  "node_getBlockNumber",
		"params":  []any{},
		"id":      1,
	}

	// Send the request
	responseBody, err := w.sendJSONRPCRequest(ctx, payload)
	if err != nil {
		return 0, err
	}

	// Parse the response
	var response struct {
		Result json.RawMessage `json:"result"`
	}

	if err := json.Unmarshal(responseBody, &response); err != nil {
		return 0, fmt.Errorf("failed to parse block number response: %w", err)
	}

	return parseBlockNumber(response.Result)
}

// fetchBlockInfo gets details of a specific block
func (w *Watcher) fetchBlockInfo(ctx context.Context, blockNumber int) (BlockInfo, error) {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  "node_getBlock",
		"params":  []any{blockNumber},
		"id":      1,
	}

	// Send the request
	responseBody, err := w.sendJSONRPCRequest(ctx, payload)
	if err != nil {
		return BlockInfo{}, err
	}

	// Parse the response
	var response BlockResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return BlockInfo{}, fmt.Errorf("failed to parse block response: %w", err)
	}

	// Extract the necessary information from the block
	info := BlockInfo{}

	// Get the timestamp from global variables (remove 0x prefix and convert from hex)
	timestampHex := strings.TrimPrefix(response.Result.Header.GlobalVariables.Timestamp, "0x")
	timestamp, err := strconv.ParseUint(timestampHex, 16, 64)
	if err != nil {
		return BlockInfo{}, fmt.Errorf("failed to parse timestamp: %w", err)
	}
	info.Timestamp = timestamp

	// Get the transaction hash from the first transaction in the block (if available)
	if len(response.Result.Body.TxEffects) > 0 {
		info.TxHash = response.Result.Body.TxEffects[0].TxHash
	} else {
		// If no transactions, use the block's archive root as a fallback identifier
		info.TxHash = response.Result.Archive.Root
	}

	return info, nil
}

// sendJSONRPCRequest sends a JSON-RPC request and returns the response body
func (w *Watcher) sendJSONRPCRequest(ctx context.Context, payload map[string]any) ([]byte, error) {
	// Marshal the payload
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("error marshaling JSON: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", w.rpcURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Send the request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Read the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response: %w", err)
	}

	return body, nil
}

// parseBlockNumber handles different formats of block number in responses
func parseBlockNumber(rawMessage json.RawMessage) (int, error) {
	// Try to unmarshal as string first (hex format)
	var hexStr string
	if err := json.Unmarshal(rawMessage, &hexStr); err == nil {
		// It's a hex string like "0x123"
		parsedNum, err := strconv.ParseInt(hexStr, 0, 64)
		if err != nil {
			return 0, fmt.Errorf("failed to parse hex block number: %w", err)
		}
		return int(parsedNum), nil
	}

	// Try to unmarshal as number
	var num float64
	if err := json.Unmarshal(rawMessage, &num); err != nil {
		return 0, fmt.Errorf("block number is neither string nor number: %w", err)
	}

	return int(num), nil
}

// hexToUint64 converts a hex string to uint64
func hexToUint64(hexStr string) (uint64, error) {
	// Remove "0x" prefix if present
	hexStr = strings.TrimPrefix(hexStr, "0x")

	// Parse the hex string to uint64
	value, err := strconv.ParseUint(hexStr, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to convert hex to uint64: %s, error: %w", hexStr, err)
	}

	return value, nil
}

// isPrintableString checks if a string contains mostly printable ASCII characters
func isPrintableString(s string) bool {
	printable := 0
	for _, r := range s {
		if r >= 32 && r <= 126 {
			printable++
		}
	}
	return printable >= 3 && float64(printable)/float64(len(s)) > 0.5
}

// Struct definitions

// EthereumBlock represents key information about an Ethereum block
type EthereumBlock struct {
	Number    uint64
	Hash      string
	Timestamp uint64
}

// LogParameters encapsulates the core parameters from a log
type LogParameters struct {
	SenderAddress    vaa.Address
	Sequence         uint64
	Nonce            uint32
	ConsistencyLevel uint8
}

// Helper struct for block information
type BlockInfo struct {
	TxHash    string
	Timestamp uint64
}

// JSON-RPC related structures
type JsonRpcResponse struct {
	JsonRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Result  struct {
		Logs       []ExtendedPublicLog `json:"logs"`
		MaxLogsHit bool                `json:"maxLogsHit"`
	} `json:"result"`
}

type BlockResponse struct {
	JsonRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Result  BlockResult `json:"result"`
}

type BlockResult struct {
	Archive BlockArchive `json:"archive"`
	Header  BlockHeader  `json:"header"`
	Body    BlockBody    `json:"body"`
}

type BlockArchive struct {
	Root                   string `json:"root"`
	NextAvailableLeafIndex int    `json:"nextAvailableLeafIndex"`
}

type BlockHeader struct {
	GlobalVariables GlobalVariables `json:"globalVariables"`
}

type GlobalVariables struct {
	ChainID     string `json:"chainId"`
	Version     string `json:"version"`
	BlockNumber string `json:"blockNumber"`
	SlotNumber  string `json:"slotNumber"`
	Timestamp   string `json:"timestamp"`
	Coinbase    string `json:"coinbase"`
}

type BlockBody struct {
	TxEffects []TxEffect `json:"txEffects"`
}

type TxEffect struct {
	TxHash string `json:"txHash"`
}

type LogId struct {
	BlockNumber int `json:"blockNumber"`
	TxIndex     int `json:"txIndex"`
	LogIndex    int `json:"logIndex"`
}

type PublicLog struct {
	ContractAddress string   `json:"contractAddress"`
	Log             []string `json:"log"`
}

type ExtendedPublicLog struct {
	ID  LogId     `json:"id"`
	Log PublicLog `json:"log"`
}
