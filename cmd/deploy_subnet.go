// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package cmd

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/go-bip32"
	"github.com/luxfi/go-bip39"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/sdk/wallet/primary"
	"github.com/spf13/cobra"
)

var deploySubnetCmd = &cobra.Command{
	Use:   "deploy-subnet",
	Short: "Deploy a subnet and blockchain to the running network",
	Long: `Deploy a new subnet and blockchain to the running Lux network.

This command creates a subnet on P-Chain and deploys a blockchain with the
specified genesis configuration.`,
	RunE: runDeploySubnet,
}

var (
	subnetName   string
	genesisPath  string
	vmID         string
	nodeEndpoint string
)

func init() {
	deploySubnetCmd.Flags().StringVar(&subnetName, "name", "zoo", "Name of the subnet/blockchain")
	deploySubnetCmd.Flags().StringVar(&genesisPath, "genesis", "", "Path to genesis JSON file")
	deploySubnetCmd.Flags().StringVar(&vmID, "vm-id", "ag3GReYPNuSR17rUP8acMdZipQBikdXNRKDyFszAysmy3vDXE", "VM ID for the blockchain")
	deploySubnetCmd.Flags().StringVar(&nodeEndpoint, "endpoint", "http://localhost:9630", "Node API endpoint")
}

func runDeploySubnet(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Get mnemonic from environment
	mnemonic := os.Getenv("LUX_MNEMONIC")
	if mnemonic == "" {
		return fmt.Errorf("LUX_MNEMONIC environment variable not set")
	}

	// Derive private key from mnemonic using BIP44 path m/44'/9000'/0'/0/0
	seed := bip39.NewSeed(mnemonic, "")
	masterKey, err := bip32.NewMasterKey(seed)
	if err != nil {
		return fmt.Errorf("failed to create master key: %w", err)
	}

	// BIP44 path: m/44'/9000'/0'/0/0
	path := []uint32{
		bip32.FirstHardenedChild + 44,   // purpose
		bip32.FirstHardenedChild + 9000, // Lux coin type
		bip32.FirstHardenedChild + 0,    // account
		0,                                // external chain
		0,                                // first address
	}

	key := masterKey
	for _, idx := range path {
		key, err = key.NewChildKey(idx)
		if err != nil {
			return fmt.Errorf("failed to derive key: %w", err)
		}
	}

	// Convert to secp256k1 private key
	privKey, err := secp256k1.ToPrivateKey(key.Key)
	if err != nil {
		return fmt.Errorf("failed to create private key: %w", err)
	}

	addr := privKey.Address()
	fmt.Printf("Using wallet: 0x%s\n", hex.EncodeToString(addr[:]))

	// Create keychain with adapter for wallet compatibility
	baseKc := secp256k1fx.NewKeychain(privKey)
	kc := &primary.KeychainAdapter{Keychain: baseKc}

	// Create wallet connected to the network
	walletCfg := primary.WalletConfig{
		URI:         nodeEndpoint,
		LUXKeychain: kc,
		EthKeychain: nil,
	}

	w, err := primary.MakeWallet(ctx, &walletCfg)
	if err != nil {
		return fmt.Errorf("failed to create wallet: %w", err)
	}

	fmt.Printf("Wallet created, connecting to P-chain...\n")

	// Get P-chain wallet
	pWallet := w.P()

	// Create the subnet
	fmt.Printf("\nCreating subnet '%s'...\n", subnetName)

	owner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{addr},
	}

	createSubnetTx, err := pWallet.IssueCreateSubnetTx(owner)
	if err != nil {
		return fmt.Errorf("failed to create subnet: %w", err)
	}

	subnetID := createSubnetTx.ID()
	fmt.Printf("Subnet created! ID: %s\n", subnetID)

	// Load genesis for blockchain
	var genesisBytes []byte
	if genesisPath != "" {
		genesisBytes, err = os.ReadFile(genesisPath)
		if err != nil {
			return fmt.Errorf("failed to read genesis file: %w", err)
		}
	} else {
		genesisBytes = []byte(`{}`)
	}

	// Parse VM ID
	parsedVMID, err := ids.FromString(vmID)
	if err != nil {
		return fmt.Errorf("invalid VM ID: %w", err)
	}

	// Create the blockchain
	fmt.Printf("\nCreating blockchain '%s' on subnet %s...\n", subnetName, subnetID)

	createChainTx, err := pWallet.IssueCreateChainTx(
		subnetID,
		genesisBytes,
		parsedVMID,
		nil,
		subnetName,
	)
	if err != nil {
		return fmt.Errorf("failed to create blockchain: %w", err)
	}

	chainID := createChainTx.ID()
	fmt.Printf("Blockchain created! ID: %s\n", chainID)

	fmt.Println("\n=== Deployment Summary ===")
	fmt.Printf("Subnet ID:     %s\n", subnetID)
	fmt.Printf("Blockchain ID: %s\n", chainID)
	fmt.Printf("VM ID:         %s\n", vmID)
	fmt.Printf("Name:          %s\n", subnetName)

	return nil
}
