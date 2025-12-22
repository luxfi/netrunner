// Copyright (C) 2021-2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause
package ux

import (
	"fmt"

	"github.com/luxfi/log"
)

//nolint:govet // msg is intentionally a format string that may come from variables
func Print(logger log.Logger, msg string, args ...interface{}) {
	fmtMsg := fmt.Sprintf(msg, args...)
	log.Info(fmtMsg)
}
