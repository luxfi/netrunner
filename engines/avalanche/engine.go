// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/netrunner/engines"
)

func init() {
	engines.Register(engines.EngineAvalanche, NewAvalancheEngine)
}

// AvalancheEngine wraps avalanchego node management
type AvalancheEngine struct {
	name         string
	binary       string
	dataDir      string
	config       *engines.NodeConfig
	process      *exec.Cmd
	httpClient   *http.Client
	rpcEndpoint  string
	startTime    time.Time
	
	// Cached info
	networkID uint32
	chainID   ids.ID
}

// NewAvalancheEngine creates a new Avalanche engine
func NewAvalancheEngine(name string, binary string) (engines.Engine, error) {
	if binary == "" {
		binary = "avalanchego"
	}
	return &AvalancheEngine{
		name:   name,
		binary: binary,
	}, nil
}

func (e *AvalancheEngine) Name() string                   { return e.name }
func (e *AvalancheEngine) Type() engines.EngineType       { return engines.EngineAvalanche }
func (e *AvalancheEngine) NetworkID() uint32              { return e.networkID }
func (e *AvalancheEngine) ChainID() ids.ID                { return e.chainID }
func (e *AvalancheEngine) ParentChain() *engines.ChainInfo { return nil } // L1

func (e *AvalancheEngine) Start(ctx context.Context, config *engines.NodeConfig) error {
	e.config = config
	e.networkID = config.NetworkID
	
	// Setup data directory
	if config.DataDir == "" {
		config.DataDir = filepath.Join(os.TempDir(), "avalanche", e.name)
	}
	e.dataDir = config.DataDir
	
	if err := os.MkdirAll(e.dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data dir: %w", err)
	}
	
	// Build command arguments
	args := []string{
		fmt.Sprintf("--network-id=%s", getAvalancheNetwork(config.NetworkID)),
		fmt.Sprintf("--http-port=%d", config.HTTPPort),
		fmt.Sprintf("--staking-port=%d", config.StakingPort),
		fmt.Sprintf("--data-dir=%s", e.dataDir),
		fmt.Sprintf("--log-level=%s", config.LogLevel),
		"--http-host=0.0.0.0",
		"--http-allowed-hosts=*",
		"--http-allowed-origins=*",
	}
	
	// Add bootstrap nodes if custom network
	if len(config.BootstrapIPs) > 0 {
		for _, ip := range config.BootstrapIPs {
			args = append(args, fmt.Sprintf("--bootstrap-ips=%s", ip))
		}
	}
	
	// Add extra configs
	for k, v := range config.Extra {
		args = append(args, fmt.Sprintf("--%s=%v", k, v))
	}
	
	// Start the process
	e.process = exec.CommandContext(ctx, e.binary, args...)
	e.process.Dir = e.dataDir
	
	// Setup logs
	logFile, err := os.Create(filepath.Join(e.dataDir, "node.log"))
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}
	e.process.Stdout = logFile
	e.process.Stderr = logFile
	
	if err := e.process.Start(); err != nil {
		return fmt.Errorf("failed to start avalanchego: %w", err)
	}
	
	e.startTime = time.Now()
	e.rpcEndpoint = e.RPCEndpoint()
	e.httpClient = &http.Client{Timeout: 10 * time.Second}
	
	// Wait for node to be responsive
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	
	timeout := time.After(60 * time.Second) // Avalanche can take longer
	for {
		select {
		case <-ticker.C:
			if nodeID, err := e.getNodeID(ctx); err == nil && nodeID != "" {
				// Cache C-Chain ID
				if cid, err := e.getBlockchainID(ctx, "C"); err == nil {
					e.chainID = cid
				}
				return nil
			}
		case <-timeout:
			return fmt.Errorf("avalanchego failed to start within 60 seconds")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (e *AvalancheEngine) Stop(ctx context.Context) error {
	if e.process == nil || e.process.Process == nil {
		return nil
	}
	
	// Try graceful shutdown first
	if err := e.process.Process.Signal(os.Interrupt); err != nil {
		return e.process.Process.Kill()
	}
	
	done := make(chan error, 1)
	go func() {
		done <- e.process.Wait()
	}()
	
	select {
	case <-done:
		return nil
	case <-time.After(10 * time.Second):
		return e.process.Process.Kill()
	case <-ctx.Done():
		return e.process.Process.Kill()
	}
}

func (e *AvalancheEngine) Restart(ctx context.Context) error {
	if err := e.Stop(ctx); err != nil {
		return err
	}
	time.Sleep(2 * time.Second) // Give ports time to release
	return e.Start(ctx, e.config)
}

func (e *AvalancheEngine) Health(ctx context.Context) (*engines.HealthStatus, error) {
	if e.httpClient == nil {
		return &engines.HealthStatus{Healthy: false}, nil
	}
	
	// Get node health
	healthy, err := e.getHealth(ctx)
	if err != nil {
		return &engines.HealthStatus{Healthy: false}, nil
	}
	
	// Get peers
	peerCount, _ := e.getPeerCount(ctx)
	
	// Get version
	version, _ := e.getNodeVersion(ctx)
	
	return &engines.HealthStatus{
		Healthy:     healthy,
		PeerCount:   peerCount,
		Version:     version,
		// BlockHeight from C-Chain would require eth client
	}, nil
}

func (e *AvalancheEngine) IsRunning() bool {
	return e.process != nil && e.process.Process != nil
}

func (e *AvalancheEngine) Uptime() time.Duration {
	if !e.IsRunning() {
		return 0
	}
	return time.Since(e.startTime)
}

func (e *AvalancheEngine) RPCEndpoint() string {
	if e.config == nil {
		return ""
	}
	return fmt.Sprintf("http://localhost:%d", e.config.HTTPPort)
}

func (e *AvalancheEngine) WSEndpoint() string {
	if e.config == nil {
		return ""
	}
	// Avalanche uses same port for HTTP and WS
	return fmt.Sprintf("ws://localhost:%d/ext/bc/C/ws", e.config.HTTPPort)
}

func (e *AvalancheEngine) P2PEndpoint() string {
	if e.config == nil {
		return ""
	}
	return fmt.Sprintf("localhost:%d", e.config.StakingPort)
}

func (e *AvalancheEngine) Metrics() map[string]interface{} {
	return map[string]interface{}{
		"uptime_seconds": e.Uptime().Seconds(),
		"running":        e.IsRunning(),
		"network_id":     e.networkID,
		"chain_id":       e.chainID.String(),
	}
}

// getAvalancheNetwork converts network ID to avalanche network name
func getAvalancheNetwork(networkID uint32) string {
	switch networkID {
	case 1:
		return "mainnet"
	case 5:
		return "fuji"
	case 43114:
		return "mainnet" // C-Chain ID
	case 43113:
		return "fuji" // Fuji C-Chain
	default:
		return fmt.Sprintf("%d", networkID)
	}
}

// Helper methods for RPC calls

func (e *AvalancheEngine) callRPC(ctx context.Context, endpoint string, method string, params interface{}) (json.RawMessage, error) {
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	}
	
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	
	req, err := http.NewRequestWithContext(ctx, "POST", e.rpcEndpoint+endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	
	var result struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	
	if result.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", result.Error.Code, result.Error.Message)
	}
	
	return result.Result, nil
}

func (e *AvalancheEngine) getNodeID(ctx context.Context) (string, error) {
	result, err := e.callRPC(ctx, "/ext/info", "info.getNodeID", struct{}{})
	if err != nil {
		return "", err
	}
	
	var resp struct {
		NodeID string `json:"nodeID"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", err
	}
	
	return resp.NodeID, nil
}

func (e *AvalancheEngine) getBlockchainID(ctx context.Context, alias string) (ids.ID, error) {
	result, err := e.callRPC(ctx, "/ext/info", "info.getBlockchainID", map[string]string{"alias": alias})
	if err != nil {
		return ids.Empty, err
	}
	
	var resp struct {
		BlockchainID string `json:"blockchainID"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return ids.Empty, err
	}
	
	return ids.FromString(resp.BlockchainID)
}

func (e *AvalancheEngine) getHealth(ctx context.Context) (bool, error) {
	result, err := e.callRPC(ctx, "/ext/health", "health.health", struct{}{})
	if err != nil {
		return false, err
	}
	
	var resp struct {
		Healthy bool `json:"healthy"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return false, err
	}
	
	return resp.Healthy, nil
}

func (e *AvalancheEngine) getPeerCount(ctx context.Context) (int, error) {
	result, err := e.callRPC(ctx, "/ext/info", "info.peers", struct{}{})
	if err != nil {
		return 0, err
	}
	
	var resp struct {
		NumPeers int `json:"numPeers"`
		Peers    []interface{} `json:"peers"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		// Try counting peers array
		var peers []interface{}
		if err := json.Unmarshal(result, &peers); err != nil {
			return 0, err
		}
		return len(peers), nil
	}
	
	if resp.NumPeers > 0 {
		return resp.NumPeers, nil
	}
	return len(resp.Peers), nil
}

func (e *AvalancheEngine) getNodeVersion(ctx context.Context) (string, error) {
	result, err := e.callRPC(ctx, "/ext/info", "info.getNodeVersion", struct{}{})
	if err != nil {
		return "", err
	}
	
	var resp struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", err
	}
	
	return resp.Version, nil
}