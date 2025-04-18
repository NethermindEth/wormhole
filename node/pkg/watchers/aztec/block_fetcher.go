package aztec

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

// BlockFetcher defines the interface for retrieving Aztec chain data
type BlockFetcher interface {
	FetchLatestBlockNumber(ctx context.Context) (int, error)
	FetchPublicLogs(ctx context.Context, fromBlock, toBlock int) ([]ExtendedPublicLog, error)
	FetchBlockInfo(ctx context.Context, blockNumber int) (BlockInfo, error)
	FetchBlockDetails(ctx context.Context, blockNumber int) (*BlockArchive, error)
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

	f.logger.Debug("Fetching latest block number")
	responseBody, err := f.client.DoRequest(ctx, f.rpcURL, payload)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch latest block number: %w", err)
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

	f.logger.Debug("Fetching public logs",
		zap.Int("fromBlock", fromBlock),
		zap.Int("toBlock", toBlock))

	responseBody, err := f.client.DoRequest(ctx, f.rpcURL, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch public logs: %w", err)
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

// FetchBlockInfo gets info for a specific block
func (f *aztecBlockFetcher) FetchBlockInfo(ctx context.Context, blockNumber int) (BlockInfo, error) {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  "node_getBlock",
		"params":  []any{blockNumber},
		"id":      1,
	}

	f.logger.Debug("Fetching block info", zap.Int("blockNumber", blockNumber))
	responseBody, err := f.client.DoRequest(ctx, f.rpcURL, payload)
	if err != nil {
		return BlockInfo{}, fmt.Errorf("failed to fetch block info: %w", err)
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

	// Get the timestamp from global variables (remove 0x prefix and convert from hex)
	timestampHex := strings.TrimPrefix(response.Result.Header.GlobalVariables.Timestamp, "0x")
	timestamp, err := strconv.ParseUint(timestampHex, 16, 64)
	if err != nil {
		return BlockInfo{}, &ErrParsingFailed{
			What: "timestamp",
			Err:  err,
		}
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

// FetchBlockDetails gets detailed information about an Aztec block
func (f *aztecBlockFetcher) FetchBlockDetails(ctx context.Context, blockNumber int) (*BlockArchive, error) {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  "node_getBlock",
		"params":  []any{blockNumber},
		"id":      1,
	}

	f.logger.Debug("Fetching block details", zap.Int("blockNumber", blockNumber))
	responseBody, err := f.client.DoRequest(ctx, f.rpcURL, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch block details: %w", err)
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
