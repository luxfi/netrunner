// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package engines

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/luxfi/ids"
)

// Eth2Engine implements Ethereum 2.0 consensus (Beacon Chain + Execution Layer)
type Eth2Engine struct {
	mu               sync.RWMutex
	name             string
	beaconBinary     string // Consensus client (e.g., Prysm, Lighthouse, Teku)
	executionBinary  string // Execution client (e.g., Geth, Erigon, Nethermind)
	beaconCmd        *exec.Cmd
	executionCmd     *exec.Cmd
	config           *NodeConfig
	startTime        time.Time
	running          bool
	dataDir          string
	
	// Eth2 specific
	chainID          uint64
	networkName      string
	consensusClient  ConsensusClient
	executionClient  ExecutionClient
	validatorEnabled bool
	jwtSecret        string
}

// ConsensusClient types
type ConsensusClient string

const (
	Prysm      ConsensusClient = "prysm"
	Lighthouse ConsensusClient = "lighthouse"
	Teku       ConsensusClient = "teku"
	Nimbus     ConsensusClient = "nimbus"
	Lodestar   ConsensusClient = "lodestar"
)

// ExecutionClient types
type ExecutionClient string

const (
	Geth       ExecutionClient = "geth"
	Erigon     ExecutionClient = "erigon"
	Nethermind ExecutionClient = "nethermind"
	Besu       ExecutionClient = "besu"
)

// Eth2Config contains Ethereum 2.0 specific configuration
type Eth2Config struct {
	ChainID               uint64          `json:"chain_id"`
	NetworkName           string          `json:"network_name"`
	ConsensusClient       ConsensusClient `json:"consensus_client"`
	ExecutionClient       ExecutionClient `json:"execution_client"`
	ValidatorEnabled      bool            `json:"validator_enabled"`
	ValidatorKeys         []string        `json:"validator_keys,omitempty"`
	InitialValidators     []string        `json:"initial_validators,omitempty"`
	TerminalTotalDifficulty string        `json:"terminal_total_difficulty"`
	GenesisTime           uint64          `json:"genesis_time"`
}

// BeaconNodeStatus represents beacon chain status
type BeaconNodeStatus struct {
	HeadSlot           uint64 `json:"head_slot"`
	SyncDistance       uint64 `json:"sync_distance"`
	IsSyncing          bool   `json:"is_syncing"`
	IsOptimistic       bool   `json:"is_optimistic"`
	ElOffline          bool   `json:"el_offline"`
	CurrentEpoch       uint64 `json:"current_epoch"`
	FinalizedEpoch     uint64 `json:"finalized_epoch"`
	JustifiedEpoch     uint64 `json:"justified_epoch"`
}

// NewEth2Engine creates a new Ethereum 2.0 engine
func NewEth2Engine(name string, beaconBinary string, executionBinary string) *Eth2Engine {
	return &Eth2Engine{
		name:            name,
		beaconBinary:    beaconBinary,
		executionBinary: executionBinary,
	}
}

func (e *Eth2Engine) Name() string {
	return e.name
}

func (e *Eth2Engine) Type() EngineType {
	return EngineEth2
}

func (e *Eth2Engine) Start(ctx context.Context, config *NodeConfig) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return fmt.Errorf("engine already running")
	}

	e.config = config
	e.dataDir = filepath.Join(config.DataDir, "eth2", e.name)

	// Create data directories
	if err := os.MkdirAll(filepath.Join(e.dataDir, "beacon"), 0755); err != nil {
		return fmt.Errorf("failed to create beacon data dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(e.dataDir, "execution"), 0755); err != nil {
		return fmt.Errorf("failed to create execution data dir: %w", err)
	}

	// Load Eth2 config from extras
	if err := e.loadEth2Config(); err != nil {
		return fmt.Errorf("failed to load eth2 config: %w", err)
	}

	// Generate JWT secret for Engine API authentication
	if err := e.generateJWTSecret(); err != nil {
		return fmt.Errorf("failed to generate JWT secret: %w", err)
	}

	// Start execution client first
	if err := e.startExecutionClient(ctx); err != nil {
		return fmt.Errorf("failed to start execution client: %w", err)
	}

	// Wait for execution client to be ready
	time.Sleep(5 * time.Second)

	// Then start consensus client
	if err := e.startConsensusClient(ctx); err != nil {
		e.stopExecutionClient()
		return fmt.Errorf("failed to start consensus client: %w", err)
	}

	e.running = true
	e.startTime = time.Now()

	return nil
}

