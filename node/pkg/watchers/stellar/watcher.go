//go:build !tinygo
// +build !tinygo

package stellar

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/certusone/wormhole/node/pkg/common"
	gossipv1 "github.com/certusone/wormhole/node/pkg/proto/gossip/v1"
	"github.com/certusone/wormhole/node/pkg/query"
	"github.com/certusone/wormhole/node/pkg/supervisor"
	"github.com/certusone/wormhole/node/pkg/watchers"
	"github.com/certusone/wormhole/node/pkg/watchers/interfaces"
	"github.com/tidwall/gjson"
	"github.com/wormhole-foundation/wormhole/sdk/vaa"
	"go.uber.org/zap"
)

type WatcherConfig struct {
	NetworkID    string
	ChainID      vaa.ChainID
	Rpc          string // Soroban RPC HTTP endpoint
	Contract     string // Core contract id
	PollInterval time.Duration
	ReadTimeout  time.Duration
	StartLedger  uint64
	MaxPerPoll   int
	RequestLimit int64
}

func (wc *WatcherConfig) Create(
	msgC chan<- *common.MessagePublication,
	obsvReqC <-chan *gossipv1.ObservationRequest,
	ccqReqC <-chan *query.PerChainQueryInternal,
	ccqRespC chan<- *query.PerChainQueryResponseInternal,
	guardianSetC chan<- *common.GuardianSet,
	env common.Environment,
) (supervisor.Runnable, interfaces.Reobserver, error) {

	_ = ccqReqC
	_ = ccqRespC
	_ = guardianSetC

	if wc.PollInterval == 0 {
		wc.PollInterval = 700 * time.Millisecond
	}
	if wc.ReadTimeout == 0 {
		wc.ReadTimeout = 10 * time.Second
	}
	if wc.MaxPerPoll <= 0 {
		wc.MaxPerPoll = 128
	}

	w := NewWatcher(
		wc.Rpc,
		wc.Contract,
		wc.ChainID,
		wc.StartLedger,
		wc.PollInterval,
		wc.ReadTimeout,
		wc.MaxPerPoll,
		msgC,
		obsvReqC,
		env,
	)

	return w.Run, nil, nil
}

func (wc *WatcherConfig) GetChainID() vaa.ChainID {
	return wc.ChainID
}

func (wc *WatcherConfig) GetNetworkID() watchers.NetworkID {
	return watchers.NetworkID(wc.NetworkID)
}

type watcher struct {
	rpc          string
	contract     string
	chainID      vaa.ChainID
	nextLedger   uint64
	pollInterval time.Duration
	httpTimeout  time.Duration
	maxPerPoll   int

	msgC       chan<- *common.MessagePublication
	obsvReqC   <-chan *gossipv1.ObservationRequest
	env        common.Environment
	httpClient *http.Client
}

func NewWatcher(
	rpc string,
	contract string,
	chainID vaa.ChainID,
	startLedger uint64,
	pollInterval time.Duration,
	readTimeout time.Duration,
	maxPerPoll int,
	msgC chan<- *common.MessagePublication,
	obsvReqC <-chan *gossipv1.ObservationRequest,
	env common.Environment,
) *watcher {
	return &watcher{
		rpc:          rpc,
		contract:     contract,
		chainID:      chainID,
		nextLedger:   startLedger,
		pollInterval: pollInterval,
		httpTimeout:  readTimeout,
		maxPerPoll:   maxPerPoll,
		msgC:         msgC,
		obsvReqC:     obsvReqC,
		env:          env,
		httpClient:   &http.Client{Timeout: readTimeout},
	}
}

func (w *watcher) Run(ctx context.Context) error {
	logger := supervisor.Logger(ctx).With(
		zap.String("component", "stellar_watcher"),
		zap.String("rpc", w.rpc),
		zap.String("contract", w.contract),
		zap.String("chain", w.chainID.String()),
	)

	if w.nextLedger == 0 {
		seq, err := w.getLatestLedger(ctx)
		if err != nil {
			logger.Error("failed to get latest ledger", zap.Error(err))
			return err
		}
		w.nextLedger = seq
		logger.Info("initialized start ledger", zap.Uint64("ledger", w.nextLedger))
	}

	t := time.NewTicker(w.pollInterval)
	defer t.Stop()

	logger.Info("stellar watcher started")

	for {
		select {
		case <-ctx.Done():
			logger.Info("stellar watcher stopping")
			return nil

		case <-t.C:
			if _, err := w.pollOnce(ctx, logger); err != nil {
				logger.Warn("pollOnce error", zap.Error(err))
				continue
			}

		case req := <-w.obsvReqC:
			logger.Debug("observation request (ignored for stellar)", zap.Any("req", req))
		}
	}
}

