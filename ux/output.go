// Copyright (C) 2021-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.
package ux

import (
	"fmt"

	"github.com/luxdefi/node/utils/logging"
)

func Print(log logging.Logger, msg string, args ...interface{}) {
	fmtMsg := fmt.Sprintf(msg, args...)
	log.Info(fmtMsg)
}
