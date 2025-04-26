package aztec

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// BlockFetcher defines the interface for retrieving Aztec chain data
type BlockFetcher interface {
	FetchLatestBlockNumber(ctx context.Context) (int, error)
	FetchPublicLogs(ctx context.Context, fromBlock, toBlock int) ([]ExtendedPublicLog, error)
	FetchBlock(ctx context.Context, blockNumber int) (BlockInfo, error)
	FetchBlockDetails(ctx context.Context, blockNumber int) (*BlockArchive, error)
	FetchBlocks(ctx context.Context, from int, limit int) ([]BlockInfo, error)
}

// aztecBlockFetcher is the implementation of BlockFetcher
type aztecBlockFetcher struct {
	rpcURL string
	client HTTPClient
	logger *zap.Logger
}

// NewAztecBlockFetcher creates a new block fetcher
func NewAztecBlockFetcher(rpcURL string, client HTTPClient, logger *zap.Logger) BlockFetcher {
	return &aztecBlockFetcher{
		rpcURL: rpcURL,
		client: client,
		logger: logger,
	}
}

// FetchLatestBlockNumber gets the latest block number from the Aztec chain
func (f *aztecBlockFetcher) FetchLatestBlockNumber(ctx context.Context) (int, error) {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  "node_getBlockNumber",
		"params":  []any{},
		"id":      1,
	}

	// Removed debug log for fetching

	responseBody, err := f.client.DoRequest(ctx, f.rpcURL, payload)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch latest block number: %v", err)
	}

	// Parse the response
	var response struct {
		Result json.RawMessage `json:"result"`
	}

	if err := json.Unmarshal(responseBody, &response); err != nil {
		return 0, &ErrParsingFailed{
			What: "block number response",
			Err:  err,
		}
	}

	return f.parseBlockNumber(response.Result)
}

// FetchPublicLogs gets logs for a specific block range
func (f *aztecBlockFetcher) FetchPublicLogs(ctx context.Context, fromBlock, toBlock int) ([]ExtendedPublicLog, error) {
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

	f.logger.Debug("Fetching logs",
		zap.Int("fromBlock", fromBlock),
		zap.Int("toBlock", toBlock))

	responseBody, err := f.client.DoRequest(ctx, f.rpcURL, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch public logs: %v", err)
	}

	// Parse the response
	var response JsonRpcResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, &ErrParsingFailed{
			What: "logs response",
			Err:  err,
		}
	}

	return response.Result.Logs, nil
}

// FetchBlock gets info for a specific block
func (f *aztecBlockFetcher) FetchBlock(ctx context.Context, blockNumber int) (BlockInfo, error) {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  "node_getBlock",
		"params":  []any{blockNumber},
		"id":      1,
	}

	responseBody, err := f.client.DoRequest(ctx, f.rpcURL, payload)
	if err != nil {
		return BlockInfo{}, fmt.Errorf("failed to fetch block info: %v", err)
	}

	// Parse the response
	var response BlockResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return BlockInfo{}, &ErrParsingFailed{
			What: "block response",
			Err:  err,
		}
	}

	info := BlockInfo{}

	// Set the block hash using the archive root
	info.BlockHash = response.Result.Archive.Root

	// Set the parent hash using lastArchive.root
	info.ParentHash = response.Result.Header.LastArchive.Root

	// Get the timestamp from global variables (remove 0x prefix and convert from hex)
	timestampHex := strings.TrimPrefix(response.Result.Header.GlobalVariables.Timestamp, "0x")
	if timestampHex == "" {
		// Handle empty timestamp (typically for genesis block)
		if blockNumber == 0 {
			// Use a default timestamp for genesis block
			info.Timestamp = 0 // Or any appropriate value
			f.logger.Debug("Genesis block has no timestamp, using default value")
		} else {
			// Use current time as fallback for non-genesis blocks
			info.Timestamp = uint64(time.Now().Unix())
			f.logger.Warn("Block has empty timestamp, using current time",
				zap.Int("blockNumber", blockNumber))
		}
	} else {
		// Parse the timestamp normally
		timestamp, err := strconv.ParseUint(timestampHex, 16, 64)
		if err != nil {
			return BlockInfo{}, &ErrParsingFailed{
				What: "timestamp",
				Err:  err,
			}
		}
		info.Timestamp = timestamp
	}

	// svlachakis check if remove - this is used in wormhole we can't remove it.
	// Get the transaction hash from the first transaction in the block (if available)
	if len(response.Result.Body.TxEffects) > 0 {
		info.TxHash = response.Result.Body.TxEffects[0].TxHash
	} else {
		// If no transactions, use a placeholder
		info.TxHash = "0x0"
	}

	// Log the block hash and parent hash for debugging
	f.logger.Debug("Fetched block info",
		zap.Int("blockNumber", blockNumber),
		zap.String("blockHash", info.BlockHash),
		zap.String("parentHash", info.ParentHash))

	return info, nil
}