func (e *Eth2Engine) loadEth2Config() error {
	// Set defaults
	e.chainID = 1337 // Default testnet chain ID
	e.networkName = "testnet"
	e.consensusClient = Prysm
	e.executionClient = Geth
	e.validatorEnabled = false

	// Override with config extras
	if chainID, ok := e.config.Extra["chain_id"].(float64); ok {
		e.chainID = uint64(chainID)
	}
	if name, ok := e.config.Extra["network_name"].(string); ok {
		e.networkName = name
	}
	if cc, ok := e.config.Extra["consensus_client"].(string); ok {
		e.consensusClient = ConsensusClient(cc)
	}
	if ec, ok := e.config.Extra["execution_client"].(string); ok {
		e.executionClient = ExecutionClient(ec)
	}
	if val, ok := e.config.Extra["validator_enabled"].(bool); ok {
		e.validatorEnabled = val
	}

	return nil
}

func (e *Eth2Engine) generateJWTSecret() error {
	jwtPath := filepath.Join(e.dataDir, "jwt.hex")
	
	// Check if already exists
	if _, err := os.Stat(jwtPath); err == nil {
		data, err := os.ReadFile(jwtPath)
		if err != nil {
			return err
		}
		e.jwtSecret = string(data)
		return nil
	}

	// Generate new JWT secret
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return fmt.Errorf("failed to generate random JWT secret: %w", err)
	}
	
	e.jwtSecret = hex.EncodeToString(secret)
	return os.WriteFile(jwtPath, []byte(e.jwtSecret), 0600)
}

func (e *Eth2Engine) startExecutionClient(ctx context.Context) error {
	var args []string
	
	switch e.executionClient {
	case Geth:
		args = e.getGethArgs()
	case Erigon:
		args = e.getErigonArgs()
	case Nethermind:
		args = e.getNethermindArgs()
	case Besu:
		args = e.getBesuArgs()
	default:
		return fmt.Errorf("unsupported execution client: %s", e.executionClient)
	}

	e.executionCmd = exec.CommandContext(ctx, e.executionBinary, args...)
	e.executionCmd.Stdout = os.Stdout
	e.executionCmd.Stderr = os.Stderr

	if err := e.executionCmd.Start(); err != nil {
		return fmt.Errorf("failed to start execution client: %w", err)
	}

	return nil
}

func (e *Eth2Engine) getGethArgs() []string {
	return []string{
		"--datadir", filepath.Join(e.dataDir, "execution"),
		"--http",
		"--http.addr", "0.0.0.0",
		"--http.port", fmt.Sprintf("%d", e.config.HTTPPort),
		"--http.api", "eth,net,web3,engine,admin,debug",
		"--http.corsdomain", "*",
		"--ws",
		"--ws.addr", "0.0.0.0",
		"--ws.port", fmt.Sprintf("%d", e.config.WSPort),
		"--ws.api", "eth,net,web3,engine,admin,debug",
		"--authrpc.addr", "0.0.0.0",
		"--authrpc.port", fmt.Sprintf("%d", e.config.HTTPPort+1000),
		"--authrpc.vhosts", "*",
		"--authrpc.jwtsecret", filepath.Join(e.dataDir, "jwt.hex"),
		"--syncmode", "snap",
		"--networkid", fmt.Sprintf("%d", e.chainID),
		"--verbosity", "3",
	}
}

