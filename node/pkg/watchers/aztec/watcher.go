package aztec

import (
	"context"
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
	processedBlocks []*ProcessedBlock
	blocksByHash    map[string]*ProcessedBlock // Map for O(1) lookups by hash
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
		blocksByHash:       make(map[string]*ProcessedBlock), // Initialize hash map
		lastBlockNumber:    config.StartBlock,                // Will process StartBlock first
		reorgDepth:         0,
	}
}

// Run starts the watcher with a single goroutine
func (w *Watcher) Run(ctx context.Context) error {
	w.logger.Info("Starting Aztec watcher",
		zap.String("rpc", w.config.RpcURL),
		zap.String("contract", w.config.ContractAddress))

	// Create an error channel
	errC := make(chan error)
	defer close(errC)

	// Start a single goroutine that handles all operations
	common.RunWithScissors(ctx, errC, "aztec_unified_processor", func(ctx context.Context) error {
		return w.unifiedProcessor(ctx)
	})

	// Wait for context cancellation or error
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errC:
		return err
	}
}

func (w *Watcher) unifiedProcessor(ctx context.Context) error {
	// Track last execution times
	lastFinalityCheck := time.Now()
	lastBlockPrune := time.Now()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	w.logger.Info("Starting Aztec event processor with reorg handling")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// Process blocks
			if err := w.processBlocks(ctx); err != nil {
				w.logger.Error("Error processing blocks", zap.Error(err))
			}

			// Check finality
			if time.Since(lastFinalityCheck) >= w.config.FinalityCheckInterval {
				w.logger.Info("Finality check interval reached",
					zap.Duration("interval", w.config.FinalityCheckInterval))

				if err := w.processFinality(ctx); err != nil {
					w.logger.Error("Error checking finality", zap.Error(err))
				}
				lastFinalityCheck = time.Now()
			}

			// Prune blocks if needed
			if time.Since(lastBlockPrune) >= w.config.BlockPruneInterval {
				w.pruneProcessedBlocks(ctx)
				lastBlockPrune = time.Now()
			}
		}
	}
}
