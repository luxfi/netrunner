package network

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/luxfi/constants"
)

//go:embed default/genesis.json
var genesisBytes []byte

// LoadLocalGenesis loads the embedded local/dev genesis from disk.
// Returns map[string]interface{} for legacy compatibility.
//
// IMPORTANT: This function is for LOCAL/DEV networks only.
// For canonical networks (mainnet/testnet/devnet), use the genesis configs
// from luxfi/genesis/configs package via local.NewCanonicalXxxConfig().
//
// The embedded genesis.json MUST have cChainGenesis as a JSON string (not object).
// This function does NOT modify cChainGenesis - it preserves it exactly as embedded.
func LoadLocalGenesis() (map[string]interface{}, error) {
	var genesisMap map[string]interface{}
	if err := json.Unmarshal(genesisBytes, &genesisMap); err != nil {
		return nil, err
	}

	// Validate cChainGenesis type - MUST be string in embedded genesis
	if cChainGenesis, ok := genesisMap["cChainGenesis"]; ok {
		switch cChainGenesis.(type) {
		case string:
			// Correct type - keep as-is, do not modify
		case map[string]interface{}:
			// Wrong type - embedded genesis.json is malformed
			return nil, fmt.Errorf(
				"embedded genesis has cChainGenesis as object, but it must be a JSON string. " +
					"Fix the embedded network/default/genesis.json file")
		default:
			return nil, fmt.Errorf(
				"expected cChainGenesis to be string, got %T", cChainGenesis)
		}
	}

	return genesisMap, nil
}

// LoadCanonicalGenesis loads canonical genesis for mainnet/testnet WITHOUT modifying configs.
// This preserves the exact cChainGenesis to ensure correct genesis hash computation.
// For custom/local networks, falls back to LoadLocalGenesis behavior.
func LoadCanonicalGenesis(networkID uint32) (map[string]interface{}, error) {
	// For local/custom networks, use the standard local genesis with TestChainConfig
	if networkID != constants.MainnetID && networkID != constants.TestnetID {
		return LoadLocalGenesis()
	}

	// For mainnet/testnet, load canonical genesis from filesystem
	genesisData, err := LoadGenesisForNetwork(networkID)
	if err != nil {
		return nil, fmt.Errorf("failed to load canonical genesis for network %d: %w", networkID, err)
	}

	var genesisMap map[string]interface{}
	if err = json.Unmarshal(genesisData, &genesisMap); err != nil {
		return nil, fmt.Errorf("failed to parse canonical genesis: %w", err)
	}

	// IMPORTANT: Do NOT modify cChainGenesis config for canonical networks.
	// The canonical genesis must be used as-is to produce the correct genesis hash.
	return genesisMap, nil
}

// LoadGenesisForNetwork loads genesis config from standard paths based on network ID.
// For mainnet (1) and testnet (2), it loads from luxfi/genesis configs.
// For other network IDs, it falls back to local genesis.
func LoadGenesisForNetwork(networkID uint32) ([]byte, error) {
	var networkName string
	switch networkID {
	case constants.MainnetID:
		networkName = "mainnet"
	case constants.TestnetID:
		networkName = "testnet"
	default:
		// For custom/local networks, use embedded genesis
		return genesisBytes, nil
	}

	// Try standard genesis locations
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, "work/lux/genesis/configs", networkName, "genesis.json"),
		filepath.Join(home, ".lux/genesis", networkName, "genesis.json"),
		filepath.Join("/etc/lux/genesis", networkName, "genesis.json"),
	}

	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err == nil {
			return data, nil
		}
	}

	return nil, fmt.Errorf("genesis config not found for network %s (ID: %d)", networkName, networkID)
}

// LoadMainnetGenesis loads the canonical mainnet genesis configuration.
func LoadMainnetGenesis() ([]byte, error) {
	return LoadGenesisForNetwork(constants.MainnetID)
}

// LoadTestnetGenesis loads the canonical testnet genesis configuration.
func LoadTestnetGenesis() ([]byte, error) {
	return LoadGenesisForNetwork(constants.TestnetID)
}
