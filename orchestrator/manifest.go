// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package orchestrator

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// StackManifest defines a multi-engine stack configuration
type StackManifest struct {
	Name        string          `yaml:"name" json:"name"`
	Version     string          `yaml:"version" json:"version"`
	Description string          `yaml:"description" json:"description"`
	Engines     []EngineConfig  `yaml:"engines" json:"engines"`
	Bridge      *BridgeConfig   `yaml:"bridge,omitempty" json:"bridge,omitempty"`
	Networks    []NetworkConfig `yaml:"networks,omitempty" json:"networks,omitempty"`
}

// EngineConfig defines an engine configuration
type EngineConfig struct {
	Name         string                 `yaml:"name" json:"name"`
	Type         string                 `yaml:"type" json:"type"` // lux, lux, geth, op, eth2
	Binary       string                 `yaml:"binary,omitempty" json:"binary,omitempty"`
	NetworkID    uint32                 `yaml:"network_id" json:"network_id"`
	HTTPPort     uint16                 `yaml:"http_port,omitempty" json:"http_port,omitempty"`
	WSPort       uint16                 `yaml:"ws_port,omitempty" json:"ws_port,omitempty"`
	StakingPort  uint16                 `yaml:"staking_port,omitempty" json:"staking_port,omitempty"`
	DataDir      string                 `yaml:"data_dir,omitempty" json:"data_dir,omitempty"`
	LogLevel     string                 `yaml:"log_level,omitempty" json:"log_level,omitempty"`
	BootstrapIPs []string               `yaml:"bootstrap_ips,omitempty" json:"bootstrap_ips,omitempty"`
	Extra        map[string]interface{} `yaml:"extra,omitempty" json:"extra,omitempty"`
	WaitHealthy  bool                   `yaml:"wait_healthy,omitempty" json:"wait_healthy,omitempty"`
	DependsOn    []string               `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`

	// OP Stack specific
	L1RPC     string `yaml:"l1_rpc,omitempty" json:"l1_rpc,omitempty"`
	Sequencer bool   `yaml:"sequencer,omitempty" json:"sequencer,omitempty"`

	// Eth2 specific
	ConsensusClient  string `yaml:"consensus_client,omitempty" json:"consensus_client,omitempty"`
	ExecutionClient  string `yaml:"execution_client,omitempty" json:"execution_client,omitempty"`
	ValidatorEnabled bool   `yaml:"validator_enabled,omitempty" json:"validator_enabled,omitempty"`
}

// BridgeConfig defines bridge configuration between engines
type BridgeConfig struct {
	Type        string            `yaml:"type" json:"type"` // awm, ibc, ccip
	Source      string            `yaml:"source" json:"source"`
	Destination string            `yaml:"destination" json:"destination"`
	RelayerKey  string            `yaml:"relayer_key,omitempty" json:"relayer_key,omitempty"`
	Contracts   map[string]string `yaml:"contracts,omitempty" json:"contracts,omitempty"`
}

// NetworkConfig defines network topology
type NetworkConfig struct {
	Name      string   `yaml:"name" json:"name"`
	Type      string   `yaml:"type" json:"type"` // l1, l2, l3
	Engine    string   `yaml:"engine" json:"engine"`
	Parent    string   `yaml:"parent,omitempty" json:"parent,omitempty"`
	ChainID   uint64   `yaml:"chain_id" json:"chain_id"`
	Endpoints []string `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
}

// Validate validates the manifest
func (m *StackManifest) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("manifest name is required")
	}

	if len(m.Engines) == 0 {
		return fmt.Errorf("at least one engine is required")
	}

	// Check engine names are unique
	names := make(map[string]bool)
	for _, e := range m.Engines {
		if e.Name == "" {
			return fmt.Errorf("engine name is required")
		}
		if names[e.Name] {
			return fmt.Errorf("duplicate engine name: %s", e.Name)
		}
		names[e.Name] = true

		// Validate engine type
		switch e.Type {
		case "lux", "geth", "op", "eth2":
			// Valid
		default:
			return fmt.Errorf("invalid engine type: %s", e.Type)
		}

		// Check dependencies exist
		for _, dep := range e.DependsOn {
			depFound := false
			for _, other := range m.Engines {
				if other.Name == dep {
					depFound = true
					break
				}
			}
			if !depFound {
				return fmt.Errorf("engine %s depends on unknown engine %s", e.Name, dep)
			}
		}
	}

	// Validate bridge if present
	if m.Bridge != nil {
		if m.Bridge.Source == "" || m.Bridge.Destination == "" {
			return fmt.Errorf("bridge source and destination are required")
		}

		// Check source and destination exist
		sourceFound := false
		destFound := false
		for _, e := range m.Engines {
			if e.Name == m.Bridge.Source {
				sourceFound = true
			}
			if e.Name == m.Bridge.Destination {
				destFound = true
			}
		}
		if !sourceFound {
			return fmt.Errorf("bridge source %s not found in engines", m.Bridge.Source)
		}
		if !destFound {
			return fmt.Errorf("bridge destination %s not found in engines", m.Bridge.Destination)
		}
	}

	return nil
}

// LoadManifest loads a manifest from file
func LoadManifest(path string) (*StackManifest, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	var manifest StackManifest

	// Try YAML first
	if err := yaml.Unmarshal(data, &manifest); err == nil {
		return &manifest, nil
	}

	// Try JSON
	if err := json.Unmarshal(data, &manifest); err == nil {
		return &manifest, nil
	}

	return nil, fmt.Errorf("failed to parse manifest as YAML or JSON")
}

// SaveManifest saves a manifest to file
func (m *StackManifest) Save(path string) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Marshal based on extension
	var data []byte
	var err error

	ext := filepath.Ext(path)
	switch ext {
	case ".json":
		data, err = json.MarshalIndent(m, "", "  ")
	case ".yaml", ".yml":
		data, err = yaml.Marshal(m)
	default:
		// Default to YAML
		data, err = yaml.Marshal(m)
	}

	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	if err := ioutil.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	return nil
}
