// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/luxfi/netrunner/engines"
	"github.com/luxfi/netrunner/engines/lux"
	"github.com/luxfi/netrunner/engines/avalanche"
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

// StartEngine starts a single engine
func (h *Host) StartEngine(ctx context.Context, name string, typ engines.EngineType, config *engines.NodeConfig) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	
	// Check if already running
	if _, exists := h.engines[name]; exists {
		return fmt.Errorf("engine %s already running", name)
	}
	
	// Allocate ports if not specified
	if config.HTTPPort == 0 {
		config.HTTPPort = h.ports.AllocateHTTP()
	}
	if config.StakingPort == 0 {
		config.StakingPort = h.ports.AllocateP2P()
	}
	
	// Create engine
	var engine engines.Engine
	var err error
	
	switch typ {
	case engines.EngineLux:
		engine, err = lux.NewLuxEngine(name, "")
	case engines.EngineAvalanche:
		engine, err = avalanche.NewAvalancheEngine(name, "")
	default:
		return fmt.Errorf("unsupported engine type: %s", typ)
	}
	
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
		Engine:    string(typ),
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
		
		if err := h.StartEngine(ctx, engine.Name, engines.EngineType(engine.Type), config); err != nil {
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