package aztec

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"bytes"
	"encoding/json"

	"github.com/certusone/wormhole/node/pkg/common"
	"github.com/certusone/wormhole/node/pkg/p2p"
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

func (e *Watcher) Run(ctx context.Context) error {
	p2p.DefaultRegistry.SetNetworkStats(e.chainID, &gossipv1.Heartbeat_Network{
		ContractAddress: e.aztecContract,
	})

	logger := supervisor.Logger(ctx)

	logger.Info("Starting watcher",
		zap.String("watcher_name", e.networkID),
		zap.String("rpc", e.aztecRPC),
		zap.String("contract", e.aztecContract),
	)

	logger.Info("watcher connecting to RPC node ",
		zap.String("url", e.aztecRPC),
	)

	timer := time.NewTicker(time.Second * 1)
	defer timer.Stop()

	supervisor.Signal(ctx, supervisor.SignalHealthy)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-e.obsvReqC:
			executeAztecRequest(e.aztecRPC)

		case <-timer.C:
			executeAztecRequest(e.aztecRPC)
		}
	}
}

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

func executeAztecRequest(endpoint string) {

	// Create log filter parameter
	logFilter := map[string]any{
		"fromBlock": 146,
		"toBlock":   150,
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
		fmt.Println("Error marshaling JSON:", err)
		return
	}

	// Create a new buffer with the JSON data
	bodyReader := bytes.NewBuffer(jsonData)

	// Send the POST request
	resp, err := http.Post(endpoint, "application/json", bodyReader)
	if err != nil {
		fmt.Println("Error sending request:", err)
		return
	}
	defer resp.Body.Close()

	// Read and print the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading response:", err)
		return
	}

	fmt.Println("SVLACHAKIS Response:", string(body))
}