func (e *Eth2Engine) getErigonArgs() []string {
	return []string{
		"--datadir", filepath.Join(e.dataDir, "execution"),
		"--http",
		"--http.addr", "0.0.0.0",
		"--http.port", fmt.Sprintf("%d", e.config.HTTPPort),
		"--http.api", "eth,erigon,engine,net,web3,debug,trace",
		"--ws",
		"--authrpc.addr", "0.0.0.0",
		"--authrpc.port", fmt.Sprintf("%d", e.config.HTTPPort+1000),
		"--authrpc.jwtsecret", filepath.Join(e.dataDir, "jwt.hex"),
		"--chain", e.networkName,
		"--log.console.verbosity", "info",
	}
}

func (e *Eth2Engine) getNethermindArgs() []string {
	return []string{
		"--datadir", filepath.Join(e.dataDir, "execution"),
		"--JsonRpc.Enabled", "true",
		"--JsonRpc.Host", "0.0.0.0",
		"--JsonRpc.Port", fmt.Sprintf("%d", e.config.HTTPPort),
		"--JsonRpc.EnabledModules", "Eth,Net,Web3,Engine,Admin,Debug",
		"--JsonRpc.JwtSecretFile", filepath.Join(e.dataDir, "jwt.hex"),
		"--JsonRpc.EngineHost", "0.0.0.0",
		"--JsonRpc.EnginePort", fmt.Sprintf("%d", e.config.HTTPPort+1000),
		"--Network.DiscoveryPort", fmt.Sprintf("%d", 30303),
		"--Network.P2PPort", fmt.Sprintf("%d", 30303),
	}
}

func (e *Eth2Engine) getBesuArgs() []string {
	return []string{
		"--data-path", filepath.Join(e.dataDir, "execution"),
		"--rpc-http-enabled",
		"--rpc-http-host", "0.0.0.0",
		"--rpc-http-port", fmt.Sprintf("%d", e.config.HTTPPort),
		"--rpc-http-api", "ETH,NET,WEB3,ENGINE,ADMIN,DEBUG",
		"--rpc-ws-enabled",
		"--rpc-ws-host", "0.0.0.0",
		"--rpc-ws-port", fmt.Sprintf("%d", e.config.WSPort),
		"--engine-rpc-port", fmt.Sprintf("%d", e.config.HTTPPort+1000),
		"--engine-jwt-secret", filepath.Join(e.dataDir, "jwt.hex"),
		"--sync-mode", "SNAP",
		"--network-id", fmt.Sprintf("%d", e.chainID),
	}
}

func (e *Eth2Engine) startConsensusClient(ctx context.Context) error {
	var args []string
	
	switch e.consensusClient {
	case Prysm:
		args = e.getPrysmArgs()
	case Lighthouse:
		args = e.getLighthouseArgs()
	case Teku:
		args = e.getTekuArgs()
	case Nimbus:
		args = e.getNimbusArgs()
	case Lodestar:
		args = e.getLodestarArgs()
	default:
		return fmt.Errorf("unsupported consensus client: %s", e.consensusClient)
	}

	e.beaconCmd = exec.CommandContext(ctx, e.beaconBinary, args...)
	e.beaconCmd.Stdout = os.Stdout
	e.beaconCmd.Stderr = os.Stderr

	if err := e.beaconCmd.Start(); err != nil {
		return fmt.Errorf("failed to start beacon client: %w", err)
	}

	return nil
}

func (e *Eth2Engine) getPrysmArgs() []string {
	return []string{
		"--datadir", filepath.Join(e.dataDir, "beacon"),
		"--execution-endpoint", fmt.Sprintf("http://localhost:%d", e.config.HTTPPort+1000),
		"--jwt-secret", filepath.Join(e.dataDir, "jwt.hex"),
		"--rpc-host", "0.0.0.0",
		"--rpc-port", fmt.Sprintf("%d", e.config.HTTPPort+2000),
		"--grpc-gateway-host", "0.0.0.0",
		"--grpc-gateway-port", fmt.Sprintf("%d", e.config.HTTPPort+2001),
		"--monitoring-port", fmt.Sprintf("%d", e.config.HTTPPort+2002),
		"--accept-terms-of-use",
		"--suggested-fee-recipient", "0x0000000000000000000000000000000000000000",
	}
}