// FetchBlockDetails gets detailed information about an Aztec block
func (f *aztecBlockFetcher) FetchBlockDetails(ctx context.Context, blockNumber int) (*BlockArchive, error) {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  "node_getBlock",
		"params":  []any{blockNumber},
		"id":      1,
	}

	// Removed debug log for fetching block details

	responseBody, err := f.client.DoRequest(ctx, f.rpcURL, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch block details: %v", err)
	}

	// Parse the response
	var response BlockResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, &ErrParsingFailed{
			What: "block response",
			Err:  err,
		}
	}

	return &response.Result.Archive, nil
}

// FetchBlocks gets info for multiple blocks in a given range
func (f *aztecBlockFetcher) FetchBlocks(ctx context.Context, startBlock int, limit int) ([]BlockInfo, error) {
	if startBlock == 0 {
		startBlock = 1
	}
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  "node_getBlocks",
		"params":  []any{startBlock, limit},
		"id":      1,
	}

	responseBody, err := f.client.DoRequest(ctx, f.rpcURL, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch blocks: %v", err)
	}

	// Parse the response
	var response struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Result  []struct {
			Archive struct {
				Root string `json:"root"`
			} `json:"archive"`
			Header struct {
				LastArchive struct {
					Root string `json:"root"`
				} `json:"lastArchive"`
				GlobalVariables struct {
					Timestamp string `json:"timestamp"`
				} `json:"globalVariables"`
			} `json:"header"`
			Body struct {
				TxEffects []struct {
					TxHash string `json:"txHash"`
				} `json:"txEffects"`
			} `json:"body"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}

	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, &ErrParsingFailed{
			What: "blocks response",
			Err:  err,
		}
	}

	// Check for RPC error
	if response.Error != nil {
		return nil, fmt.Errorf("RPC error: %s (code: %d)", response.Error.Message, response.Error.Code)
	}

	// Process each block
	blockInfos := make([]BlockInfo, 0, len(response.Result))

	for i, block := range response.Result {
		info := BlockInfo{}

		// Set the block hash using the archive root
		info.BlockHash = block.Archive.Root

		// Set the parent hash using lastArchive.root
		info.ParentHash = block.Header.LastArchive.Root

		// Get the timestamp from global variables (remove 0x prefix and convert from hex)
		timestampHex := strings.TrimPrefix(block.Header.GlobalVariables.Timestamp, "0x")
		if timestampHex == "" {
			// Handle empty timestamp (typically for genesis block)
			if startBlock+i == 0 {
				// Use a default timestamp for genesis block
				info.Timestamp = 0 // Or any appropriate value
				f.logger.Debug("Genesis block has no timestamp, using default value")
			} else {
				// Use current time as fallback for non-genesis blocks
				info.Timestamp = uint64(time.Now().Unix())
				f.logger.Warn("Block has empty timestamp, using current time",
					zap.Int("blockNumber", startBlock+i))
			}
		} else {
			// Parse the timestamp normally
			timestamp, err := strconv.ParseUint(timestampHex, 16, 64)
			if err != nil {
				return nil, &ErrParsingFailed{
					What: "timestamp",
					Err:  err,
				}
			}
			info.Timestamp = timestamp
		}

		// Get the transaction hash from the first transaction in the block (if available)
		if len(block.Body.TxEffects) > 0 {
			info.TxHash = block.Body.TxEffects[0].TxHash
		} else {
			// If no transactions, use a placeholder
			info.TxHash = "0x0"
		}

		// Log the block hash and parent hash for debugging
		f.logger.Debug("Fetched block info",
			zap.Int("blockNumber", startBlock+i),
			zap.String("blockHash", info.BlockHash),
			zap.String("parentHash", info.ParentHash))

		blockInfos = append(blockInfos, info)
	}

	f.logger.Debug("Completed block fetch",
		zap.Int("startBlock", startBlock),
		zap.Int("limit", limit),
		zap.Int("fetchedCount", len(blockInfos)))

	return blockInfos, nil
}

// parseBlockNumber handles different formats of block number in responses
func (f *aztecBlockFetcher) parseBlockNumber(rawMessage json.RawMessage) (int, error) {
	// Try to unmarshal as string first (hex format)
	var hexStr string
	if err := json.Unmarshal(rawMessage, &hexStr); err == nil {
		// It's a hex string like "0x123"
		parsedNum, err := strconv.ParseInt(hexStr, 0, 64)
		if err != nil {
			return 0, &ErrParsingFailed{
				What: "hex block number",
				Err:  err,
			}
		}
		return int(parsedNum), nil
	}

	// Try to unmarshal as number
	var num float64
	if err := json.Unmarshal(rawMessage, &num); err != nil {
		return 0, &ErrParsingFailed{
			What: "block number format",
			Err:  err,
		}
	}

	return int(num), nil
}
