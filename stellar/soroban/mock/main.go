package main

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync/atomic"
	"time"
)

type rpcReq struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	ID     any             `json:"id"`
}
type rpcResp struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   any    `json:"error,omitempty"`
}

var ledger uint64 = 1000

func main() {
	coreID := os.Getenv("CORE_CONTRACT_ID")
	if coreID == "" {
		coreID = "CABCYOURCOREID"
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var req rpcReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp := rpcResp{JSONRPC: "2.0", ID: req.ID}

		switch req.Method {
		case "getLatestLedger":
			atomic.AddUint64(&ledger, 1)
			resp.Result = map[string]any{"sequence": atomic.LoadUint64(&ledger)}

		case "getEvents":
			payload := []byte{0x01, 0x02, 0x03, 0x04}
			resp.Result = map[string]any{
				"events": []any{
					map[string]any{
						"ledger":     atomic.LoadUint64(&ledger),
						"txHash":     "0xdeadbeef",
						"contractId": coreID,
						"type":       "LogMessagePublished",
						"topics": []any{
							coreID,
							float64(42),
							map[string]any{"base64": base64.StdEncoding.EncodeToString(payload)},
						},
						"data":      map[string]any{"nonce": 7, "consistency": 1},
						"timestamp": time.Now().UTC().Format(time.RFC3339),
					},
				},
			}

		default:
			resp.Error = map[string]any{"code": -32601, "message": "method not found"}
		}

		_ = json.NewEncoder(w).Encode(resp)
	})

	log.Println("mock soroban rpc on :8000 (CORE_CONTRACT_ID =", coreID, ")")
	log.Fatal(http.ListenAndServe(":8000", nil))
}
