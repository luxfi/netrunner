// Copyright (C) 2019-2022, Lux Partners Limited All rights reserved.
// See the file LICENSE for licensing terms.
package constants

import "path/filepath"

const (
	LogNameMain            = "main"
	LogNameControl         = "control"
	LogNameTest            = "test"
	RootDirPrefix          = "network-runner-root-data"
	DefaultExecPathEnvVar  = "LUXGO_EXEC_PATH"
	DefaultPluginDirEnvVar = "LUXGO_PLUGIN_PATH"
	LocalGenesisFile       = "genesis.json"
	IPv4Lookback           = "127.0.0.1"
)

var LocalConfigDir = filepath.Join("local", "default")
