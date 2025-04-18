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
	EthRpcURL       string

	// Processing parameters
	StartBlock        int
	PayloadInitialCap int

	// Timeouts and intervals
	RPCTimeout            time.Duration
	LogProcessingInterval time.Duration
	FinalityCheckInterval time.Duration
	FinalityTimeout       time.Duration
	RequestTimeout        time.Duration

	// Ethereum rollup contract address
	RollupContractAddress string

	// Retry configuration
	MaxRetries        int
	InitialBackoff    time.Duration
	BackoffMultiplier float64
}

// DefaultConfig returns a default configuration
func DefaultConfig(chainID vaa.ChainID, networkID string, rpcURL, contractAddress, ethRpcURL string) Config {
	return Config{
		// Chain identification
		ChainID:   chainID,
		NetworkID: networkID,

		// Connection details
		RpcURL:          rpcURL,
		ContractAddress: contractAddress,
		EthRpcURL:       ethRpcURL,

		// Processing parameters
		StartBlock:        0,
		PayloadInitialCap: 13,

		// Timeouts and intervals
		RPCTimeout:            30 * time.Second,
		LogProcessingInterval: 1 * time.Second,
		FinalityCheckInterval: 10 * time.Second,
		FinalityTimeout:       30 * time.Minute,
		RequestTimeout:        10 * time.Second,

		// Ethereum rollup contract address
		RollupContractAddress: "0x0b306bf915c4d645ff596e518faf3f9669b97016",

		// Retry configuration
		MaxRetries:        3,
		InitialBackoff:    500 * time.Millisecond,
		BackoffMultiplier: 1.5,
	}
}
