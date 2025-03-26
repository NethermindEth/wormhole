package aztec

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"bytes"
	"encoding/json"

	"github.com/certusone/wormhole/node/pkg/common"
	gossipv1 "github.com/certusone/wormhole/node/pkg/proto/gossip/v1"
	"github.com/certusone/wormhole/node/pkg/readiness"
	"github.com/certusone/wormhole/node/pkg/supervisor"
	"github.com/certusone/wormhole/node/pkg/watchers"
	"github.com/wormhole-foundation/wormhole/sdk/vaa"
	"go.uber.org/zap"
)

type (
	// Watcher is responsible for looking over aztec blockchain and reporting new transactions to the wormhole contract
	Watcher struct {
		chainID   vaa.ChainID
		networkID string

		aztecRPC      string
		aztecContract string

		msgC          chan<- *common.MessagePublication
		obsvReqC      <-chan *gossipv1.ObservationRequest
		readinessSync readiness.Component
	}
)

// var (
// 	aztecMessagesConfirmed = promauto.NewCounterVec(
// 		prometheus.CounterOpts{
// 			Name: "wormhole_aztec_observations_confirmed_total",
// 			Help: "Total number of verified observations found for the chain",
// 		}, []string{"chain_name"})
// )

var lastProcessedBlock int = -1

// NewWatcher creates a new aztec appid watcher
func NewWatcher(
	chainID vaa.ChainID,
	networkID watchers.NetworkID,
	aztecRPC string,
	aztecContract string,
	msgC chan<- *common.MessagePublication,
	obsvReqC <-chan *gossipv1.ObservationRequest,
) *Watcher {
	return &Watcher{
		chainID:       chainID,
		networkID:     string(networkID),
		aztecRPC:      aztecRPC,
		aztecContract: aztecContract,
		msgC:          msgC,
		obsvReqC:      obsvReqC,
		readinessSync: common.MustConvertChainIdToReadinessSyncing(chainID),
	}
}

// func (e *Watcher) Run(ctx context.Context) error {
// 	p2p.DefaultRegistry.SetNetworkStats(e.chainID, &gossipv1.Heartbeat_Network{
// 		ContractAddress: e.aztecContract,
// 	})

// 	logger := supervisor.Logger(ctx)

// 	logger.Info("Starting watcher",
// 		zap.String("watcher_name", e.networkID),
// 		zap.String("rpc", e.aztecRPC),
// 		zap.String("contract", e.aztecContract),
// 	)

// 	logger.Info("watcher connecting to RPC node ",
// 		zap.String("url", e.aztecRPC),
// 	)

// 	timer := time.NewTicker(time.Second * 1)
// 	defer timer.Stop()

// 	supervisor.Signal(ctx, supervisor.SignalHealthy)

// 	for {
// 		select {
// 		case <-ctx.Done():
// 			return ctx.Err()
// 		case <-e.obsvReqC:
// 			executeAztecRequest(e.aztecRPC)

// 		case <-timer.C:
// 			executeAztecRequest(e.aztecRPC)
// 		}
// 	}
// }

