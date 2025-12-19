// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package engines

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/luxfi/ids"
)

// OPStackEngine implements the OP Stack consensus engine
type OPStackEngine struct {
	mu           sync.RWMutex
	name         string
	opNodeBinary string
	opGethBinary string
	nodeCmd      *exec.Cmd
	gethCmd      *exec.Cmd
	config       *NodeConfig
	startTime    time.Time
	running      bool
	dataDir      string
	
	// OP Stack specific
	l1RPC        string
	sequencer    bool
	rollupConfig *RollupConfig
}

// RollupConfig contains OP Stack rollup configuration
type RollupConfig struct {
	Genesis         RollupGenesis         `json:"genesis"`
	BlockTime       uint64                `json:"block_time"`
	MaxSequencerDrift uint64              `json:"max_sequencer_drift"`
	SeqWindowSize   uint64                `json:"seq_window_size"`
	ChannelTimeout  uint64                `json:"channel_timeout"`
	L1ChainID       uint64                `json:"l1_chain_id"`
	L2ChainID       uint64                `json:"l2_chain_id"`
	P2PSequencerAddress string            `json:"p2p_sequencer_address"`
}

// RollupGenesis contains OP Stack genesis configuration
type RollupGenesis struct {
	L1 struct {
		Hash   string `json:"hash"`
		Number uint64 `json:"number"`
	} `json:"l1"`
	L2 struct {
		Hash   string `json:"hash"`
		Number uint64 `json:"number"`
	} `json:"l2"`
	L2Time uint64 `json:"l2_time"`
}

// NewOPStackEngine creates a new OP Stack engine
func NewOPStackEngine(name string, opNodeBinary string, opGethBinary string) *OPStackEngine {
	return &OPStackEngine{
		name:         name,
		opNodeBinary: opNodeBinary,
		opGethBinary: opGethBinary,
	}
}

func (e *OPStackEngine) Name() string {
	return e.name
}

func (e *OPStackEngine) Type() EngineType {
	return EngineOP
}

func (e *OPStackEngine) Start(ctx context.Context, config *NodeConfig) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return fmt.Errorf("engine already running")
	}

	e.config = config
	e.dataDir = filepath.Join(config.DataDir, "opstack", e.name)

	// Create data directories
	if err := os.MkdirAll(filepath.Join(e.dataDir, "geth"), 0755); err != nil {
		return fmt.Errorf("failed to create geth data dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(e.dataDir, "op-node"), 0755); err != nil {
		return fmt.Errorf("failed to create op-node data dir: %w", err)
	}

	// Extract OP Stack specific config
	if l1RPC, ok := config.Extra["l1_rpc"].(string); ok {
		e.l1RPC = l1RPC
	} else {
		return fmt.Errorf("l1_rpc required for OP Stack")
	}

	if seq, ok := config.Extra["sequencer"].(bool); ok {
		e.sequencer = seq
	}

	// Load or create rollup config
	if err := e.loadRollupConfig(); err != nil {
		return fmt.Errorf("failed to load rollup config: %w", err)
	}

	// Start op-geth first
	if err := e.startGeth(ctx); err != nil {
		return fmt.Errorf("failed to start op-geth: %w", err)
	}

	// Then start op-node
	if err := e.startNode(ctx); err != nil {
		e.stopGeth()
		return fmt.Errorf("failed to start op-node: %w", err)
	}

	e.running = true
	e.startTime = time.Now()

	return nil
}

