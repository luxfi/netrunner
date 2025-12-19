// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package geth

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/netrunner/engines"
)

func init() {
	engines.Register(engines.EngineGeth, GethFactory)
}

// GethFactory creates Geth engines
func GethFactory(name string, binary string) (engines.Engine, error) {
	return NewGethEngine(name, binary)
}

// GethEngine wraps standalone Geth node management
type GethEngine struct {
	name      string
	binary    string
	dataDir   string
	config    *engines.NodeConfig
	process   *exec.Cmd
	startTime time.Time
	
	// Cached info
	networkID uint32
	chainID   ids.ID
}

// NewGethEngine creates a new Geth engine
func NewGethEngine(name string, binary string) (engines.Engine, error) {
	if binary == "" {
		binary = "geth"
	}
	return &GethEngine{
		name:   name,
		binary: binary,
	}, nil
}

func (e *GethEngine) Name() string                   { return e.name }
func (e *GethEngine) Type() engines.EngineType       { return engines.EngineGeth }
func (e *GethEngine) NetworkID() uint32              { return e.networkID }
func (e *GethEngine) ChainID() ids.ID                { return e.chainID }
func (e *GethEngine) ParentChain() *engines.ChainInfo { return nil } // L1

func (e *GethEngine) Start(ctx context.Context, config *engines.NodeConfig) error {
	e.config = config
	e.networkID = config.NetworkID
	
	// Generate chain ID from network ID
	chainIDBytes := make([]byte, 32)
	chainIDBig := big.NewInt(int64(config.NetworkID))
	chainIDBig.FillBytes(chainIDBytes)
	copy(e.chainID[:], chainIDBytes)
	
	// Setup data directory
	if config.DataDir == "" {
		config.DataDir = filepath.Join(os.TempDir(), "geth", e.name)
	}
	e.dataDir = config.DataDir
	
	if err := os.MkdirAll(e.dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data dir: %w", err)
	}
	
	// Initialize genesis if needed
	if _, err := os.Stat(filepath.Join(e.dataDir, "geth", "chaindata")); os.IsNotExist(err) {
		// Create simple genesis
		genesis := fmt.Sprintf(`{
			"config": {
				"chainId": %d,
				"homesteadBlock": 0,
				"eip150Block": 0,
				"eip155Block": 0,
				"eip158Block": 0,
				"byzantiumBlock": 0,
				"constantinopleBlock": 0,
				"petersburgBlock": 0,
				"istanbulBlock": 0,
				"berlinBlock": 0,
				"londonBlock": 0,
				"shanghaiTime": 0,
				"cancunTime": 0
			},
			"difficulty": "0x1",
			"gasLimit": "0x8000000",
			"alloc": {}
		}`, config.NetworkID)
		
		genesisPath := filepath.Join(e.dataDir, "genesis.json")
		if err := os.WriteFile(genesisPath, []byte(genesis), 0644); err != nil {
			return fmt.Errorf("failed to write genesis: %w", err)
		}
		
		// Initialize geth with genesis
		initCmd := exec.CommandContext(ctx, e.binary, "init", genesisPath, "--datadir", e.dataDir)
		if err := initCmd.Run(); err != nil {
			return fmt.Errorf("failed to init geth: %w", err)
		}
	}
	
	// Build command arguments
	args := []string{
		"--datadir", e.dataDir,
		"--networkid", fmt.Sprintf("%d", config.NetworkID),
		"--http",
		"--http.addr", "0.0.0.0",
		"--http.port", fmt.Sprintf("%d", config.HTTPPort),
		"--http.api", "eth,net,web3,debug,personal,admin",
		"--http.corsdomain", "*",
		"--http.vhosts", "*",
		"--ws",
		"--ws.addr", "0.0.0.0",
		"--ws.port", fmt.Sprintf("%d", config.WSPort),
		"--ws.api", "eth,net,web3,debug",
		"--ws.origins", "*",
		"--port", fmt.Sprintf("%d", config.StakingPort),
		"--nodiscover",
		"--maxpeers", "0",
		"--syncmode", "full",
		"--gcmode", "archive",
	}
	
	// Add dev mode for testing
	if config.NetworkID > 90000 {
		args = append(args, "--dev", "--dev.period", "1")
	}
	
	// Add bootstrap nodes
	for _, bootnode := range config.BootstrapIPs {
		args = append(args, "--bootnodes", bootnode)
	}
	
	// Add extra configs
	for k, v := range config.Extra {
		switch k {
		case "mine":
			if v.(bool) {
				args = append(args, "--mine", "--miner.threads", "1")
			}
		case "unlock":
			args = append(args, "--unlock", v.(string))
		case "password":
			args = append(args, "--password", v.(string))
		}
	}
	
	// Start the process
	e.process = exec.CommandContext(ctx, e.binary, args...)
	e.process.Dir = e.dataDir
	
	// Setup logs
	logFile, err := os.Create(filepath.Join(e.dataDir, "geth.log"))
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}
	e.process.Stdout = logFile
	e.process.Stderr = logFile
	
	if err := e.process.Start(); err != nil {
		return fmt.Errorf("failed to start geth: %w", err)
	}
	
	e.startTime = time.Now()
	
	// Wait for node to be responsive
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	
	timeout := time.After(30 * time.Second)
	for {
		select {
		case <-ticker.C:
			// Try to connect to RPC
			if e.checkRPC() {
				return nil
			}
		case <-timeout:
			return fmt.Errorf("geth failed to start within 30 seconds")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (e *GethEngine) Stop(ctx context.Context) error {
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

func (e *GethEngine) Restart(ctx context.Context) error {
	if err := e.Stop(ctx); err != nil {
		return err
	}
	time.Sleep(2 * time.Second) // Give ports time to release
	return e.Start(ctx, e.config)
}

func (e *GethEngine) Health(ctx context.Context) (*engines.HealthStatus, error) {
	if !e.IsRunning() {
		return &engines.HealthStatus{Healthy: false}, nil
	}
	
	// Check if RPC is responsive
	healthy := e.checkRPC()
	
	return &engines.HealthStatus{
		Healthy:   healthy,
		PeerCount: 0, // Would need JSON-RPC client to get real peer count
		Version:   "geth",
	}, nil
}

func (e *GethEngine) IsRunning() bool {
	return e.process != nil && e.process.Process != nil
}

func (e *GethEngine) Uptime() time.Duration {
	if !e.IsRunning() {
		return 0
	}
	return time.Since(e.startTime)
}

func (e *GethEngine) RPCEndpoint() string {
	if e.config == nil {
		return ""
	}
	return fmt.Sprintf("http://localhost:%d", e.config.HTTPPort)
}

func (e *GethEngine) WSEndpoint() string {
	if e.config == nil {
		return ""
	}
	return fmt.Sprintf("ws://localhost:%d", e.config.WSPort)
}

func (e *GethEngine) P2PEndpoint() string {
	if e.config == nil {
		return ""
	}
	return fmt.Sprintf("localhost:%d", e.config.StakingPort)
}

func (e *GethEngine) Metrics() map[string]interface{} {
	return map[string]interface{}{
		"uptime_seconds": e.Uptime().Seconds(),
		"running":        e.IsRunning(),
		"network_id":     e.networkID,
		"chain_id":       e.chainID.String(),
	}
}

// checkRPC checks if the RPC endpoint is responsive
func (e *GethEngine) checkRPC() bool {
	// Simple check - would need proper JSON-RPC client for real check
	// For now, just check if process is still running
	if e.process == nil || e.process.Process == nil {
		return false
	}
	
	// Check if process is still alive
	if err := e.process.Process.Signal(os.Signal(nil)); err != nil {
		return false
	}
	
	return true
}