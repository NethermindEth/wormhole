package aztec

import (
	"context"
	"time"

	"github.com/certusone/wormhole/node/pkg/common"
	gossipv1 "github.com/certusone/wormhole/node/pkg/proto/gossip/v1"
	"github.com/certusone/wormhole/node/pkg/query"
	"github.com/certusone/wormhole/node/pkg/readiness"
	"github.com/certusone/wormhole/node/pkg/supervisor"
	"github.com/certusone/wormhole/node/pkg/watchers"
	"github.com/certusone/wormhole/node/pkg/watchers/interfaces"
	"github.com/wormhole-foundation/wormhole/sdk/vaa"
	"go.uber.org/zap"
)

// GetChainID implements the watchers.WatcherConfig interface
func (c *WatcherConfig) GetChainID() vaa.ChainID {
	return c.ChainID
}

// GetNetworkID implements the watchers.WatcherConfig interface
func (c *WatcherConfig) GetNetworkID() watchers.NetworkID {
	return c.NetworkID
}

// RequiredL1Finalizer implements the watchers.WatcherConfig interface
// Return an empty network ID since Aztec handles its own L1/L2 finality checks
func (c *WatcherConfig) RequiredL1Finalizer() watchers.NetworkID {
	return ""
}

// SetL1Finalizer implements the watchers.WatcherConfig interface
// This is a no-op for Aztec since we use our own internal L1Verifier instead
func (c *WatcherConfig) SetL1Finalizer(l1finalizer interfaces.L1Finalizer) {
	// No-op: we use our own internal L1Verifier/L1Finalizer
}

// Create implements the watchers.WatcherConfig interface with the updated signature
func (c *WatcherConfig) Create(
	msgC chan<- *common.MessagePublication,
	obsvReqC <-chan *gossipv1.ObservationRequest,
	queryReqC <-chan *query.PerChainQueryInternal,
	queryRespC chan<- *query.PerChainQueryResponseInternal,
	gst chan<- *common.GuardianSet,
	env common.Environment,
) (interfaces.L1Finalizer, supervisor.Runnable, interfaces.Reobserver, error) {
	// Create the runnable and L1Finalizer
	l1Finalizer, runnable := NewWatcherFromConfig(c.ChainID, string(c.NetworkID), c.Rpc, c.Contract, msgC, obsvReqC)

	// Return the L1Verifier as an L1Finalizer along with the runnable
	// This makes it available to the framework if needed
	return l1Finalizer, runnable, nil, nil
}

// WatcherFactory creates and initializes a new Aztec watcher
type WatcherFactory struct {
	// Configuration values passed in from the main application
	NetworkID string
	ChainID   vaa.ChainID
}

// NewWatcherFromConfig creates a new Aztec watcher from config values
func NewWatcherFromConfig(
	chainID vaa.ChainID,
	networkID string,
	rpcURL string,
	contractAddress string,
	msgC chan<- *common.MessagePublication,
	obsvReqC <-chan *gossipv1.ObservationRequest,
) (interfaces.L1Finalizer, supervisor.Runnable) {
	// Create a shared HTTPClient
	httpClient := NewHTTPClient(
		10*time.Second,         // Default timeout
		3,                      // Default max retries
		500*time.Millisecond,   // Default initial backoff
		1.5,                    // Default backoff multiplier
		zap.L().Named("aztec"), // Default logger until context provides one
	)

	// Create a shared L1Verifier instance
	l1Verifier := NewAztecFinalityVerifier(
		rpcURL, // We use the Aztec RPC URL
		httpClient,
		zap.L().Named("aztec.finality"),
	)

	// Create a runnable that uses the L1Verifier
	runnable := supervisor.Runnable(func(ctx context.Context) error {
		logger := supervisor.Logger(ctx)
		logger.Info("Starting Aztec watcher",
			zap.String("rpc", rpcURL),
			zap.String("contract", contractAddress))

		// Create the readiness component
		readinessSync := common.MustConvertChainIdToReadinessSyncing(chainID)

		// Create default config
		config := DefaultConfig(chainID, networkID, rpcURL, contractAddress)

		// Create a new HTTPClient with context-provided logger
		httpClient := NewHTTPClient(
			config.RequestTimeout,
			config.MaxRetries,
			config.InitialBackoff,
			config.BackoffMultiplier,
			logger,
		)

		// Create the components with context-provided logger
		blockFetcher := NewAztecBlockFetcher(rpcURL, httpClient, logger)

		// Use the existing L1Verifier but update its logger
		// We can't create a new one because we need to return the original instance
		if l1v, ok := l1Verifier.(*aztecFinalityVerifier); ok {
			l1v.logger = logger.Named("aztec_finality")
		}

		observationManager := NewObservationManager(networkID, logger)

		// Create the watcher
		watcher := &Watcher{
			config:             config,
			blockFetcher:       blockFetcher,
			l1Verifier:         l1Verifier,
			observationManager: observationManager,
			msgC:               msgC,
			logger:             logger,

			// New fields for reorg handling
			processedBlocks: make([]*ProcessedBlock, 0),
			lastBlockNumber: config.StartBlock - 1, // Will process StartBlock first
			reorgDepth:      0,
		}

		// Signal initialization complete
		readiness.SetReady(readinessSync)
		supervisor.Signal(ctx, supervisor.SignalHealthy)

		// Run the watcher
		return watcher.Run(ctx)
	})

	return l1Verifier, runnable
}

// Create a factory instance that can be used in the main application
func NewAztecWatcherFactory(networkID string, chainID vaa.ChainID) *WatcherFactory {
	return &WatcherFactory{
		NetworkID: networkID,
		ChainID:   chainID,
	}
}
