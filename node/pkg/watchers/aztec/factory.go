package aztec

import (
	"context"
	"time"

	"github.com/certusone/wormhole/node/pkg/common"
	gossipv1 "github.com/certusone/wormhole/node/pkg/proto/gossip/v1"
	"github.com/certusone/wormhole/node/pkg/readiness"
	"github.com/certusone/wormhole/node/pkg/supervisor"
	"github.com/certusone/wormhole/node/pkg/watchers/interfaces"
	"github.com/wormhole-foundation/wormhole/sdk/vaa"
	"go.uber.org/zap"
)

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

		// Create the watcher with simplified structure
		watcher := NewWatcher(
			config,
			blockFetcher,
			l1Verifier,
			observationManager,
			msgC,
			logger,
		)

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
