// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/luxfi/netrunner/engines"
)

// Host manages multiple consensus engines
type Host struct {
	mu        sync.RWMutex
	engines   map[string]engines.Engine
	ports     *PortManager
	indexer   *Indexer
	metrics   *MetricsCollector
	networks  map[string]*NetworkInfo
}

// NetworkInfo describes a network's properties
type NetworkInfo struct {
	Name      string
	Engine    string
	NetworkID uint32
	ChainID   string
	RPC       string
	WS        string
	Parent    string // parent network name for L2/L3
}

// NewHost creates a new multi-engine host
func NewHost() *Host {
	return &Host{
		engines:  make(map[string]engines.Engine),
		ports:    NewPortManager(),
		networks: make(map[string]*NetworkInfo),
		metrics:  NewMetricsCollector(),
	}
}

// EngineOptions contains options for starting an engine
type EngineOptions struct {
	NetworkID    uint32
	HTTPPort     uint16
	WSPort       uint16
	StakingPort  uint16
	DataDir      string
	LogLevel     string
	Binary       string
	BootstrapIPs []string
	Extra        map[string]interface{}
}

// StartEngine starts a single engine
func (h *Host) StartEngine(ctx context.Context, name string, typ string, options *EngineOptions) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	
	// Check if already running
	if _, exists := h.engines[name]; exists {
		return fmt.Errorf("engine %s already running", name)
	}
	
	// Convert options to NodeConfig
	config := &engines.NodeConfig{
		NetworkID:    options.NetworkID,
		HTTPPort:     options.HTTPPort,
		WSPort:       options.WSPort,
		StakingPort:  options.StakingPort,
		DataDir:      options.DataDir,
		LogLevel:     options.LogLevel,
		BootstrapIPs: options.BootstrapIPs,
		Extra:        options.Extra,
	}
	
	// Allocate ports if not specified
	if config.HTTPPort == 0 {
		config.HTTPPort = h.ports.AllocateHTTP()
	}
	if config.WSPort == 0 {
		config.WSPort = config.HTTPPort + 1
	}
	if config.StakingPort == 0 {
		config.StakingPort = h.ports.AllocateP2P()
	}
	
	// Create engine using factory
	engine, err := engines.New(engines.EngineType(typ), name, options.Binary)
	if err != nil {
		return fmt.Errorf("failed to create engine: %w", err)
	}
	
	// Start engine
	if err := engine.Start(ctx, config); err != nil {
		h.ports.Release(config.HTTPPort)
		h.ports.Release(config.StakingPort)
		return fmt.Errorf("failed to start engine: %w", err)
	}
	
	h.engines[name] = engine
	
	// Register network info
	h.networks[name] = &NetworkInfo{
		Name:      name,
		Engine:    typ,
		NetworkID: config.NetworkID,
		ChainID:   engine.ChainID().String(),
		RPC:       engine.RPCEndpoint(),
		WS:        engine.WSEndpoint(),
	}
	
	// Start metrics collection
	go h.collectMetrics(name, engine)
	
	return nil
}

// StopEngine stops a single engine
func (h *Host) StopEngine(ctx context.Context, name string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	
	engine, exists := h.engines[name]
	if !exists {
		return fmt.Errorf("engine %s not found", name)
	}
	
	if err := engine.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop engine: %w", err)
	}
	
	delete(h.engines, name)
	delete(h.networks, name)
	
	return nil
}

// ListEngines returns all running engines
func (h *Host) ListEngines() []EngineInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()
	
	var infos []EngineInfo
	for name, engine := range h.engines {
		health, _ := engine.Health(context.Background())
		infos = append(infos, EngineInfo{
			Name:      name,
			Type:      string(engine.Type()),
			NetworkID: engine.NetworkID(),
			ChainID:   engine.ChainID().String(),
			RPC:       engine.RPCEndpoint(),
			WS:        engine.WSEndpoint(),
			Healthy:   health != nil && health.Healthy,
			Uptime:    engine.Uptime(),
		})
	}
	return infos
}

// EngineInfo contains engine information
type EngineInfo struct {
	Name      string
	Type      string
	NetworkID uint32
	ChainID   string
	RPC       string
	WS        string
	Healthy   bool
	Uptime    time.Duration
}

// StartStack starts a predefined stack of engines
func (h *Host) StartStack(ctx context.Context, manifest *StackManifest) error {
	// Validate manifest
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("invalid manifest: %w", err)
	}
	
	// Start engines in order (L1 first, then L2, then L3)
	for _, engine := range manifest.Engines {
		config := &engines.NodeConfig{
			NetworkID:    engine.NetworkID,
			HTTPPort:     engine.HTTPPort,
			StakingPort:  engine.StakingPort,
			DataDir:      engine.DataDir,
			LogLevel:     engine.LogLevel,
			BootstrapIPs: engine.BootstrapIPs,
			Extra:        engine.Extra,
		}
		
		options := &EngineOptions{
			NetworkID:    config.NetworkID,
			HTTPPort:     config.HTTPPort,
			WSPort:       config.WSPort,
			StakingPort:  config.StakingPort,
			DataDir:      config.DataDir,
			LogLevel:     config.LogLevel,
			BootstrapIPs: config.BootstrapIPs,
			Extra:        config.Extra,
		}
		if err := h.StartEngine(ctx, engine.Name, engine.Type, options); err != nil {
			// Rollback on failure
			h.StopStack(ctx, manifest.Name)
			return fmt.Errorf("failed to start %s: %w", engine.Name, err)
		}
		
		// Wait for health before starting dependent engines
		if engine.WaitHealthy {
			if err := h.waitHealthy(ctx, engine.Name, 30*time.Second); err != nil {
				h.StopStack(ctx, manifest.Name)
				return fmt.Errorf("engine %s failed health check: %w", engine.Name, err)
			}
		}
	}
	
	// Configure bridges if specified
	if manifest.Bridge != nil {
		if err := h.configureBridge(ctx, manifest.Bridge); err != nil {
			return fmt.Errorf("failed to configure bridge: %w", err)
		}
	}
	
	return nil
}