type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	ID      int         `json:"id,omitempty"`
}

type rpcResponse struct {
	JSONRPC string           `json:"jsonrpc,omitempty"`
	Result  *json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
	ID int `json:"id,omitempty"`
}

func (w *watcher) call(ctx context.Context, method string, params any) (*json.RawMessage, error) {
	req := rpcRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	}
	body, _ := json.Marshal(&req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, w.rpc, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := w.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rr rpcResponse
	if err := json.Unmarshal(b, &rr); err != nil {
		return nil, fmt.Errorf("jsonrpc decode: %w", err)
	}
	if rr.Error != nil {
		return nil, fmt.Errorf("jsonrpc error %d: %s", rr.Error.Code, rr.Error.Message)
	}
	return rr.Result, nil
}

func (w *watcher) getLatestLedger(ctx context.Context) (uint64, error) {
	res, err := w.call(ctx, "getLatestLedger", nil)
	if err != nil {
		return 0, err
	}
	seq := gjson.GetBytes(*res, "sequence").Uint()
	return seq, nil
}

func (w *watcher) pollOnce(ctx context.Context, logger *zap.Logger) (bool, error) {
	params := map[string]any{
		"fromLedger": w.nextLedger,
		"limit":      w.maxPerPoll,
		"contract":   w.contract,
	}
	res, err := w.call(ctx, "getEvents", params)
	if err != nil {
		return false, err
	}

	events := gjson.GetBytes(*res, "events")
	if !events.Exists() || len(events.Array()) == 0 {
		latest, err := w.getLatestLedger(ctx)
		if err == nil && latest > w.nextLedger {
			w.nextLedger = latest
			return true, nil
		}
		return false, nil
	}

	advanced := false
	now := time.Now().UTC()

	for _, e := range events.Array() {
		if c := e.Get("contractId").Str; w.contract != "" && c != w.contract {
			continue
		}

		ledger := e.Get("ledger").Uint()
		txHash := e.Get("txHash").Str
		etype := e.Get("type").Str
		topics := e.Get("topics").Array()
		data := e.Get("data")

		if etype != "LogMessagePublished" {
			continue
		}
		if len(topics) < 3 {
			continue
		}

		mp := &common.MessagePublication{
			Timestamp:        now,
			Nonce:            uint32(data.Get("nonce").Uint()),
			Sequence:         topics[1].Uint(),
			ConsistencyLevel: uint8(data.Get("consistency").Uint()),
			EmitterChain:     w.chainID,
			EmitterAddress:   stringToAddress(topics[0].Str),
			Payload:          extractPayload(topics[2], logger),
		}

		logger.Debug("stellar publish",
			zap.Uint64("ledger", ledger),
			zap.String("tx", txHash),
			zap.Uint64("seq", mp.Sequence),
			zap.Uint8("consistency", mp.ConsistencyLevel),
		)

		select {
		case w.msgC <- mp:
		case <-ctx.Done():
			return advanced, ctx.Err()
		}

		if ledger >= w.nextLedger {
			w.nextLedger = ledger + 1
			advanced = true
		}
	}

	return advanced, nil
}

func stringToAddress(s string) vaa.Address {
	var out vaa.Address
	b := []byte(s)
	if len(b) >= len(out) {
		copy(out[:], b[:len(out)])
	} else {
		copy(out[:], b)
	}
	return out
}

func extractPayload(topic gjson.Result, logger *zap.Logger) []byte {
	if topic.IsObject() {
		if bs := topic.Get("base64"); bs.Exists() {
			dec, err := base64.StdEncoding.DecodeString(bs.Str)
			if err == nil {
				return dec
			}
			logger.Debug("payload base64 decode failed", zap.Error(err))
		}
	}
	return []byte(topic.Str)
}
