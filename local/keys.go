// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package local

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	luxcrypto "github.com/luxfi/crypto/secp256k1"
	ethcrypto "github.com/luxfi/crypto"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/formatting/address"
)

// KeyInfo contains computed addresses from a private key
type KeyInfo struct {
	PrivKeyHex string
	EthAddr    string
	ShortID    ids.ShortID
}

// DefaultKeyPath returns the default path for lux keys
func DefaultKeyPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".lux", "keys")
}

// LoadOrGenerateKeys loads validator keys from the specified path or generates new ones if missing.
// Keys are stored as hex-encoded private keys in files named validator_XXX.pk
func LoadOrGenerateKeys(keyPath string, count int) ([]KeyInfo, error) {
	if keyPath == "" {
		keyPath = DefaultKeyPath()
	}

	// Ensure key directory exists
	if err := os.MkdirAll(keyPath, 0700); err != nil {
		return nil, fmt.Errorf("failed to create key directory: %w", err)
	}

	keys := make([]KeyInfo, count)
	for i := 0; i < count; i++ {
		keyFile := filepath.Join(keyPath, fmt.Sprintf("validator_%03d.pk", i))

		var privKeyHex string
		data, err := os.ReadFile(keyFile)
		if err != nil {
			// Key doesn't exist, generate new one
			privKey, genErr := generatePrivateKey()
			if genErr != nil {
				return nil, fmt.Errorf("failed to generate key %d: %w", i, genErr)
			}
			privKeyHex = hex.EncodeToString(privKey)

			// Save the new key
			if err := os.WriteFile(keyFile, []byte(privKeyHex+"\n"), 0600); err != nil {
				return nil, fmt.Errorf("failed to save key %d: %w", i, err)
			}
		} else {
			privKeyHex = strings.TrimSpace(string(data))
		}

		// Compute addresses from the key
		keyInfo, err := ComputeKeyInfo(privKeyHex)
		if err != nil {
			return nil, fmt.Errorf("failed to compute addresses for key %d: %w", i, err)
		}
		keys[i] = keyInfo
	}

	return keys, nil
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

// generatePrivateKey generates a new random 32-byte private key
func generatePrivateKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

// UTXO chain constants (P/X chains)
//
// UTXO Base Unit: MicroLux (10^-6 LUX) - consensus constraint, uses uint64
// UTXO Decimals: 6
//
// EVM Base Unit: WeiLux (10^-18 LUX) - presentation only, uses uint256
// ERC20 Decimals: 18
const (
	// MicroLux is the base unit for UTXO chains (P/X)
	// 1 LUX = 1,000,000 MicroLux
	MicroLux uint64 = 1_000_000 // 10^6

	// OneBillionLUX is 1B LUX in MicroLux (UTXO base unit)
	// 1,000,000,000 * 10^6 = 10^15 MicroLux
	OneBillionLUX uint64 = 1_000_000_000 * MicroLux

	// OneMillionLUX is 1M LUX in MicroLux (UTXO base unit)
	// 1,000,000 * 10^6 = 10^12 MicroLux
	OneMillionLUX uint64 = 1_000_000 * MicroLux

	// OnePercentLUX is 1% of 1B = 10M LUX in MicroLux
	OnePercentLUX uint64 = OneBillionLUX / 100

	// SecondsPerYear for vesting calculations
	SecondsPerYear uint64 = 365 * 24 * 3600

	// Jan1_2020 is Unix timestamp for Jan 1, 2020 00:00:00 UTC (vesting start)
	Jan1_2020 uint64 = 1577836800
)

// ImmediateUnlockLUX is 5% of 1B = 50M LUX for immediate spending (fees, transactions)
const ImmediateUnlockLUX uint64 = OneBillionLUX * 5 / 100

// DefaultValidatorStake is 1M LUX per validator in μLUX
const DefaultValidatorStake uint64 = OneMillionLUX

// GenerateVestingSchedule creates an unlock schedule with:
// - 5% immediately available (locktime=0) for transaction fees
// - 1% per year for 95 years starting from Jan 1, 2020
// This ensures the wallet has spendable funds for chain creation and other operations.
func GenerateVestingSchedule() []map[string]interface{} {
	// First entry: immediately available funds (locktime=0)
	schedule := make([]map[string]interface{}, 96) // 1 immediate + 95 vested
	schedule[0] = map[string]interface{}{
		"amount":   ImmediateUnlockLUX,
		"locktime": uint64(0), // Immediately available
	}
	// Remaining 95% vests 1% per year starting from 2020
	for year := 0; year < 95; year++ {
		unlockTime := Jan1_2020 + (uint64(year) * SecondsPerYear)
		schedule[year+1] = map[string]interface{}{
			"amount":   OnePercentLUX,
			"locktime": unlockTime,
		}
	}
	return schedule
}

// GenerateAllocationsFromKeys creates genesis allocations for loaded keys with vesting
// The first key gets immediately spendable funds (locktime=0) for transaction fees.
// Other keys get vesting schedules starting from Jan 1, 2020.
func GenerateAllocationsFromKeys(keys []KeyInfo, hrp string) ([]map[string]interface{}, error) {
	allocations := make([]map[string]interface{}, len(keys))

	for i, key := range keys {
		luxAddr, err := FormatAddress("P", hrp, key.ShortID)
		if err != nil {
			return nil, fmt.Errorf("failed to format address for key %d: %w", i, err)
		}

		var unlockSchedule []map[string]interface{}
		if i == 0 {
			// First key: all funds immediately available (locktime=0) for transactions
			// This matches how mainnet treasury works
			unlockSchedule = []map[string]interface{}{
				{
					"amount":   OneBillionLUX,
					"locktime": uint64(0),
				},
			}
		} else {
			// Other keys: use vesting schedule
			unlockSchedule = GenerateVestingSchedule()
		}

		allocations[i] = map[string]interface{}{
			"ethAddr":        key.EthAddr,
			"luxAddr":        luxAddr,
			"initialAmount":  uint64(0), // initialAmount is NOT immediately spendable
			"unlockSchedule": unlockSchedule,
		}
	}
	return allocations, nil
}

// GenerateCChainAllocFromKeys creates C-chain genesis allocations for loaded keys
func GenerateCChainAllocFromKeys(keys []KeyInfo) map[string]map[string]string {
	alloc := make(map[string]map[string]string)
	balanceHex := fmt.Sprintf("0x%x", OneBillionLUX)
	for _, key := range keys {
		alloc[key.EthAddr] = map[string]string{"balance": balanceHex}
	}
	return alloc
}

// GenerateValidatorAllocations creates P-Chain allocations for a given number of validators.
// Each validator gets DefaultValidatorStake (1M LUX) with funds immediately available.
// The first validator gets extra funds for transaction fees and chain creation.
func GenerateValidatorAllocations(numValidators uint32, hrp string) ([]map[string]interface{}, []KeyInfo, error) {
	// Generate or load keys for the validators
	keys, err := LoadOrGenerateKeys("", int(numValidators))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate validator keys: %w", err)
	}

	allocations := make([]map[string]interface{}, numValidators)
	for i := uint32(0); i < numValidators; i++ {
		key := keys[i]
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

	return allocations, keys, nil
}
