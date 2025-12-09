// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package local

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	luxcrypto "github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/geth/crypto"
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
	ethPrivKey, err := crypto.ToECDSA(privKeyBytes)
	if err != nil {
		return KeyInfo{}, fmt.Errorf("invalid ECDSA key: %w", err)
	}
	ethAddr := crypto.PubkeyToAddress(ethPrivKey.PublicKey)

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

// Vesting configuration constants
const (
	// OneBillionLUX is 1B LUX in nLUX (9 decimals)
	OneBillionLUX uint64 = 1_000_000_000_000_000_000
	// OnePercentLUX is 1% of 1B = 10M LUX
	OnePercentLUX uint64 = OneBillionLUX / 100
	// SecondsPerYear for vesting calculations
	SecondsPerYear uint64 = 365 * 24 * 3600
	// Jan1_2020 is Unix timestamp for Jan 1, 2020 00:00:00 UTC (vesting start)
	Jan1_2020 uint64 = 1577836800
)

// GenerateVestingSchedule creates an unlock schedule with 1% per year for 100 years
// starting from Jan 1, 2020. This means ~5-6% is already unlocked as of Dec 2025.
func GenerateVestingSchedule() []map[string]interface{} {
	schedule := make([]map[string]interface{}, 100)
	for year := 0; year < 100; year++ {
		unlockTime := Jan1_2020 + (uint64(year) * SecondsPerYear)
		schedule[year] = map[string]interface{}{
			"amount":   OnePercentLUX,
			"locktime": unlockTime,
		}
	}
	return schedule
}

// GenerateAllocationsFromKeys creates genesis allocations for loaded keys with vesting
func GenerateAllocationsFromKeys(keys []KeyInfo, hrp string) ([]map[string]interface{}, error) {
	vestingSchedule := GenerateVestingSchedule()
	allocations := make([]map[string]interface{}, len(keys))

	for i, key := range keys {
		luxAddr, err := FormatAddress("X", hrp, key.ShortID)
		if err != nil {
			return nil, fmt.Errorf("failed to format address for key %d: %w", i, err)
		}
		allocations[i] = map[string]interface{}{
			"ethAddr":        key.EthAddr,
			"luxAddr":        luxAddr,
			"initialAmount":  OneBillionLUX, // 1B LUX on X-chain
			"unlockSchedule": vestingSchedule,
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
