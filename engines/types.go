// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package engines

import (
	"context"
	"fmt"
	"time"

	"github.com/luxfi/ids"
)

// EngineType identifies the consensus engine implementation
type EngineType string

const (
	EngineLux  EngineType = "lux"
	EngineGeth EngineType = "geth"
	EngineOP   EngineType = "op"
	EngineEth2 EngineType = "eth2"
)

// ChainInfo describes a blockchain's network properties
type ChainInfo struct {
	ChainID   ids.ID
	NetworkID uint32
	ParentID  *ids.ID // nil for L1, set for L2/L3
}

// HealthStatus represents engine health
type HealthStatus struct {
	Healthy     bool
	BlockHeight uint64
	PeerCount   int
	Syncing     bool
	Version     string
}

// NodeConfig contains engine startup configuration
type NodeConfig struct {
	NetworkID    uint32
	HTTPPort     uint16
	WSPort       uint16
	StakingPort  uint16
	DataDir      string
	LogLevel     string
	BootstrapIPs []string
	// Chain-specific configs
	Extra map[string]interface{}
}

// Engine is the interface for all consensus implementations
type Engine interface {
	// Lifecycle
	Name() string
	Type() EngineType
	Start(ctx context.Context, config *NodeConfig) error
	Stop(ctx context.Context) error
	Restart(ctx context.Context) error

	// Status
	Health(ctx context.Context) (*HealthStatus, error)
	IsRunning() bool
	Uptime() time.Duration

	// Network info
	NetworkID() uint32
	ChainID() ids.ID
	RPCEndpoint() string
	WSEndpoint() string
	P2PEndpoint() string

	// Chain relationships
	ParentChain() *ChainInfo // nil for L1s

	// Metrics
	Metrics() map[string]interface{}
}

// EngineFactory creates engines from configs
type EngineFactory func(name string, binary string) (Engine, error)

var factories = map[EngineType]EngineFactory{}

// Register registers an engine factory
func Register(typ EngineType, factory EngineFactory) {
	factories[typ] = factory
}

// New creates an engine of the given type
func New(typ EngineType, name string, binary string) (Engine, error) {
	factory, ok := factories[typ]
	if !ok {
		return nil, fmt.Errorf("unknown engine type: %s", typ)
	}
	return factory(name, binary)
}
