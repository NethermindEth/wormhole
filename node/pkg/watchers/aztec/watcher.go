package aztec

import (
	"context"
	"sync"
	"time"

	"github.com/certusone/wormhole/node/pkg/common"
	"go.uber.org/zap"
)

// Watcher handles the processing of blocks and logs
type Watcher struct {
	config             Config
	blockFetcher       BlockFetcher
	l1Verifier         L1Verifier
	observationManager ObservationManager
	msgC               chan<- *common.MessagePublication
	logger             *zap.Logger

	// Chain tracking for reorg detection
	mu              sync.Mutex
	processedBlocks []*ProcessedBlock
	lastBlockNumber int
	reorgDepth      int // Track deepest reorg for metrics
}

// ProcessedBlock stores information about a processed block
type ProcessedBlock struct {
	Number       int
	Hash         string
	ParentHash   string
	Observations []*ObservationRecord
	Timestamp    uint64
	IsCanonical  bool
}

// ObservationRecord represents a message that has been observed and possibly published
type ObservationRecord struct {
	ID               string
	BlockNumber      int
	LogParameters    LogParameters
	Payload          []byte
	BlockInfo        BlockInfo
	IsPublished      bool
	PublishedTime    time.Time
	IsInvalidated    bool // Set to true if this observation was invalidated by a reorg
	InvalidationTime time.Time
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
		logger:             logger,
		processedBlocks:    make([]*ProcessedBlock, 0),
		lastBlockNumber:    config.StartBlock - 1, // Will process StartBlock first
		reorgDepth:         0,
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

	// Start periodic block pruning goroutine
	common.RunWithScissors(ctx, errC, "aztec_block_pruner", func(ctx context.Context) error {
		return w.runBlockPruner(ctx)
	})

	// Wait for context cancellation or error
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errC:
		return err
	}
}

// GetProcessedBlockByNumber returns a processed block by its number
func (w *Watcher) GetProcessedBlockByNumber(blockNumber int) *ProcessedBlock {
	w.mu.Lock()
	defer w.mu.Unlock()

	for i := len(w.processedBlocks) - 1; i >= 0; i-- {
		if w.processedBlocks[i].Number == blockNumber {
			return w.processedBlocks[i]
		}
	}
	return nil
}

// GetLatestCanonicalBlock returns the latest block in the canonical chain
func (w *Watcher) GetLatestCanonicalBlock() *ProcessedBlock {
	w.mu.Lock()
	defer w.mu.Unlock()

	for i := len(w.processedBlocks) - 1; i >= 0; i-- {
		if w.processedBlocks[i].IsCanonical {
			return w.processedBlocks[i]
		}
	}
	return nil
}
