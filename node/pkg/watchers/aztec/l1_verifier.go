package aztec

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/certusone/wormhole/node/pkg/watchers/interfaces"
	"go.uber.org/zap"
)

// L1Verifier defines the interface for Ethereum L1 verification operations
// It extends interfaces.L1Finalizer to provide compatibility with the watcher framework
type L1Verifier interface {
	// Include interfaces.L1Finalizer as an embedded interface
	interfaces.L1Finalizer

	// Additional methods specific to our L1Verifier
	IsAvailable(ctx context.Context) bool
	FetchFinalizedBlock(ctx context.Context) (*EthereumBlock, error)
	GetL1InclusionBlock(ctx context.Context, blockHash string) (uint64, error)
	CheckRollupContractExists(ctx context.Context) bool
}

// GetLatestFinalizedBlockNumber implements the interfaces.L1Finalizer interface
// by returning the latest finalized block number from the cache or a conservative estimate
func (v *ethereumVerifier) GetLatestFinalizedBlockNumber() uint64 {
	// Check the cache first
	v.finalizedBlockCacheMu.RLock()
	if v.finalizedBlockCache != nil && time.Since(v.finalizedBlockCacheTime) < v.finalizedBlockCacheTTL {
		blockNum := v.finalizedBlockCache.Number
		v.finalizedBlockCacheMu.RUnlock()
		return blockNum
	}
	v.finalizedBlockCacheMu.RUnlock()

	// If we don't have a cached value, return 0 (the most conservative)
	// This will only happen until the first successful finalized block fetch
	v.logger.Debug("No cached finalized block available, returning 0")
	return 0
}

// ethereumVerifier is the implementation of L1Verifier
type ethereumVerifier struct {
	ethRpcURL             string
	rollupContractAddress string
	client                HTTPClient
	logger                *zap.Logger

	// Cache for finalized blocks
	finalizedBlockCache     *EthereumBlock
	finalizedBlockCacheTime time.Time
	finalizedBlockCacheMu   sync.RWMutex
	finalizedBlockCacheTTL  time.Duration

	// Health tracking
	lastAvailabilityCheck      time.Time
	lastAvailabilityResult     bool
	lastAvailabilityCheckMutex sync.RWMutex
	availabilityCheckInterval  time.Duration
}

// NewEthereumVerifier creates a new L1 verifier
func NewEthereumVerifier(
	ethRpcURL string,
	rollupContractAddress string,
	client HTTPClient,
	logger *zap.Logger,
) L1Verifier {
	return &ethereumVerifier{
		ethRpcURL:                 ethRpcURL,
		rollupContractAddress:     rollupContractAddress,
		client:                    client,
		logger:                    logger,
		finalizedBlockCacheTTL:    30 * time.Second,
		availabilityCheckInterval: 1 * time.Minute,
	}
}

// IsAvailable checks if the Ethereum RPC endpoint is responsive
func (v *ethereumVerifier) IsAvailable(ctx context.Context) bool {
	// Check if we have a recent result
	v.lastAvailabilityCheckMutex.RLock()
	if time.Since(v.lastAvailabilityCheck) < v.availabilityCheckInterval {
		result := v.lastAvailabilityResult
		v.lastAvailabilityCheckMutex.RUnlock()
		return result
	}
	v.lastAvailabilityCheckMutex.RUnlock()

	// Need to do a fresh check
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  "net_version",
		"params":  []any{},
		"id":      1,
	}

	v.logger.Debug("Checking Ethereum RPC availability")
	responseBody, err := v.client.DoRequest(ctx, v.ethRpcURL, payload)
	if err != nil {
		v.logger.Warn("Ethereum RPC is not available",
			zap.String("url", v.ethRpcURL),
			zap.Error(err))

		// Update the cache
		v.lastAvailabilityCheckMutex.Lock()
		v.lastAvailabilityCheck = time.Now()
		v.lastAvailabilityResult = false
		v.lastAvailabilityCheckMutex.Unlock()

		return false
	}

	var response struct {
		Result string `json:"result"`
	}

	if err := json.Unmarshal(responseBody, &response); err != nil {
		v.logger.Warn("Failed to parse Ethereum RPC response", zap.Error(err))

		// Update the cache
		v.lastAvailabilityCheckMutex.Lock()
		v.lastAvailabilityCheck = time.Now()
		v.lastAvailabilityResult = false
		v.lastAvailabilityCheckMutex.Unlock()

		return false
	}

	v.logger.Info("Ethereum RPC is available",
		zap.String("network_id", response.Result))

	// Update the cache
	v.lastAvailabilityCheckMutex.Lock()
	v.lastAvailabilityCheck = time.Now()
	v.lastAvailabilityResult = true
	v.lastAvailabilityCheckMutex.Unlock()

	return true
}

