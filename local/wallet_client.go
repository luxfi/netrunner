// Copyright (C) 2019-2025, Lux Industries Inc All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package local

import (
	"context"

	"github.com/luxfi/node/vms/platformvm"
	"github.com/luxfi/node/vms/platformvm/txs"
	pwalletwallet "github.com/luxfi/node/wallet/chain/p/wallet"
	"github.com/luxfi/node/wallet/net/primary/common"
)

// walletClient wraps platformvm.Client to implement wallet.Client
type walletClient struct {
	platformClient *platformvm.Client
}

// newWalletClient creates a new wallet client wrapper
func newWalletClient(platformClient *platformvm.Client) pwalletwallet.Client {
	return &walletClient{
		platformClient: platformClient,
	}
}

// IssueTx implements wallet.Client
func (w *walletClient) IssueTx(tx *txs.Tx, options ...common.Option) error {
	ops := common.NewOptions(options)
	ctx := ops.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// Serialize the transaction
	txBytes := tx.Bytes()

	// Issue through platform client
	_, err := w.platformClient.IssueTx(ctx, txBytes)
	return err
}
