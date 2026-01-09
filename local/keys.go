// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

// Package local provides network configuration for local development and testing.
// For key management, use github.com/luxfi/keys package directly.
package local

import (
	"encoding/hex"
	"fmt"

	"github.com/luxfi/address"
	ethcrypto "github.com/luxfi/crypto"
	luxcrypto "github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
	"github.com/luxfi/keys"
)

// KeyInfo contains computed addresses from a private key.
// Deprecated: Use github.com/luxfi/keys.ValidatorKey instead.
type KeyInfo struct {
	PrivKeyHex string
	EthAddr    string
	ShortID    ids.ShortID
}

// DefaultKeyPath returns the default path for lux keys.
// Deprecated: Use keys.NewKeyStore("").
func DefaultKeyPath() string {
	return keys.NewKeyStore("").BaseDir()
}

// LoadOrGenerateKeys loads validator keys using the keys package.
// Deprecated: Use keys.KeyStore.LoadAll() or keys.KeyStore.GenerateMultiple().
func LoadOrGenerateKeys(keyPath string, count int) ([]KeyInfo, error) {
	ks := keys.NewKeyStore(keyPath)

	// Try to load existing keys first
	vkeys, err := ks.LoadAll()
	if err != nil || len(vkeys) == 0 {
		// Generate new keys
		vkeys, err = ks.GenerateMultiple(count, "validator")
		if err != nil {
			return nil, fmt.Errorf("failed to generate keys: %w", err)
		}
	}

	// Ensure we have enough keys
	if len(vkeys) < count {
		// Generate additional keys
		additional, err := ks.GenerateMultiple(count-len(vkeys), "validator")
		if err != nil {
			return nil, fmt.Errorf("failed to generate additional keys: %w", err)
		}
		vkeys = append(vkeys, additional...)
	}

	// Convert to KeyInfo for backward compatibility
	result := make([]KeyInfo, count)
	for i := 0; i < count; i++ {
		vk := vkeys[i]
		result[i] = KeyInfo{
			PrivKeyHex: hex.EncodeToString(vk.ECPrivateKey),
			EthAddr:    vk.CChainAddrHex(),
			ShortID:    vk.PChainAddr,
		}
	}

	return result, nil
}

// ComputeKeyInfo derives addresses from a hex-encoded private key
func ComputeKeyInfo(privKeyHex string) (KeyInfo, error) {
	privKeyBytes, err := hex.DecodeString(privKeyHex)
	if err != nil {
		return KeyInfo{}, fmt.Errorf("invalid hex key: %w", err)
	}

	// Get ETH address
	ethPrivKey, err := ethcrypto.ToECDSA(privKeyBytes)
	if err != nil {
		return KeyInfo{}, fmt.Errorf("invalid ECDSA key: %w", err)
	}
	ethAddr := ethcrypto.PubkeyToAddress(ethPrivKey.PublicKey)

	// Get Lux ShortID (for X/P chain addresses)
	luxPrivKey, err := luxcrypto.ToPrivateKey(privKeyBytes)
	if err != nil {
		return KeyInfo{}, fmt.Errorf("invalid Lux key: %w", err)
	}
	pubKey := luxPrivKey.PublicKey()
	shortID := ids.ShortID(pubKey.Address())

	return KeyInfo{
		PrivKeyHex: privKeyHex,
		EthAddr:    ethAddr.Hex(),
		ShortID:    shortID,
	}, nil
}

// FormatAddress formats a ShortID as an X or P chain address with the given HRP
func FormatAddress(chainID string, hrp string, shortID ids.ShortID) (string, error) {
	return address.Format(chainID, hrp, shortID[:])
}

// Re-export constants from keys package for backward compatibility
// Deprecated: Use github.com/luxfi/keys constants directly
const (
	MicroLux              = keys.MicroLux
	Lux                   = keys.Lux
	MegaLux               = keys.MegaLux
	GigaLux               = keys.GigaLux
	OneMillionLUX         = keys.MegaLux
	OneBillionLUX         = keys.GigaLux
	DefaultValidatorStake = keys.DefaultValidatorStake
)

// GenerateValidatorAllocations creates P-Chain allocations for a given number of validators.
// Deprecated: Use keys.NewAllocationBuilder directly.
func GenerateValidatorAllocations(numValidators uint32, hrp string) ([]map[string]interface{}, []KeyInfo, error) {
	// Generate or load keys for the validators
	keyInfos, err := LoadOrGenerateKeys("", int(numValidators))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate validator keys: %w", err)
	}

	allocations := make([]map[string]interface{}, numValidators)
	for i := uint32(0); i < numValidators; i++ {
		key := keyInfos[i]
		luxAddr, err := FormatAddress("P", hrp, key.ShortID)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to format address for validator %d: %w", i, err)
		}

		// All validator funds immediately available (locktime=0)
		// First validator gets extra for fees
		amount := DefaultValidatorStake
		if i == 0 {
			// First validator gets 10x stake for fees, chain creation, etc.
			amount = DefaultValidatorStake * 10
		}

		allocations[i] = map[string]interface{}{
			"ethAddr":       key.EthAddr,
			"luxAddr":       luxAddr,
			"initialAmount": uint64(0),
			"unlockSchedule": []map[string]interface{}{
				{
					"amount":   amount,
					"locktime": uint64(0), // Immediately available
				},
			},
		}
	}

	return allocations, keyInfos, nil
}