func (e *OPStackEngine) startGeth(ctx context.Context) error {
	args := []string{
		"--datadir", filepath.Join(e.dataDir, "geth"),
		"--http",
		"--http.port", fmt.Sprintf("%d", e.config.HTTPPort),
		"--http.addr", "0.0.0.0",
		"--http.api", "web3,debug,eth,txpool,net,engine",
		"--ws",
		"--ws.port", fmt.Sprintf("%d", e.config.WSPort),
		"--ws.addr", "0.0.0.0",
		"--ws.api", "web3,debug,eth,txpool,net,engine",
		"--syncmode", "full",
		"--gcmode", "archive",
		"--nodiscover",
		"--maxpeers", "0",
		"--networkid", fmt.Sprintf("%d", e.config.NetworkID),
		"--authrpc.vhosts", "*",
		"--authrpc.addr", "0.0.0.0",
		"--authrpc.port", fmt.Sprintf("%d", e.config.HTTPPort+1000),
		"--authrpc.jwtsecret", filepath.Join(e.dataDir, "jwt.hex"),
	}

	if e.sequencer {
		args = append(args, "--rollup.sequencerhttp", fmt.Sprintf("http://localhost:%d", e.config.HTTPPort+2000))
	}

	// Create JWT secret if it doesn't exist
	jwtPath := filepath.Join(e.dataDir, "jwt.hex")
	if _, err := os.Stat(jwtPath); os.IsNotExist(err) {
		jwt := make([]byte, 32)
		for i := range jwt {
			jwt[i] = byte(i)
		}
		if err := os.WriteFile(jwtPath, []byte(fmt.Sprintf("%x", jwt)), 0600); err != nil {
			return fmt.Errorf("failed to write JWT secret: %w", err)
		}
	}

	e.gethCmd = exec.CommandContext(ctx, e.opGethBinary, args...)
	e.gethCmd.Stdout = os.Stdout
	e.gethCmd.Stderr = os.Stderr

	if err := e.gethCmd.Start(); err != nil {
		return fmt.Errorf("failed to start op-geth: %w", err)
	}

	// Wait for geth to be ready
	time.Sleep(3 * time.Second)

	return nil
}

func (e *OPStackEngine) startNode(ctx context.Context) error {
	args := []string{
		"--l1", e.l1RPC,
		"--l2", fmt.Sprintf("http://localhost:%d", e.config.HTTPPort+1000),
		"--l2.jwt-secret", filepath.Join(e.dataDir, "jwt.hex"),
		"--network", e.name,
		"--rpc.addr", "0.0.0.0",
		"--rpc.port", fmt.Sprintf("%d", e.config.HTTPPort+2000),
		"--p2p.disable",
		"--log.level", e.config.LogLevel,
	}

	if e.sequencer {
		args = append(args, 
			"--sequencer.enabled",
			"--sequencer.l1-confs", "3",
			"--verifier.l1-confs", "3",
		)
	}

	// Add rollup config
	rollupConfigPath := filepath.Join(e.dataDir, "rollup.json")
	if data, err := json.Marshal(e.rollupConfig); err == nil {
		os.WriteFile(rollupConfigPath, data, 0644)
		args = append(args, "--rollup.config", rollupConfigPath)
	}

	e.nodeCmd = exec.CommandContext(ctx, e.opNodeBinary, args...)
	e.nodeCmd.Stdout = os.Stdout
	e.nodeCmd.Stderr = os.Stderr

	if err := e.nodeCmd.Start(); err != nil {
		return fmt.Errorf("failed to start op-node: %w", err)
	}

	return nil
}

func (e *OPStackEngine) loadRollupConfig() error {
	// Load existing or create default config
	configPath := filepath.Join(e.dataDir, "rollup.json")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Create default config
		e.rollupConfig = &RollupConfig{
			BlockTime:         2,
			MaxSequencerDrift: 600,
			SeqWindowSize:     3600,
			ChannelTimeout:    300,
			L1ChainID:         uint64(e.config.NetworkID),
			L2ChainID:         uint64(e.config.NetworkID) + 10000,
		}
		
		// Set genesis
		e.rollupConfig.Genesis.L1.Number = 0
		e.rollupConfig.Genesis.L1.Hash = "0x0000000000000000000000000000000000000000000000000000000000000000"
		e.rollupConfig.Genesis.L2.Number = 0
		e.rollupConfig.Genesis.L2.Hash = "0x0000000000000000000000000000000000000000000000000000000000000000"
		e.rollupConfig.Genesis.L2Time = uint64(time.Now().Unix())
		
		// Save config
		data, err := json.MarshalIndent(e.rollupConfig, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(configPath, data, 0644)
	}

	// Load existing config
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	
	return json.Unmarshal(data, &e.rollupConfig)
}

func (e *OPStackEngine) Stop(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return nil
	}

	// Stop op-node first
	if e.nodeCmd != nil && e.nodeCmd.Process != nil {
		e.nodeCmd.Process.Signal(os.Interrupt)
		done := make(chan error)
		go func() {
			done <- e.nodeCmd.Wait()
		}()
		
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			e.nodeCmd.Process.Kill()
		}
	}

	// Then stop geth
	e.stopGeth()

	e.running = false
	return nil
}

