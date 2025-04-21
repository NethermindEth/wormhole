package aztec

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/certusone/wormhole/node/pkg/watchers/interfaces"
	"go.uber.org/zap"
)

// L1Verifier defines the interface for verifying finality
type L1Verifier interface {
	// Include interfaces.L1Finalizer as an embedded interface
	interfaces.L1Finalizer

	// Get the latest finalized block from Aztec
	GetFinalizedBlock(ctx context.Context) (*FinalizedBlock, error)

	// Check if a block is finalized
	IsBlockFinalized(ctx context.Context, blockNumber int) (bool, error)
}

// aztecFinalityVerifier is a simplified L1Verifier that queries Aztec directly
type aztecFinalityVerifier struct {
	rpcURL string
	client HTTPClient
	logger *zap.Logger

	// Cache for finalized blocks
	finalizedBlockCache     *FinalizedBlock
	finalizedBlockCacheTime time.Time
	finalizedBlockCacheMu   sync.RWMutex
	finalizedBlockCacheTTL  time.Duration
}

// NewAztecFinalityVerifier creates a new L1 verifier
func NewAztecFinalityVerifier(
	rpcURL string,
	client HTTPClient,
	logger *zap.Logger,
) L1Verifier {
	return &aztecFinalityVerifier{
		rpcURL:                 rpcURL,
		client:                 client,
		logger:                 logger,
		finalizedBlockCacheTTL: 30 * time.Second,
	}
}

// GetLatestFinalizedBlockNumber implements the interfaces.L1Finalizer interface
func (v *aztecFinalityVerifier) GetLatestFinalizedBlockNumber() uint64 {
	// Check the cache first
	v.finalizedBlockCacheMu.RLock()
	if v.finalizedBlockCache != nil && time.Since(v.finalizedBlockCacheTime) < v.finalizedBlockCacheTTL {
		blockNum := v.finalizedBlockCache.Number
		v.finalizedBlockCacheMu.RUnlock()
		return uint64(blockNum)
	}
	v.finalizedBlockCacheMu.RUnlock()

	// If no cache, fetch the latest finalized block
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	block, err := v.GetFinalizedBlock(ctx)
	if err != nil {
		v.logger.Warn("Failed to get finalized block for L1Finalizer", zap.Error(err))
		return 0
	}

	return uint64(block.Number)
}

// GetFinalizedBlock gets the latest finalized block from Aztec
func (v *aztecFinalityVerifier) GetFinalizedBlock(ctx context.Context) (*FinalizedBlock, error) {
	// Check cache first
	v.finalizedBlockCacheMu.RLock()
	if v.finalizedBlockCache != nil && time.Since(v.finalizedBlockCacheTime) < v.finalizedBlockCacheTTL {
		block := v.finalizedBlockCache
		v.finalizedBlockCacheMu.RUnlock()
		v.logger.Debug("Using cached finalized block",
			zap.Int("number", block.Number),
			zap.String("hash", block.Hash))
		return block, nil
	}
	v.finalizedBlockCacheMu.RUnlock()

	// Cache miss, fetch from network
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  "node_getL2Tips",
		"params":  []any{},
		"id":      1,
	}

	v.logger.Debug("Fetching L2 tips")
	responseBody, err := v.client.DoRequest(ctx, v.rpcURL, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch L2 tips: %v", err)
	}

	var response L2TipsResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, &ErrParsingFailed{
			What: "L2 tips response",
			Err:  err,
		}
	}

	// Create finalized block info
	block := &FinalizedBlock{
		Number: response.Result.Finalized.Number,
		Hash:   response.Result.Finalized.Hash,
	}

	// Update the cache
	v.finalizedBlockCacheMu.Lock()
	v.finalizedBlockCache = block
	v.finalizedBlockCacheTime = time.Now()
	v.finalizedBlockCacheMu.Unlock()

	v.logger.Info("Updated finalized block",
		zap.Int("number", block.Number))

	return block, nil
}

// IsBlockFinalized checks if a specific block number is finalized
func (v *aztecFinalityVerifier) IsBlockFinalized(ctx context.Context, blockNumber int) (bool, error) {
	finalizedBlock, err := v.GetFinalizedBlock(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to get finalized block: %v", err)
	}

	isFinalized := blockNumber <= finalizedBlock.Number
	v.logger.Debug("Block finality check",
		zap.Int("block", blockNumber),
		zap.Int("finalized_block", finalizedBlock.Number),
		zap.Bool("is_finalized", isFinalized))

	return isFinalized, nil
}