func (e *Eth2Engine) getLighthouseArgs() []string {
	return []string{
		"beacon_node",
		"--datadir", filepath.Join(e.dataDir, "beacon"),
		"--execution-endpoint", fmt.Sprintf("http://localhost:%d", e.config.HTTPPort+1000),
		"--execution-jwt", filepath.Join(e.dataDir, "jwt.hex"),
		"--http",
		"--http-address", "0.0.0.0",
		"--http-port", fmt.Sprintf("%d", e.config.HTTPPort+2000),
		"--metrics",
		"--metrics-address", "0.0.0.0",
		"--metrics-port", fmt.Sprintf("%d", e.config.HTTPPort+2002),
		"--network", e.networkName,
	}
}

func (e *Eth2Engine) getTekuArgs() []string {
	return []string{
		"--data-path", filepath.Join(e.dataDir, "beacon"),
		"--ee-endpoint", fmt.Sprintf("http://localhost:%d", e.config.HTTPPort+1000),
		"--ee-jwt-secret-file", filepath.Join(e.dataDir, "jwt.hex"),
		"--rest-api-enabled",
		"--rest-api-interface", "0.0.0.0",
		"--rest-api-port", fmt.Sprintf("%d", e.config.HTTPPort+2000),
		"--metrics-enabled",
		"--metrics-interface", "0.0.0.0",
		"--metrics-port", fmt.Sprintf("%d", e.config.HTTPPort+2002),
		"--network", e.networkName,
	}
}

func (e *Eth2Engine) getNimbusArgs() []string {
	return []string{
		"--data-dir", filepath.Join(e.dataDir, "beacon"),
		"--web3-url", fmt.Sprintf("http://localhost:%d", e.config.HTTPPort+1000),
		"--jwt-secret", filepath.Join(e.dataDir, "jwt.hex"),
		"--rest",
		"--rest-address", "0.0.0.0",
		"--rest-port", fmt.Sprintf("%d", e.config.HTTPPort+2000),
		"--metrics",
		"--metrics-address", "0.0.0.0",
		"--metrics-port", fmt.Sprintf("%d", e.config.HTTPPort+2002),
		"--network", e.networkName,
	}
}

func (e *Eth2Engine) getLodestarArgs() []string {
	return []string{
		"beacon",
		"--dataDir", filepath.Join(e.dataDir, "beacon"),
		"--execution.urls", fmt.Sprintf("http://localhost:%d", e.config.HTTPPort+1000),
		"--jwt-secret", filepath.Join(e.dataDir, "jwt.hex"),
		"--rest",
		"--rest.address", "0.0.0.0",
		"--rest.port", fmt.Sprintf("%d", e.config.HTTPPort+2000),
		"--metrics",
		"--metrics.address", "0.0.0.0",
		"--metrics.port", fmt.Sprintf("%d", e.config.HTTPPort+2002),
		"--network", e.networkName,
	}
}

func (e *Eth2Engine) Stop(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return nil
	}

	// Stop beacon client first
	if e.beaconCmd != nil && e.beaconCmd.Process != nil {
		e.beaconCmd.Process.Signal(os.Interrupt)
		done := make(chan error)
		go func() {
			done <- e.beaconCmd.Wait()
		}()
		
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			e.beaconCmd.Process.Kill()
		}
	}

	// Then stop execution client
	e.stopExecutionClient()

	e.running = false
	return nil
}