func (e *Watcher) Run(ctx context.Context) error {
	// Setup a logger
	logger := supervisor.Logger(ctx)

	// Create a connection to the blockchain and subscribe to
	// core contract events here

	// Create the timer for the get_block_height go routine
	timer := time.NewTicker(time.Second * 1)
	defer timer.Stop()

	// Create an error channel
	errC := make(chan error)
	defer close(errC)

	// Signal that basic initialization is complete
	readiness.SetReady(e.readinessSync)

	// Signal to the supervisor that this runnable has finished initialization
	supervisor.Signal(ctx, supervisor.SignalHealthy)

	// Create the go routine to handle events from core contract
	common.RunWithScissors(ctx, errC, "core_events", func(ctx context.Context) error {
		logger.Error("Entering core_events...")
		for {
			select {
			case err := <-errC:
				logger.Error("core_events died", zap.Error(err))
				return fmt.Errorf("core_events died: %w", err)
			case <-ctx.Done():
				logger.Error("coreEvents context done")
				return ctx.Err()

			default:
				executeAztecRequestAllBlocks(e.aztecRPC, logger)
				// Read events and handle them here
				// If this is a blocking read, then set readiness in the
				// get_block_height thread. Else, uncomment the following line:
				readiness.SetReady(e.readinessSync)
			} // end select
		} // end for
	}) // end RunWithScissors

	// Create the go routine to periodically get the block height
	common.RunWithScissors(ctx, errC, "get_block_height", func(ctx context.Context) error {
		for {
			select {
			case err := <-errC:
				logger.Error("get_block_height died", zap.Error(err))
				return fmt.Errorf("get_block_height died: %w", err)
			case <-ctx.Done():
				logger.Error("get_block_height context done")
				return ctx.Err()

			case <-timer.C:
				// Get the block height

				// Try to handle readiness in core_events go routine.
				// If core_events read is a blocking read, then handle
				// readiness here and uncomment the following line:
				// readiness.SetReady(e.readinessSync)
			} // end select
		} // end for
	}) // end RunWithScissors

	// Create the go routine to listen for re-observation requests
	common.RunWithScissors(ctx, errC, "fetch_obvs_req", func(ctx context.Context) error {
		for {
			select {
			case err := <-errC:
				logger.Error("fetch_obvs_req died", zap.Error(err))
				return fmt.Errorf("fetch_obvs_req died: %w", err)
			case <-ctx.Done():
				logger.Error("fetch_obvs_req context done")
				return ctx.Err()
			case <-e.obsvReqC:
				return ctx.Err()
				// Handle the re-observation request
			} // end select
		} // end for
	}) // end RunWithScissors

	// This is done at the end of the Run function to cleanup as needed
	// and return the reason for Run() returning.
	select {
	case <-ctx.Done():
		// Close socket(s), if necessary
		return ctx.Err()
	case err := <-errC:
		// Close socket(s), if necessary
		return err
	} // end select
} // end Run()

// func (e *Watcher) observeData(logger *zap.Logger, data gjson.Result, nativeSeq uint64, isReobservation bool) {

// 	logger.Info("SVLACHAKIS Received obsv request", zap.Any("logs", data.Raw))

// 	em := data.Get("sender")
// 	if !em.Exists() {
// 		logger.Error("sender field missing")
// 		return
// 	}

// 	emitter := make([]byte, 8)
// 	binary.BigEndian.PutUint64(emitter, em.Uint())

// 	var a vaa.Address
// 	copy(a[24:], emitter)

// 	id := make([]byte, 8)
// 	binary.BigEndian.PutUint64(id, nativeSeq)

// 	var txHash = eth_common.BytesToHash(id) // 32 bytes = d3b136a6a182a40554b2fafbc8d12a7a22737c10c81e33b33d1dcb74c532708b

// 	v := data.Get("payload")
// 	if !v.Exists() {
// 		logger.Error("payload field missing")
// 		return
// 	}

// 	pl, err := hex.DecodeString(v.String()[2:])
// 	if err != nil {
// 		logger.Error("payload decode")
// 		return
// 	}

// 	ts := data.Get("timestamp")
// 	if !ts.Exists() {
// 		logger.Error("timestamp field missing")
// 		return
// 	}

// 	nonce := data.Get("nonce")
// 	if !nonce.Exists() {
// 		logger.Error("nonce field missing")
// 		return
// 	}

// 	sequence := data.Get("sequence")
// 	if !sequence.Exists() {
// 		logger.Error("sequence field missing")
// 		return
// 	}

// 	consistencyLevel := data.Get("consistency_level")
// 	if !consistencyLevel.Exists() {
// 		logger.Error("consistencyLevel field missing")
// 		return
// 	}

// 	observation := &common.MessagePublication{
// 		TxID:             txHash.Bytes(),
// 		Timestamp:        time.Unix(int64(ts.Uint()), 0),
// 		Nonce:            uint32(nonce.Uint()), // uint32
// 		Sequence:         sequence.Uint(),
// 		EmitterChain:     e.chainID,
// 		EmitterAddress:   a,
// 		Payload:          pl,
// 		ConsistencyLevel: uint8(consistencyLevel.Uint()),
// 		IsReobservation:  isReobservation,
// 	}

