// Copyright (C) 2021-2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause
package constants

import "path/filepath"

const (
	LogNameMain    = "main"
	LogNameControl = "control"
	LogNameTest    = "test"
	RootDirPrefix  = "network-runner-root-data"

	// Network run directory structure (flat, no nesting)
	// Structure: ~/.lux/runs/<networkName>/run_<timestamp>/node1/, node2/, ...
	RunsDir       = "runs"       // Top-level runs directory
	RunDirPrefix  = "run"        // Prefix for timestamped run directories
	DefaultNetwork = "local"     // Default network name

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