// FetchFinalizedBlock gets the latest finalized block from Ethereum
func (v *ethereumVerifier) FetchFinalizedBlock(ctx context.Context) (*EthereumBlock, error) {
	// Check cache first
	v.finalizedBlockCacheMu.RLock()
	if v.finalizedBlockCache != nil && time.Since(v.finalizedBlockCacheTime) < v.finalizedBlockCacheTTL {
		block := v.finalizedBlockCache
		v.finalizedBlockCacheMu.RUnlock()
		return block, nil
	}
	v.finalizedBlockCacheMu.RUnlock()

	// Cache miss, fetch from network
	var block *EthereumBlock
	var err error

	// First try with "finalized" tag which is supported post-merge
	block, err = v.fetchEthereumBlock(ctx, "finalized")
	if err != nil {
		v.logger.Debug("Failed to fetch finalized block, trying safe", zap.Error(err))

		// If that fails, try "safe" which is another post-merge tag
		block, err = v.fetchEthereumBlock(ctx, "safe")
		if err != nil {
			v.logger.Debug("Failed to fetch safe block, trying latest with offset", zap.Error(err))

			// If both post-merge tags fail, fall back to "latest" and apply a safety offset
			// for test environments or pre-merge clients
			latestBlock, err := v.fetchEthereumBlock(ctx, "latest")
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
			block, err = v.fetchEthereumBlock(ctx, blockNumberHex)
			if err != nil {
				return nil, fmt.Errorf("failed to fetch block at number %s: %w", blockNumberHex, err)
			}
		}
	}

	// Update the cache
	v.finalizedBlockCacheMu.Lock()
	v.finalizedBlockCache = block
	v.finalizedBlockCacheTime = time.Now()
	v.finalizedBlockCacheMu.Unlock()

	return block, nil
}

// GetL1InclusionBlock finds the Ethereum L1 block that includes the Aztec L2 block
func (v *ethereumVerifier) GetL1InclusionBlock(ctx context.Context, blockHash string) (uint64, error) {
	// Log the block hash we're searching for in a more visible format
	v.logger.Info("🔍 SEARCHING FOR AZTEC BLOCK IN ETHEREUM CHAIN",
		zap.String("aztec_block_hash", blockHash))

	// First, we need to create a filter to get relevant events
	from, to, err := v.getSuitableBlockRange(ctx)
	if err != nil {
		v.logger.Error("❌ Failed to get block range for Ethereum search", zap.Error(err))
		return 0, fmt.Errorf("failed to get block range: %w", err)
	}

	v.logger.Info("🔍 Searching Ethereum blocks for Aztec inclusion",
		zap.String("aztec_block_hash", blockHash),
		zap.Uint64("from_block", from),
		zap.Uint64("to_block", to),
		zap.String("rollup_contract", v.rollupContractAddress))

	// Prepare the filter for the block commitment events
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  "eth_getLogs",
		"params": []any{map[string]any{
			"fromBlock": fmt.Sprintf("0x%x", from),
			"toBlock":   fmt.Sprintf("0x%x", to),
			"address":   v.rollupContractAddress,
		}},
		"id": 1,
	}

	// Log the actual JSON payload for debugging
	payloadJson, _ := json.Marshal(payload)
	v.logger.Debug("🔍 eth_getLogs payload", zap.String("payload", string(payloadJson)))

	// Send the request
	responseBody, err := v.client.DoRequest(ctx, v.ethRpcURL, payload)
	if err != nil {
		v.logger.Error("❌ Failed to get logs from Ethereum", zap.Error(err))
		return 0, fmt.Errorf("failed to get logs: %w", err)
	}

	// Debug the raw response
	v.logger.Debug("🔍 Raw eth_getLogs response",
		zap.String("response", string(responseBody)))

	// Check if the response contains an error
	hasError, rpcError := GetJSONRPCError(responseBody)
	if hasError {
		v.logger.Error("❌ eth_getLogs returned an error",
			zap.Int("code", rpcError.Code),
			zap.String("message", rpcError.Msg))
		return 0, rpcError
	}

	// Parse the response to find the event
	var logsResponse struct {
		Result []struct {
			BlockNumber string   `json:"blockNumber"`
			Data        string   `json:"data"`
			Topics      []string `json:"topics"`
		} `json:"result"`
	}

	if err := json.Unmarshal(responseBody, &logsResponse); err != nil {
		v.logger.Error("❌ Failed to parse logs response", zap.Error(err))
		return 0, &ErrParsingFailed{
			What: "logs response",
			Err:  err,
		}
	}

	v.logger.Info("🔍 Received logs from rollup contract",
		zap.Int("log_count", len(logsResponse.Result)),
		zap.String("contract", v.rollupContractAddress))

	// For debugging, log all logs we received
	for i, log := range logsResponse.Result {
		v.logger.Debug("🔍 Log entry",
			zap.Int("index", i),
			zap.String("block_number", log.BlockNumber),
			zap.Strings("topics", log.Topics),
			zap.String("data", log.Data))
	}

	// Detailed logging about the hash we're looking for
	v.logger.Info("🔍 Looking for block hash in logs",
		zap.String("aztec_block_hash", blockHash),
		zap.String("aztec_block_hash_no_prefix", strings.TrimPrefix(blockHash, "0x")))

	// Look through the logs for our block hash
	for _, log := range logsResponse.Result {
		// Enhanced logging for each log we're checking
		topicsStr := strings.Join(log.Topics, ", ")
		v.logger.Debug("🔍 Checking log for block hash",
			zap.String("block_number", log.BlockNumber),
			zap.String("topics", topicsStr))

		// Check if this log contains our block hash
		containsHash, matchDetails := v.containsBlockHashDetailed(log.Data, log.Topics, blockHash)
		if containsHash {
			// Found the matching event, get the block number
			blockNum, err := ParseHexUint64(log.BlockNumber)
			if err != nil {
				v.logger.Error("❌ Failed to parse block number", zap.Error(err))
				return 0, &ErrParsingFailed{
					What: "block number",
					Err:  err,
				}
			}

			v.logger.Info("✅ FOUND L1 inclusion for Aztec block",
				zap.String("aztec_block_hash", blockHash),
				zap.Uint64("l1_block_number", blockNum),
				zap.String("match_location", matchDetails))

			return blockNum, nil
		}
	}

	// If we get here, we haven't found the block inclusion yet
	v.logger.Info("⏳ Aztec block not yet included in L1",
		zap.String("aztec_block_hash", blockHash),
		zap.Int("logs_checked", len(logsResponse.Result)))

	// IMPORTANT: Check if the rollup contract address is correct
	v.logger.Warn("⚠️ Possible issue: Check if rollup contract address is correct",
		zap.String("current_contract_address", v.rollupContractAddress),
		zap.String("suggestion", "Verify this matches the actual Aztec rollup contract"))

	return 0, &ErrBlockNotIncluded{
		BlockNumber: -1, // We don't know the actual block number
	}
}