// 	aztecMessagesConfirmed.WithLabelValues(e.networkID).Inc()

// 	logger.Info("message observed",
// 		zap.String("txHash", observation.TxIDString()),
// 		zap.Time("timestamp", observation.Timestamp),
// 		zap.Uint32("nonce", observation.Nonce),
// 		zap.Uint64("sequence", observation.Sequence),
// 		zap.Stringer("emitter_chain", observation.EmitterChain),
// 		zap.Stringer("emitter_address", observation.EmitterAddress),
// 		zap.Binary("payload", observation.Payload),
// 		zap.Uint8("consistencyLevel", observation.ConsistencyLevel),
// 	)

// 	e.msgC <- observation
// }

// func (e *Watcher) retrievePayload(s string) ([]byte, error) {
// 	res, err := http.Get(s) // nolint
// 	if err != nil {
// 		return nil, err
// 	}
// 	defer res.Body.Close()
// 	body, err := io.ReadAll(res.Body)
// 	if err != nil {
// 		return nil, err
// 	}
// 	return body, err
// }

func executeAztecRequestAllBlocks(endpoint string, logger *zap.Logger) {
	// Get the latest block number
	latestBlock, err := getLatestBlockNumber(endpoint)
	if err != nil {
		logger.Error("Error getting latest block", zap.Error(err))
		return
	}

	// Only process if there are new blocks
	if lastProcessedBlock >= latestBlock {
		// Only log this at debug level since it's expected behavior
		logger.Debug("No new blocks to process",
			zap.Int("latest", latestBlock),
			zap.Int("lastProcessed", lastProcessedBlock))
		return
	}

	// Log that we found new blocks to process
	logger.Info("Processing new blocks",
		zap.Int("from", lastProcessedBlock+1),
		zap.Int("to", latestBlock))

	// Start from the next unprocessed block
	startBlock := lastProcessedBlock + 1

	// Process blocks in batches (adjust batch size as needed)
	batchSize := 2000

	for fromBlock := startBlock; fromBlock <= latestBlock; fromBlock += batchSize {
		toBlock := fromBlock + batchSize - 1
		if toBlock > latestBlock {
			toBlock = latestBlock
		}

		logger.Info("Processing block batch",
			zap.Int("fromBlock", fromBlock),
			zap.Int("toBlock", toBlock))

		// Create log filter parameter
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

		// Marshal the payload to JSON
		jsonData, err := json.Marshal(payload)
		if err != nil {
			logger.Error("Error marshaling JSON", zap.Error(err))
			continue
		}

		// Send the POST request
		resp, err := http.Post(endpoint, "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			logger.Error("Error sending request", zap.Error(err))
			continue
		}

		// Read and process the response
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			logger.Error("Error reading response", zap.Error(err))
			continue
		}

		// Process logs from this batch
		processLogs(body, fromBlock, toBlock, logger)

		// Update the last processed block
		lastProcessedBlock = toBlock
	}

	logger.Info("Completed processing blocks", zap.Int("up_to", lastProcessedBlock))
}

func getLatestBlockNumber(endpoint string) (int, error) {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  "node_getBlockNumber",
		"params":  []any{},
		"id":      1,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	resp, err := http.Post(endpoint, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	// Parse the response to get the block number
	var result struct {
		Result json.RawMessage `json:"result"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	// Check if the result is a string (hex) or a number
	var blockNum int

	// Try to unmarshal as string first
	var hexStr string
	if err := json.Unmarshal(result.Result, &hexStr); err == nil {
		// It's a hex string like "0x123"
		parsedNum, err := strconv.ParseInt(hexStr, 0, 64)
		if err != nil {
			return 0, err
		}
		blockNum = int(parsedNum)
	} else {
		// Try to unmarshal as number
		var num float64
		if err := json.Unmarshal(result.Result, &num); err != nil {
			return 0, fmt.Errorf("result is neither string nor number: %v", err)
		}
		blockNum = int(num)
	}

	return blockNum, nil
}

func processLogs(responseBody []byte, fromBlock, toBlock int, logger *zap.Logger) {
	fmt.Println("SVLACHAKIS Response:", string(responseBody))
}