// StopStack stops all engines in a stack
func (h *Host) StopStack(ctx context.Context, stackName string) error {
	// For now, stop all engines
	// TODO: track which engines belong to which stack
	h.mu.Lock()
	engines := make([]string, 0, len(h.engines))
	for name := range h.engines {
		engines = append(engines, name)
	}
	h.mu.Unlock()
	
	var lastErr error
	for _, name := range engines {
		if err := h.StopEngine(ctx, name); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// waitHealthy waits for an engine to become healthy
func (h *Host) waitHealthy(ctx context.Context, name string, timeout time.Duration) error {
	h.mu.RLock()
	engine, exists := h.engines[name]
	h.mu.RUnlock()
	
	if !exists {
		return fmt.Errorf("engine %s not found", name)
	}
	
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			health, err := engine.Health(ctx)
			if err == nil && health.Healthy {
				return nil
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for engine to become healthy")
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// configureBridge sets up bridge between engines
func (h *Host) configureBridge(ctx context.Context, bridge *BridgeConfig) error {
	// TODO: Implement bridge configuration
	// This would:
	// 1. Deploy bridge contracts
	// 2. Start relayer
	// 3. Configure message passing
	return nil
}

// collectMetrics collects metrics from an engine
func (h *Host) collectMetrics(name string, engine engines.Engine) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	
	for range ticker.C {
		h.mu.RLock()
		if _, exists := h.engines[name]; !exists {
			h.mu.RUnlock()
			return // Engine stopped
		}
		h.mu.RUnlock()
		
		metrics := engine.Metrics()
		h.metrics.Record(name, metrics)
	}
}

// Additional methods for CLI compatibility

// WaitForHealth waits for an engine to be healthy
func (h *Host) WaitForHealth(ctx context.Context, name string, timeout time.Duration) error {
	return h.waitHealthy(ctx, name, timeout)
}

// SetupBridge configures a bridge between engines
func (h *Host) SetupBridge(ctx context.Context, bridge *BridgeConfig) error {
	return h.configureBridge(ctx, bridge)
}

// StopAll stops all running engines
func (h *Host) StopAll(ctx context.Context) error {
	h.mu.Lock()
	engines := make([]string, 0, len(h.engines))
	for name := range h.engines {
		engines = append(engines, name)
	}
	h.mu.Unlock()
	
	var lastErr error
	for _, name := range engines {
		if err := h.StopEngine(ctx, name); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// EngineStatus contains engine status information
type EngineStatus struct {
	Type   string
	Health *engines.HealthStatus
}

// GetAllStatus returns status of all engines
func (h *Host) GetAllStatus(ctx context.Context) (map[string]*EngineStatus, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	
	statuses := make(map[string]*EngineStatus)
	for name, engine := range h.engines {
		health, err := engine.Health(ctx)
		if err != nil {
			health = &engines.HealthStatus{Healthy: false}
		}
		statuses[name] = &EngineStatus{
			Type:   string(engine.Type()),
			Health: health,
		}
	}
	return statuses, nil
}

// GetMetrics returns collected metrics
func (h *Host) GetMetrics() map[string]*EngineMetrics {
	if h.metrics == nil {
		return make(map[string]*EngineMetrics)
	}
	return h.metrics.GetAll()
}

// SaveState saves the host state to disk
func (h *Host) SaveState(path string) error {
	h.mu.RLock()
	defer h.mu.RUnlock()
	
	state := &HostState{
		Engines:  make(map[string]*EngineState),
		Networks: h.networks,
	}
	
	for name, engine := range h.engines {
		state.Engines[name] = &EngineState{
			Name:      name,
			Type:      string(engine.Type()),
			NetworkID: engine.NetworkID(),
			ChainID:   engine.ChainID().String(),
			RPC:       engine.RPCEndpoint(),
			WS:        engine.WSEndpoint(),
			Uptime:    engine.Uptime().Seconds(),
		}
	}
	
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	
	return os.WriteFile(path, data, 0644)
}

// LoadState loads the host state from disk
func (h *Host) LoadState(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	
	var state HostState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}
	
	h.mu.Lock()
	defer h.mu.Unlock()
	
	h.networks = state.Networks
	// Note: Engines need to be restarted separately
	
	return nil
}

// HostState represents the persistent state of the host
type HostState struct {
	Engines  map[string]*EngineState  `json:"engines"`
	Networks map[string]*NetworkInfo  `json:"networks"`
}

// EngineState represents the state of an engine
type EngineState struct {
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	NetworkID uint32  `json:"network_id"`
	ChainID   string  `json:"chain_id"`
	RPC       string  `json:"rpc"`
	WS        string  `json:"ws"`
	Uptime    float64 `json:"uptime_seconds"`
}