// CheckRollupContractExists verifies the rollup contract exists on Ethereum
func (v *ethereumVerifier) CheckRollupContractExists(ctx context.Context) bool {
	// Create a payload to check the contract code
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  "eth_getCode",
		"params":  []any{v.rollupContractAddress, "latest"},
		"id":      1,
	}

	v.logger.Debug("Checking if rollup contract exists",
		zap.String("contract", v.rollupContractAddress))

	responseBody, err := v.client.DoRequest(ctx, v.ethRpcURL, payload)
	if err != nil {
		v.logger.Error("❌ Failed to check rollup contract",
			zap.String("contract", v.rollupContractAddress),
			zap.Error(err))
		return false
	}

	// Parse the response
	var response struct {
		Result string `json:"result"`
	}

	if err := json.Unmarshal(responseBody, &response); err != nil {
		v.logger.Error("❌ Failed to parse eth_getCode response",
			zap.Error(err))
		return false
	}

	// Check if the contract exists (has code)
	if response.Result == "0x" || response.Result == "" {
		v.logger.Error("❌ ROLLUP CONTRACT NOT FOUND - NO CODE AT ADDRESS",
			zap.String("contract_address", v.rollupContractAddress))
		return false
	} else {
		codeLength := len(response.Result) - 2 // subtract "0x"
		v.logger.Info("✅ Rollup contract exists",
			zap.String("contract_address", v.rollupContractAddress),
			zap.Int("code_length_hex_chars", codeLength))
		return true
	}
}

// Helper methods

// fetchEthereumBlock gets a block from Ethereum by tag or number
func (v *ethereumVerifier) fetchEthereumBlock(ctx context.Context, blockTag string) (*EthereumBlock, error) {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  "eth_getBlockByNumber",
		"params":  []any{blockTag, true},
		"id":      1,
	}

	v.logger.Debug("Fetching Ethereum block", zap.String("blockTag", blockTag))
	responseBody, err := v.client.DoRequest(ctx, v.ethRpcURL, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Ethereum block: %w", err)
	}

	var response struct {
		Result struct {
			Number    string `json:"number"`
			Hash      string `json:"hash"`
			Timestamp string `json:"timestamp"`
		} `json:"result"`
	}

	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, &ErrParsingFailed{
			What: "Ethereum block response",
			Err:  err,
		}
	}

	// Parse the block number
	blockNumber, err := ParseHexUint64(response.Result.Number)
	if err != nil {
		return nil, &ErrParsingFailed{
			What: "block number",
			Err:  err,
		}
	}

	// Parse the timestamp
	timestamp, err := ParseHexUint64(response.Result.Timestamp)
	if err != nil {
		return nil, &ErrParsingFailed{
			What: "timestamp",
			Err:  err,
		}
	}

	return &EthereumBlock{
		Number:    blockNumber,
		Hash:      response.Result.Hash,
		Timestamp: timestamp,
	}, nil
}

// getSuitableBlockRange returns a reasonable block range to search for L1 inclusions
func (v *ethereumVerifier) getSuitableBlockRange(ctx context.Context) (uint64, uint64, error) {
	// Get the latest Ethereum block
	latest, err := v.fetchEthereumBlock(ctx, "latest")
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

// containsBlockHashDetailed checks if data or topics contain the block hash
// and returns details on where it was found
func (v *ethereumVerifier) containsBlockHashDetailed(data string, topics []string, blockHash string) (bool, string) {
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