func (e *OPStackEngine) stopGeth() {
	if e.gethCmd != nil && e.gethCmd.Process != nil {
		e.gethCmd.Process.Signal(os.Interrupt)
		done := make(chan error)
		go func() {
			done <- e.gethCmd.Wait()
		}()
		
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			e.gethCmd.Process.Kill()
		}
	}
}

func (e *OPStackEngine) Restart(ctx context.Context) error {
	if err := e.Stop(ctx); err != nil {
		return err
	}
	time.Sleep(2 * time.Second)
	return e.Start(ctx, e.config)
}

func (e *OPStackEngine) Health(ctx context.Context) (*HealthStatus, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if !e.running {
		return &HealthStatus{Healthy: false}, nil
	}

	// Check both processes are running
	if e.nodeCmd == nil || e.nodeCmd.Process == nil {
		return &HealthStatus{Healthy: false}, nil
	}
	if e.gethCmd == nil || e.gethCmd.Process == nil {
		return &HealthStatus{Healthy: false}, nil
	}

	// TODO: Add RPC health checks
	return &HealthStatus{
		Healthy:     true,
		BlockHeight: 0, // TODO: fetch from RPC
		PeerCount:   0, // TODO: fetch from RPC
		Syncing:     false,
		Version:     "op-stack-v1.0.0",
	}, nil
}

func (e *OPStackEngine) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.running
}

func (e *OPStackEngine) Uptime() time.Duration {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if !e.running {
		return 0
	}
	return time.Since(e.startTime)
}

func (e *OPStackEngine) NetworkID() uint32 {
	return e.config.NetworkID
}

func (e *OPStackEngine) ChainID() ids.ID {
	// Convert L2 chain ID to ids.ID
	chainID := ids.Empty
	if e.rollupConfig != nil {
		// Create a deterministic ID from L2 chain ID
		chainIDStr := fmt.Sprintf("op-l2-%d", e.rollupConfig.L2ChainID)
		hash := ids.Checksum256([]byte(chainIDStr))
		chainID = ids.ID(hash)
	}
	return chainID
}

func (e *OPStackEngine) RPCEndpoint() string {
	if e.config == nil {
		return ""
	}
	return fmt.Sprintf("http://localhost:%d", e.config.HTTPPort)
}

func (e *OPStackEngine) WSEndpoint() string {
	if e.config == nil {
		return ""
	}
	return fmt.Sprintf("ws://localhost:%d", e.config.WSPort)
}

func (e *OPStackEngine) P2PEndpoint() string {
	// OP Stack uses different P2P, return op-node RPC
	if e.config == nil {
		return ""
	}
	return fmt.Sprintf("http://localhost:%d", e.config.HTTPPort+2000)
}

func (e *OPStackEngine) ParentChain() *ChainInfo {
	if e.rollupConfig == nil {
		return nil
	}
	// OP Stack L2s have an L1 parent
	parentHash := ids.Checksum256([]byte(fmt.Sprintf("l1-%d", e.rollupConfig.L1ChainID)))
	parentChainID := ids.ID(parentHash)
	return &ChainInfo{
		ChainID:   parentChainID,
		NetworkID: uint32(e.rollupConfig.L1ChainID),
		ParentID:  nil, // L1 has no parent
	}
}

func (e *OPStackEngine) Metrics() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	metrics := map[string]interface{}{
		"running":   e.running,
		"uptime":    e.Uptime().Seconds(),
		"type":      "op-stack",
		"sequencer": e.sequencer,
	}
	
	if e.rollupConfig != nil {
		metrics["l1_chain_id"] = e.rollupConfig.L1ChainID
		metrics["l2_chain_id"] = e.rollupConfig.L2ChainID
		metrics["block_time"] = e.rollupConfig.BlockTime
	}
	
	return metrics
}

// OPStackFactory creates OP Stack engines
func OPStackFactory(name string, binary string) (Engine, error) {
	// For OP Stack we need two binaries
	parts := strings.Split(binary, ":")
	if len(parts) != 2 {
		return nil, fmt.Errorf("OP Stack requires two binaries: op-node:op-geth")
	}
	return NewOPStackEngine(name, parts[0], parts[1]), nil
}

func init() {
	Register(EngineOP, OPStackFactory)
}