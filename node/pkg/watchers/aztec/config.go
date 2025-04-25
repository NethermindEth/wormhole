package aztec

import (
	"time"

	"github.com/wormhole-foundation/wormhole/sdk/vaa"
)

// Config holds all configuration for the Aztec watcher
type Config struct {
	// Chain identification
	ChainID   vaa.ChainID
	NetworkID string

	// Connection details
	RpcURL          string
	ContractAddress string

	// Processing parameters
	StartBlock        int
	PayloadInitialCap int

	// Timeouts and intervals
	RPCTimeout            time.Duration
	LogProcessingInterval time.Duration
	FinalityCheckInterval time.Duration
	RequestTimeout        time.Duration
	BlockPruneInterval    time.Duration // How often to prune old blocks

	// Retry configuration
	MaxRetries        int
	InitialBackoff    time.Duration
	BackoffMultiplier float64
}

// DefaultConfig returns a default configuration
func DefaultConfig(chainID vaa.ChainID, networkID string, rpcURL, contractAddress string) Config {
	return Config{
		// Chain identification
		ChainID:   chainID,
		NetworkID: networkID,

		// Connection details
		RpcURL:          rpcURL,
		ContractAddress: contractAddress,

		// Processing parameters
		StartBlock:        0,
		PayloadInitialCap: 13,

		// Timeouts and intervals
		RPCTimeout:            30 * time.Second,
		LogProcessingInterval: 1 * time.Second,
		FinalityCheckInterval: 10 * time.Second,
		RequestTimeout:        10 * time.Second,
		BlockPruneInterval:    1 * time.Hour, // Default to pruning every hour

		// Retry configuration
		MaxRetries:        3,
		InitialBackoff:    500 * time.Millisecond,
		BackoffMultiplier: 1.5,
	}
}
