package aztec

import (
	"strconv"
	"time"

	"github.com/certusone/wormhole/node/pkg/watchers"
	"github.com/wormhole-foundation/wormhole/sdk/vaa"
)

// WatcherConfig is the configuration used by node.go
type WatcherConfig struct {
	NetworkID watchers.NetworkID
	ChainID   vaa.ChainID
	Rpc       string
	Contract  string
}

// LogParameters encapsulates the core parameters from a log
type LogParameters struct {
	SenderAddress    vaa.Address
	Sequence         uint64
	Nonce            uint32
	ConsistencyLevel uint8
}

// PendingObservation represents an observation waiting for finality
type PendingObservation struct {
	Params        LogParameters
	Payload       []byte
	BlockInfo     BlockInfo
	AztecBlockNum int
	AttemptCount  int
	SubmitTime    time.Time
}

// BlockInfo enhanced to include block hash and parent hash
type BlockInfo struct {
	TxHash     string
	Timestamp  uint64
	BlockHash  string
	ParentHash string
}

// FinalizedBlock represents a finalized block's information
type FinalizedBlock struct {
	Number int
	Hash   string
}

// L2Tips represents the response from the node_getL2Tips RPC method
type L2Tips struct {
	Latest struct {
		Number int    `json:"number"`
		Hash   string `json:"hash"`
	} `json:"latest"`
	Proven struct {
		Number int    `json:"number"`
		Hash   string `json:"hash"`
	} `json:"proven"`
	Finalized struct {
		Number int    `json:"number"`
		Hash   string `json:"hash"`
	} `json:"finalized"`
}

// L2TipsResponse represents the JSON-RPC response containing L2Tips
type L2TipsResponse struct {
	JsonRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Result  L2Tips `json:"result"`
}

// JSON-RPC related structures
type JsonRpcResponse struct {
	JsonRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Result  struct {
		Logs       []ExtendedPublicLog `json:"logs"`
		MaxLogsHit bool                `json:"maxLogsHit"`
	} `json:"result"`
}

type BlockResponse struct {
	JsonRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Result  BlockResult `json:"result"`
}

type BlockResult struct {
	Archive BlockArchive `json:"archive"`
	Header  BlockHeader  `json:"header"`
	Body    BlockBody    `json:"body"`
}

type BlockArchive struct {
	Root                   string `json:"root"`
	NextAvailableLeafIndex int    `json:"nextAvailableLeafIndex"`
}

type BlockHeader struct {
	GlobalVariables GlobalVariables `json:"globalVariables"`
}

type GlobalVariables struct {
	ChainID     string `json:"chainId"`
	Version     string `json:"version"`
	BlockNumber string `json:"blockNumber"`
	SlotNumber  string `json:"slotNumber"`
	Timestamp   string `json:"timestamp"`
	Coinbase    string `json:"coinbase"`
}

type BlockBody struct {
	TxEffects []TxEffect `json:"txEffects"`
}

type TxEffect struct {
	TxHash string `json:"txHash"`
}

type LogId struct {
	BlockNumber int `json:"blockNumber"`
	TxIndex     int `json:"txIndex"`
	LogIndex    int `json:"logIndex"`
}

type PublicLog struct {
	ContractAddress string   `json:"contractAddress"`
	Log             []string `json:"log"`
}

type ExtendedPublicLog struct {
	ID  LogId     `json:"id"`
	Log PublicLog `json:"log"`
}

// Error types
type ErrNetworkUnavailable struct {
	URL string
}

func (e ErrNetworkUnavailable) Error() string {
	return "network unavailable: " + e.URL
}

type ErrRPCError struct {
	Method string
	Code   int
	Msg    string
}

func (e ErrRPCError) Error() string {
	return "RPC error calling " + e.Method + ": " + e.Msg
}

type ErrMaxRetriesExceeded struct {
	Method string
}

func (e ErrMaxRetriesExceeded) Error() string {
	return "max retries exceeded for " + e.Method
}

type ErrParsingFailed struct {
	What string
	Err  error
}

func (e ErrParsingFailed) Error() string {
	return "failed parsing " + e.What + ": " + e.Err.Error()
}

type ErrBlockNotIncluded struct {
	BlockNumber int
}

func (e ErrBlockNotIncluded) Error() string {
	return "block not yet included in L1: " + strconv.Itoa(e.BlockNumber)
}