func (e *Eth2Engine) stopExecutionClient() {
	if e.executionCmd != nil && e.executionCmd.Process != nil {
		e.executionCmd.Process.Signal(os.Interrupt)
		done := make(chan error)
		go func() {
			done <- e.executionCmd.Wait()
		}()
		
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			e.executionCmd.Process.Kill()
		}
	}
}

func (e *Eth2Engine) Restart(ctx context.Context) error {
	if err := e.Stop(ctx); err != nil {
		return err
	}
	time.Sleep(2 * time.Second)
	return e.Start(ctx, e.config)
}

func (e *Eth2Engine) Health(ctx context.Context) (*HealthStatus, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if !e.running {
		return &HealthStatus{Healthy: false}, nil
	}

	// Check both processes are running
	if e.beaconCmd == nil || e.beaconCmd.Process == nil {
		return &HealthStatus{Healthy: false}, nil
	}
	if e.executionCmd == nil || e.executionCmd.Process == nil {
		return &HealthStatus{Healthy: false}, nil
	}

	// Try to fetch beacon node status
	status, err := e.getBeaconNodeStatus()
	if err != nil {
		return &HealthStatus{
			Healthy: false,
			Version: "eth2",
		}, nil
	}

	return &HealthStatus{
		Healthy:     !status.ElOffline && !status.IsSyncing,
		BlockHeight: status.HeadSlot,
		PeerCount:   0, // TODO: fetch peer count
		Syncing:     status.IsSyncing,
		Version:     fmt.Sprintf("eth2-%s-%s", e.consensusClient, e.executionClient),
	}, nil
}

func (e *Eth2Engine) getBeaconNodeStatus() (*BeaconNodeStatus, error) {
	url := fmt.Sprintf("http://localhost:%d/eth/v1/node/syncing", e.config.HTTPPort+2000)
	
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Data BeaconNodeStatus `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	return &result.Data, nil
}

func (e *Eth2Engine) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.running
}

func (e *Eth2Engine) Uptime() time.Duration {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if !e.running {
		return 0
	}
	return time.Since(e.startTime)
}

func (e *Eth2Engine) NetworkID() uint32 {
	return uint32(e.chainID)
}

func (e *Eth2Engine) ChainID() ids.ID {
	// Convert chain ID to ids.ID
	chainIDStr := fmt.Sprintf("eth2-%d", e.chainID)
	hash := ids.Checksum256([]byte(chainIDStr))
	return ids.ID(hash)
}

func (e *Eth2Engine) RPCEndpoint() string {
	if e.config == nil {
		return ""
	}
	return fmt.Sprintf("http://localhost:%d", e.config.HTTPPort)
}

func (e *Eth2Engine) WSEndpoint() string {
	if e.config == nil {
		return ""
	}
	return fmt.Sprintf("ws://localhost:%d", e.config.WSPort)
}

func (e *Eth2Engine) P2PEndpoint() string {
	// Return beacon API endpoint
	if e.config == nil {
		return ""
	}
	return fmt.Sprintf("http://localhost:%d", e.config.HTTPPort+2000)
}

func (e *Eth2Engine) ParentChain() *ChainInfo {
	// Eth2 mainnet has no parent
	return nil
}

func (e *Eth2Engine) Metrics() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	metrics := map[string]interface{}{
		"running":          e.running,
		"uptime":           e.Uptime().Seconds(),
		"type":             "eth2",
		"consensus_client": string(e.consensusClient),
		"execution_client": string(e.executionClient),
		"chain_id":         e.chainID,
		"network":          e.networkName,
		"validator":        e.validatorEnabled,
	}
	
	return metrics
}

// Eth2Factory creates Ethereum 2.0 engines
func Eth2Factory(name string, binary string) (Engine, error) {
	// For Eth2 we need two binaries
	parts := strings.Split(binary, ":")
	if len(parts) != 2 {
		return nil, fmt.Errorf("Eth2 requires two binaries: beacon:execution")
	}
	return NewEth2Engine(name, parts[0], parts[1]), nil
}

func init() {
	Register(EngineEth2, Eth2Factory)
}