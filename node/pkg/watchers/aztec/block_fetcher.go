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
	FetchPublicLogs(ctx context.Context, fromBlock, toBlock int) ([]ExtendedPublicLog, error)
	FetchBlock(ctx context.Context, blockNumber int) (BlockInfo, error)
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
