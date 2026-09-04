package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type ZapRequest struct {
	Zap     string         `json:"zap,omitempty"`
	Version string         `json:"version,omitempty"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
	ID      any            `json:"id,omitempty"`
}

type ZapResponse struct {
	Zap    string `json:"zap"`
	ID     any    `json:"id,omitempty"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

func handleZapRPC(cfg GatewayConfig, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/zap+json")
	w.Header().Set("X-Zap-Version", "1.0.0")

	if r.Method == "GET" {
		json.NewEncoder(w).Encode(map[string]any{
			"zap":       "1.0",
			"status":    "ZAP_ONLINE",
			"service":   "netrunner",
			"transport": "zap-http",
			"time":      time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	var req ZapRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(ZapResponse{
			Zap:   "1.0",
			Error: fmt.Sprintf("invalid zap frame: %v", err),
		})
		return
	}

	resp := ZapResponse{Zap: "1.0", ID: req.ID}

	switch req.Method {
	case "zap.ping", "ping":
		resp.Result = map[string]any{"status": "pong", "timestamp_ns": time.Now().UTC().UnixNano()}
	case "netrunner.status", "status":
		resp.Result = getNetworkResponse(cfg)
	case "netrunner.consensus", "consensus":
		resp.Result = getConsensusResponse(cfg)
	case "netrunner.validators", "validators":
		vals, err := fetchPChainValidators(cfg.LuxRPC)
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.Result = vals["result"]
		}
	case "netrunner.chains", "chains":
		resp.Result = []map[string]any{
			{
				"chain":      "lux",
				"chain_id":   96369,
				"name":       "Lux Primary Network",
				"vm":         "luxfi/geth",
				"lang":       "go",
				"rpc_path":   "/v1/chain/C/rpc",
				"is_primary": true,
			},
			{
				"chain":      "zoo",
				"chain_id":   200200,
				"name":       "Zoo L2 EVM",
				"vm":         "lux-cpp/noded",
				"lang":       "cpp",
				"rpc_path":   "/v1/chain/zoo",
				"is_primary": false,
			},
			{
				"chain":      "hanzo",
				"chain_id":   36963,
				"name":       "Hanzo L2 EVM",
				"vm":         "lux-rs/hanzod",
				"lang":       "rust",
				"rpc_path":   "/v1/chain/hanzo",
				"is_primary": false,
			},
		}
	default:
		resp.Error = fmt.Sprintf("unknown zap method: %s", req.Method)
	}

	json.NewEncoder(w).Encode(resp)
}
