// Copyright (C) 2021-2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause
package constants

import "path/filepath"

const (
	LogNameMain    = "main"
	LogNameControl = "control"
	LogNameTest    = "test"
	RootDirPrefix  = "network-runner-root-data"

	// Network-centric directory structure constants
	// Structure: ~/.lux/networks/<networkName>/runs/<runID>/node1/, node2/, ...
	NetworksDir = "networks" // Top-level networks directory
	RunsDir     = "runs"     // Runs subdirectory within each network

	// Unified chain config directory (shared across all nodes)
	// Structure: ~/.lux/chains/<chainName>/genesis.json, config.json, upgrade.json
	ChainsDir         = "chains"
	ChainGenesisFile  = "genesis.json"
	ChainConfigFile   = "config.json"
	ChainUpgradeFile  = "upgrade.json"

	// Unified plugins directory
	// Structure: ~/.lux/plugins/current/<vmid> (symlinks to actual binaries)
	PluginsDir        = "plugins"
	CurrentPluginsDir = "current"
)

var (
	LocalConfigDir   = filepath.Join("local", "default")
	LocalGenesisFile = "genesis.json"
)
