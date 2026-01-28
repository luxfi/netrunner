// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package lux

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	apiinfo "github.com/luxfi/api/info"
	"github.com/luxfi/ids"
	"github.com/luxfi/netrunner/engines"
	sdkhealth "github.com/luxfi/sdk/health"
	sdkinfo "github.com/luxfi/sdk/info"
)

func init() {
	engines.Register(engines.EngineLux, LuxFactory)
}

// LuxFactory creates Lux engines
func LuxFactory(name string, binary string) (engines.Engine, error) {
	return NewLuxEngine(name, binary)
}

// LuxEngine wraps luxd node management
type LuxEngine struct {
	name         string
	binary       string
	dataDir      string
	config       *engines.NodeConfig
	process      *exec.Cmd
	infoClient   *sdkinfo.Client
	healthClient *sdkhealth.Client
	startTime    time.Time

	// Cached info
	networkID uint32
	chainID   ids.ID
}

// NewLuxEngine creates a new Lux engine
func NewLuxEngine(name string, binary string) (engines.Engine, error) {
	if binary == "" {
		binary = "luxd"
	}
	return &LuxEngine{
		name:   name,
		binary: binary,
	}, nil
}

func (e *LuxEngine) Name() string                    { return e.name }
func (e *LuxEngine) Type() engines.EngineType        { return engines.EngineLux }
func (e *LuxEngine) NetworkID() uint32               { return e.networkID }
func (e *LuxEngine) ChainID() ids.ID                 { return e.chainID }
func (e *LuxEngine) ParentChain() *engines.ChainInfo { return nil } // L1

func (e *LuxEngine) Start(ctx context.Context, config *engines.NodeConfig) error {
	e.config = config
	e.networkID = config.NetworkID

	// Setup data directory
	if config.DataDir == "" {
		config.DataDir = filepath.Join(os.TempDir(), "lux", e.name)
	}
	e.dataDir = config.DataDir

	if err := os.MkdirAll(e.dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data dir: %w", err)
	}

	// Build command arguments
	args := []string{
		fmt.Sprintf("--network-id=%d", config.NetworkID),
		fmt.Sprintf("--http-port=%d", config.HTTPPort),
		fmt.Sprintf("--staking-port=%d", config.StakingPort),
		fmt.Sprintf("--data-dir=%s", e.dataDir),
		fmt.Sprintf("--log-level=%s", config.LogLevel),
		"--http-host=0.0.0.0",
		"--http-allowed-hosts=*",
	}

	// Add bootstrap nodes
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
		return fmt.Errorf("failed to start luxd: %w", err)
	}

	e.startTime = time.Now()

	// Setup RPC clients
	e.infoClient = sdkinfo.NewClient(e.RPCEndpoint())
	e.healthClient = sdkhealth.NewClient(e.RPCEndpoint())

	// Wait for node to be responsive
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	timeout := time.After(30 * time.Second)
	for {
		select {
		case <-ticker.C:
			if _, _, err := e.infoClient.GetNodeID(ctx); err == nil {
				// Cache chain ID
				if cid, err := e.infoClient.GetBlockchainID(ctx, "C"); err == nil {
					e.chainID = cid
				}
				return nil
			}
		case <-timeout:
			return fmt.Errorf("luxd failed to start within 30 seconds")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (e *LuxEngine) Stop(ctx context.Context) error {
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

func (e *LuxEngine) Restart(ctx context.Context) error {
	if err := e.Stop(ctx); err != nil {
		return err
	}
	time.Sleep(2 * time.Second) // Give ports time to release
	return e.Start(ctx, e.config)
}

func (e *LuxEngine) Health(ctx context.Context) (*engines.HealthStatus, error) {
	if e.healthClient == nil || e.infoClient == nil {
		return &engines.HealthStatus{Healthy: false}, nil
	}

	// Get node health
	healthResp, err := e.healthClient.Health(ctx, nil)
	if err != nil {
		return &engines.HealthStatus{Healthy: false}, nil
	}

	// Get peers
	peers, err := e.infoClient.Peers(ctx, nil)
	if err != nil {
		peers = []apiinfo.Peer{}
	}

	// Get version
	versions, err := e.infoClient.GetNodeVersion(ctx)
	if err != nil {
		versions = &apiinfo.GetNodeVersionReply{}
	}

	return &engines.HealthStatus{
		Healthy:   healthResp.Healthy,
		PeerCount: len(peers),
		Version:   versions.Version,
		// BlockHeight from C-Chain would require eth client
	}, nil
}

func (e *LuxEngine) IsRunning() bool {
	return e.process != nil && e.process.Process != nil
}

func (e *LuxEngine) Uptime() time.Duration {
	if !e.IsRunning() {
		return 0
	}
	return time.Since(e.startTime)
}

func (e *LuxEngine) RPCEndpoint() string {
	if e.config == nil {
		return ""
	}
	return fmt.Sprintf("http://localhost:%d", e.config.HTTPPort)
}

func (e *LuxEngine) WSEndpoint() string {
	if e.config == nil {
		return ""
	}
	if e.config.WSPort == 0 {
		// Lux uses same port for HTTP and WS
		return fmt.Sprintf("ws://localhost:%d/ext/bc/C/ws", e.config.HTTPPort)
	}
	return fmt.Sprintf("ws://localhost:%d", e.config.WSPort)
}

func (e *LuxEngine) P2PEndpoint() string {
	if e.config == nil {
		return ""
	}
	return fmt.Sprintf("localhost:%d", e.config.StakingPort)
}

func (e *LuxEngine) Metrics() map[string]interface{} {
	return map[string]interface{}{
		"uptime_seconds": e.Uptime().Seconds(),
		"running":        e.IsRunning(),
		"network_id":     e.networkID,
		"chain_id":       e.chainID.String(),
	}
}
