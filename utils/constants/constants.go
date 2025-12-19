// Copyright (C) 2021-2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause
package constants

import "path/filepath"

const (
	LogNameMain    = "main"
	LogNameControl = "control"
	LogNameTest    = "test"
	RootDirPrefix  = "network-runner-root-data"
)

var (
	LocalConfigDir   = filepath.Join("local", "default")
	LocalGenesisFile = "genesis.json"
)
