// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package local

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/luxfi/const"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/bls/signer/localsigner"
	"github.com/luxfi/node/vms/platformvm/signer"
	luxcrypto "github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/genesis/configs"
	"github.com/luxfi/ids"
	"github.com/luxfi/keys"
	"github.com/luxfi/netrunner/network"
	"github.com/luxfi/netrunner/network/node"
	"github.com/luxfi/netrunner/utils"
	"github.com/luxfi/node/config"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/utils/formatting/address"
	"golang.org/x/exp/maps"
)

// NewConfigForNetwork creates a network config for the specified network ID.
// This uses the proper genesis configuration from github.com/luxfi/genesis/configs.
//
// For mainnet/testnet, this function:
// 1. Uses embedded validator keys from defaultNetworkConfig (not generating new ones)
// 2. Preserves initialStakers from the genesis package (already matches embedded keys)
// 3. Generates P-chain allocations for the embedded validators (10M LUX each)
// 4. Preserves cchainGenesis from the genesis package
//
// Supported network IDs:
//   - 96369: LUX Mainnet
//   - 96368: LUX Testnet
//   - 1337: Local development network
func NewConfigForNetwork(binaryPath string, numNodes uint32, networkID uint32) (network.Config, error) {
	// Get genesis for the specified network
	genesisJSON, err := configs.GetGenesis(networkID)
	if err != nil {
		return network.Config{}, fmt.Errorf("failed to get genesis for network %d: %w", networkID, err)
	}

	// Start with the default config structure (includes embedded validator keys)
	netConfig := NewDefaultConfig(binaryPath)

	// For local network (1337), use the genesis as-is since it already has stakers
	// matching the pre-generated node keys
	// Note: configs.CustomID is 1337 for custom local development network
	if networkID == configs.CustomID {
		netConfig.Genesis = string(genesisJSON)
		// Handle node count for local network
		if int(numNodes) > len(netConfig.NodeConfigs) {
			toAdd := int(numNodes) - len(netConfig.NodeConfigs)
			refNodeConfig := netConfig.NodeConfigs[len(netConfig.NodeConfigs)-1]
			refAPIPortIntf, ok := refNodeConfig.Flags[config.HTTPPortKey]
			if !ok {
				return netConfig, fmt.Errorf("could not get last standard api port from config")
			}
			refAPIPort, ok := refAPIPortIntf.(float64)
			if !ok {
				return netConfig, fmt.Errorf("expected float64 for last standard api port, got %T", refAPIPortIntf)
			}
			refStakingPortIntf, ok := refNodeConfig.Flags[config.StakingPortKey]
			if !ok {
				return netConfig, fmt.Errorf("could not get last standard staking port from config")
			}
			refStakingPort, ok := refStakingPortIntf.(float64)
			if !ok {
				return netConfig, fmt.Errorf("expected float64 for last standard staking port, got %T", refStakingPortIntf)
			}
			for i := 0; i < toAdd; i++ {
				nodeConfig := refNodeConfig
				stakingCert, stakingKey, err := staking.NewCertAndKeyBytes()
				if err != nil {
					return netConfig, fmt.Errorf("couldn't generate staking Cert/Key: %w", err)
				}
				nodeConfig.StakingKey = string(stakingKey)
				nodeConfig.StakingCert = string(stakingCert)
				nodeConfig.Flags = map[string]interface{}{
					config.HTTPPortKey:    int(refAPIPort) + (i+1)*2,
					config.StakingPortKey: int(refStakingPort) + (i+1)*2,
				}
				netConfig.NodeConfigs = append(netConfig.NodeConfigs, nodeConfig)
			}
		}
		if int(numNodes) < len(netConfig.NodeConfigs) {
			netConfig.NodeConfigs = netConfig.NodeConfigs[:numNodes]
		}
		return netConfig, nil
	}

	// For mainnet/testnet:
	// - Use embedded validator keys from defaultNetworkConfig
	// - Keep initialStakers from genesis (already matches embedded keys)
	// - Generate P-chain allocations for the embedded validators

	// Parse genesis to modify it
	var genesis map[string]interface{}
	if err := json.Unmarshal(genesisJSON, &genesis); err != nil {
		return network.Config{}, fmt.Errorf("failed to parse genesis: %w", err)
	}

	hrp := constants.GetHRP(networkID)

	// Number of embedded validators available
	numEmbedded := uint32(len(defaultNetworkConfig.NodeConfigs))
	if numEmbedded > 5 {
		numEmbedded = 5 // Cap at 5 embedded validators
	}

	// Use min(numNodes, numEmbedded) for node configs with embedded keys
	numEmbeddedToUse := numNodes
	if numEmbeddedToUse > numEmbedded {
		numEmbeddedToUse = numEmbedded
	}

	// For P/X-chain allocations, we need to create them for keys the wallet can use.
	// C-chain allocations are historic (embedded in genesis), but P/X can be set freely.
	// REPLACE allocations with ones for the wallet's key (mnemonic or private key).
	var newAllocations []interface{}

	// If LUX_MNEMONIC is set, create allocations for mnemonic-derived key
	// Need BOTH X-chain (for transfers) and P-chain (for chain creation/validators)
	if mnemonic := os.Getenv("LUX_MNEMONIC"); mnemonic != "" {
		validatorKeys, err := keys.DeriveValidatorsFromMnemonic(mnemonic, 1)
		if err == nil && len(validatorKeys) > 0 {
			vk := validatorKeys[0]
			addr := vk.PChainAddr // Same underlying secp256k1 address
			xLuxAddr, errX := address.Format("X", hrp, addr[:])
			pLuxAddr, errP := address.Format("P", hrp, addr[:])
			if errX == nil && errP == nil {
				fmt.Printf("🔑 Setting X+P allocations for LUX_MNEMONIC: X=%s, P=%s (2B each)\n", xLuxAddr, pLuxAddr)
				// X-chain allocation (for asset transfers)
				newAllocations = append(newAllocations, map[string]interface{}{
					"ethAddr":        vk.CChainAddrHex(),
					"luxAddr":        xLuxAddr,
					"initialAmount":  uint64(2000000000000000000), // 2B LUX
					"unlockSchedule": []map[string]interface{}{},
				})
				// P-chain allocation (for chain creation, validators)
				// IMPORTANT: Must have unlockSchedule entries for builder to create UTXOs
				newAllocations = append(newAllocations, map[string]interface{}{
					"ethAddr":       vk.CChainAddrHex(),
					"luxAddr":       pLuxAddr,
					"initialAmount": uint64(0), // Not used for P-chain, unlockSchedule is used
					"unlockSchedule": []map[string]interface{}{
						{
							"amount":   uint64(2000000000000000000), // 2B LUX
							"locktime": uint64(0),                   // Immediately available
						},
					},
				})
			}
		}
	}

	// If LUX_PRIVATE_KEY is set, also create X+P allocations for it
	if privKeyHex := os.Getenv("LUX_PRIVATE_KEY"); privKeyHex != "" {
		privKeyBytes, err := hex.DecodeString(privKeyHex)
		if err == nil && len(privKeyBytes) == 32 {
			luxPrivKey, err := luxcrypto.ToPrivateKey(privKeyBytes)
			if err == nil {
				pubKey := luxPrivKey.PublicKey()
				addr := ids.ShortID(pubKey.Address())
				xLuxAddr, errX := address.Format("X", hrp, addr[:])
				pLuxAddr, errP := address.Format("P", hrp, addr[:])
				if errX == nil && errP == nil {
					fmt.Printf("🔑 Adding X+P allocations for LUX_PRIVATE_KEY: X=%s, P=%s (2B each)\n", xLuxAddr, pLuxAddr)
					ethAddr := "0x" + hex.EncodeToString(addr[:])
					// X-chain allocation
					newAllocations = append(newAllocations, map[string]interface{}{
						"ethAddr":        ethAddr,
						"luxAddr":        xLuxAddr,
						"initialAmount":  uint64(2000000000000000000), // 2B LUX
						"unlockSchedule": []map[string]interface{}{},
					})
					// P-chain allocation - must use unlockSchedule for builder to create UTXOs
					newAllocations = append(newAllocations, map[string]interface{}{
						"ethAddr":       ethAddr,
						"luxAddr":       pLuxAddr,
						"initialAmount": uint64(0), // Not used for P-chain
						"unlockSchedule": []map[string]interface{}{
							{
								"amount":   uint64(2000000000000000000), // 2B LUX
								"locktime": uint64(0),                   // Immediately available
							},
						},
					})
				}
			}
		}
	}

	// If we created new allocations, replace the genesis allocations
	// Otherwise, keep the genesis package's allocations (for backward compatibility)
	if len(newAllocations) > 0 {
		genesis["allocations"] = newAllocations
	}

	// Generate initialStakers from embedded validator keys if not provided in genesis
	// This is needed because testnet/mainnet genesis may not have initialStakers defined
	initialStakers, hasStakers := genesis["initialStakers"]
	stakersSlice, isSlice := initialStakers.([]interface{})
	if !hasStakers || !isSlice || len(stakersSlice) == 0 {
		// Generate initialStakers from embedded validator keys
		var generatedStakers []map[string]interface{}
		for i := uint32(0); i < numEmbeddedToUse; i++ {
			embeddedConfig := defaultNetworkConfig.NodeConfigs[i]

			// Get NodeID from staking cert
			nodeID, err := utils.ToNodeID([]byte(embeddedConfig.StakingKey), []byte(embeddedConfig.StakingCert))
			if err != nil {
				return network.Config{}, fmt.Errorf("failed to get NodeID for node %d: %w", i, err)
			}

			// Get BLS key and compute ProofOfPossession
			blsKeyBytes, err := base64.StdEncoding.DecodeString(embeddedConfig.StakingSigningKey)
			if err != nil {
				return network.Config{}, fmt.Errorf("failed to decode BLS key for node %d: %w", i, err)
			}
			blsSecretKey, err := bls.SecretKeyFromBytes(blsKeyBytes)
			if err != nil {
				return network.Config{}, fmt.Errorf("failed to parse BLS key for node %d: %w", i, err)
			}
			pop, err := signer.NewProofOfPossession(blsSecretKey)
			if err != nil {
				return network.Config{}, fmt.Errorf("failed to create ProofOfPossession for node %d: %w", i, err)
			}

			// Create staker entry - use a reward address derived from the first key or a treasury address
			// For simplicity, use the X-chain address from the first allocation or default
			staker := map[string]interface{}{
				"nodeID":        nodeID.String(),
				"rewardAddress": "P-test1yljhuvjkmtu0y5ls6kf4exsdd8gea9mp7faxl2", // Default testnet treasury
				"delegationFee": uint32(20000), // 2%
				"signer": map[string]interface{}{
					"publicKey":         fmt.Sprintf("0x%x", pop.PublicKey[:]),
					"proofOfPossession": fmt.Sprintf("0x%x", pop.ProofOfPossession[:]),
				},
			}
			generatedStakers = append(generatedStakers, staker)
		}
		genesis["initialStakers"] = generatedStakers
		fmt.Printf("🔧 Generated %d initialStakers from embedded validator keys\n", len(generatedStakers))
	}

	// Set initialStakedFunds to empty - allocations are for free balance, not staking
	genesis["initialStakedFunds"] = []string{}

	// Update start time to now
	now := time.Now().Unix()
	genesis["startTime"] = uint64(now)

	// Re-serialize genesis
	updatedGenesis, err := json.MarshalIndent(genesis, "", "  ")
	if err != nil {
		return network.Config{}, fmt.Errorf("failed to serialize updated genesis: %w", err)
	}
	netConfig.Genesis = string(updatedGenesis)

	// Configure node configs using embedded keys for the first numEmbeddedToUse nodes
	netConfig.NodeConfigs = make([]node.Config, numNodes)
	for i := uint32(0); i < numNodes; i++ {
		port := 9630 + int(i)*2

		if i < numEmbeddedToUse {
			// Use embedded validator keys
			embeddedConfig := defaultNetworkConfig.NodeConfigs[i]
			netConfig.NodeConfigs[i] = node.Config{
				Flags: map[string]interface{}{
					config.HTTPPortKey:    port,
					config.StakingPortKey: port + 1,
				},
				StakingKey:         embeddedConfig.StakingKey,
				StakingCert:        embeddedConfig.StakingCert,
				StakingSigningKey:  embeddedConfig.StakingSigningKey,
				IsBeacon:           true,
				ChainConfigFiles:   map[string]string{},
				UpgradeConfigFiles: map[string]string{},
				PChainConfigFiles:  map[string]string{},
			}
		} else {
			// Generate new keys for additional nodes beyond the 5 embedded ones
			stakingCert, stakingKey, err := staking.NewCertAndKeyBytes()
			if err != nil {
				return network.Config{}, fmt.Errorf("couldn't generate staking Cert/Key for node %d: %w", i, err)
			}

			blsKey, err := localsigner.New()
			if err != nil {
				return network.Config{}, fmt.Errorf("couldn't generate BLS key for node %d: %w", i, err)
			}

			netConfig.NodeConfigs[i] = node.Config{
				Flags: map[string]interface{}{
					config.HTTPPortKey:    port,
					config.StakingPortKey: port + 1,
				},
				StakingKey:         string(stakingKey),
				StakingCert:        string(stakingCert),
				StakingSigningKey:  base64.StdEncoding.EncodeToString(blsKey.ToBytes()),
				IsBeacon:           true,
				ChainConfigFiles:   map[string]string{},
				UpgradeConfigFiles: map[string]string{},
				PChainConfigFiles:  map[string]string{},
			}
		}
	}

	return netConfig, nil
}

// NewMainnetConfig creates a network config for LUX Mainnet (network ID 96369)
func NewMainnetConfig(binaryPath string, numNodes uint32) (network.Config, error) {
	return NewConfigForNetwork(binaryPath, numNodes, configs.MainnetChainID)
}

// NewTestnetConfig creates a network config for LUX Testnet (network ID 96368)
func NewTestnetConfig(binaryPath string, numNodes uint32) (network.Config, error) {
	return NewConfigForNetwork(binaryPath, numNodes, configs.TestnetChainID)
}

// NewLocalConfig creates a network config for local development (network ID 1337)
// This is equivalent to NewDefaultConfigNNodes but uses the configs package.
func NewLocalConfig(binaryPath string, numNodes uint32) (network.Config, error) {
	return NewConfigForNetwork(binaryPath, numNodes, configs.CustomID)
}

// NewCanonicalMainnetConfig creates a mainnet network config using the CANONICAL genesis bytes.
// This function loads the pre-serialized genesis.json file directly, ensuring byte-for-byte
// deterministic output. Use this to avoid "db contains invalid genesis hash" errors on restart.
//
// CRITICAL: For mainnet/testnet, always use this function or NewCanonicalTestnetConfig
// rather than functions that regenerate genesis (which causes non-deterministic bytes).
func NewCanonicalMainnetConfig(binaryPath string, numNodes uint32) (network.Config, error) {
	return newCanonicalConfig(binaryPath, numNodes, configs.MainnetID, 9630)
}

// NewCanonicalTestnetConfig creates a testnet network config using the CANONICAL genesis bytes.
// See NewCanonicalMainnetConfig for details on why this is critical.
func NewCanonicalTestnetConfig(binaryPath string, numNodes uint32) (network.Config, error) {
	return newCanonicalConfig(binaryPath, numNodes, configs.TestnetID, 9640)
}

// NewCanonicalDevnetConfig creates a devnet network config using the CANONICAL genesis bytes.
// See NewCanonicalMainnetConfig for details on why this is critical.
func NewCanonicalDevnetConfig(binaryPath string, numNodes uint32) (network.Config, error) {
	return newCanonicalConfig(binaryPath, numNodes, configs.DevnetID, 9650)
}

// NewCanonicalCustomConfig creates a custom (local) network config using the CANONICAL genesis bytes.
// See NewCanonicalMainnetConfig for details on why this is critical.
func NewCanonicalCustomConfig(binaryPath string, numNodes uint32) (network.Config, error) {
	return newCanonicalConfig(binaryPath, numNodes, configs.CustomID, 9660)
}

// newCanonicalConfig creates a network config with canonical (pre-serialized) genesis bytes
// and loads validator keys from ~/.lux/keys/node{0..n-1}/
func newCanonicalConfig(binaryPath string, numNodes uint32, networkID uint32, portBase int) (network.Config, error) {
	// Load CANONICAL genesis bytes - no parsing/re-serialization
	genesisBytes, err := configs.GetCanonicalGenesisBytes(networkID)
	if err != nil {
		return network.Config{}, fmt.Errorf("failed to load canonical genesis: %w", err)
	}

	// Start with default config
	netConfig := NewDefaultConfig(binaryPath)
	netConfig.Genesis = string(genesisBytes)

	// Load validator keys from ~/.lux/keys/
	keysDir := os.Getenv("LUX_KEYS_DIR")
	if keysDir == "" {
		keysDir = validatorKeysDir()
	}
	ks := keys.NewKeyStore(keysDir)

	// Load keys for each node
	nodeConfigs := make([]node.Config, numNodes)
	for i := uint32(0); i < numNodes; i++ {
		name := fmt.Sprintf("node%d", i)
		vk, err := ks.Load(name)
		if err != nil {
			return network.Config{}, fmt.Errorf("failed to load validator key %s from %s: %w (run 'lux key generate' first)", name, keysDir, err)
		}

		port := portBase + int(i)*2
		nodeConfigs[i] = node.Config{
			Flags: map[string]interface{}{
				config.HTTPPortKey:    port,
				config.StakingPortKey: port + 1,
			},
			StakingKey:         string(vk.StakerKey),
			StakingCert:        string(vk.StakerCert),
			StakingSigningKey:  base64.StdEncoding.EncodeToString(vk.BLSSecretKey),
			IsBeacon:           true,
			ChainConfigFiles:   map[string]string{},
			UpgradeConfigFiles: map[string]string{},
			PChainConfigFiles:  map[string]string{},
		}
		fmt.Printf("  Loaded validator %d: %s\n", i, vk.NodeID.String())
	}
	netConfig.NodeConfigs = nodeConfigs

	fmt.Printf("✅ Loaded canonical genesis for network %d with %d validators\n", networkID, numNodes)
	return netConfig, nil
}

// NewConfigForNetworkWithCustomGenesis creates a network config with a custom genesis string.
// Use this for networks not defined in the configs package or for testing.
func NewConfigForNetworkWithCustomGenesis(binaryPath string, numNodes uint32, genesisJSON string) (network.Config, error) {
	netConfig := NewDefaultConfig(binaryPath)
	netConfig.Genesis = genesisJSON

	// Handle node count
	if int(numNodes) > len(netConfig.NodeConfigs) {
		toAdd := int(numNodes) - len(netConfig.NodeConfigs)
		refNodeConfig := netConfig.NodeConfigs[len(netConfig.NodeConfigs)-1]
		refAPIPortIntf, ok := refNodeConfig.Flags[config.HTTPPortKey]
		if !ok {
			return netConfig, fmt.Errorf("could not get last standard api port from config")
		}
		refAPIPort, ok := refAPIPortIntf.(float64)
		if !ok {
			return netConfig, fmt.Errorf("expected float64 for last standard api port, got %T", refAPIPortIntf)
		}
		refStakingPortIntf, ok := refNodeConfig.Flags[config.StakingPortKey]
		if !ok {
			return netConfig, fmt.Errorf("could not get last standard staking port from config")
		}
		refStakingPort, ok := refStakingPortIntf.(float64)
		if !ok {
			return netConfig, fmt.Errorf("expected float64 for last standard staking port, got %T", refStakingPortIntf)
		}
		for i := 0; i < toAdd; i++ {
			nodeConfig := node.Config{}
			nodeConfig.Flags = maps.Clone(refNodeConfig.Flags)
			stakingCert, stakingKey, err := staking.NewCertAndKeyBytes()
			if err != nil {
				return netConfig, fmt.Errorf("couldn't generate staking Cert/Key: %w", err)
			}
			nodeConfig.StakingKey = string(stakingKey)
			nodeConfig.StakingCert = string(stakingCert)
			nodeConfig.Flags[config.HTTPPortKey] = int(refAPIPort) + (i+1)*2
			nodeConfig.Flags[config.StakingPortKey] = int(refStakingPort) + (i+1)*2
			netConfig.NodeConfigs = append(netConfig.NodeConfigs, nodeConfig)
		}
	}
	if int(numNodes) < len(netConfig.NodeConfigs) {
		netConfig.NodeConfigs = netConfig.NodeConfigs[:numNodes]
	}

	return netConfig, nil
}

// NewConfigWithPreExistingKeys creates a network config using pre-existing validator keys.
// This is useful for:
// - Maintaining consistent NodeIDs across network restarts
// - Using keys with pre-configured BLS signers
// - Deploying to mainnet/testnet with known validator identities
//
// The keysDir should contain subdirectories (e.g., node1, node2) with:
// - staker.crt and staker.key for TLS identity
// - bls/signer.key for BLS signer (optional)
// - ec/private.key for P-Chain addresses (optional)
func NewConfigWithPreExistingKeys(binaryPath string, networkID uint32, keysDir string) (network.Config, error) {
	// Determine port base based on network ID
	// Mainnet (1): 9630 base
	// Testnet (2): 9640 base
	// Devnet (3): 9650 base
	portBase := 9630
	switch networkID {
	case constants.TestnetID: // 2
		portBase = 9640
	case constants.DevnetID: // 3
		portBase = 9650
	}

	// Get genesis for the specified network
	genesisJSON, err := configs.GetGenesis(networkID)
	if err != nil {
		return network.Config{}, fmt.Errorf("failed to get genesis for network %d: %w", networkID, err)
	}

	// Load validator keys from the keys directory
	ks := keys.NewKeyStore(keysDir)
	validatorKeys, err := ks.LoadAll()
	if err != nil {
		return network.Config{}, fmt.Errorf("failed to load validator keys from %s: %w", keysDir, err)
	}

	if len(validatorKeys) == 0 {
		return network.Config{}, fmt.Errorf("no validator keys found in %s", keysDir)
	}

	// Start with the default config structure
	netConfig := NewDefaultConfig(binaryPath)

	// Parse genesis to modify it
	var genesis map[string]interface{}
	if err := json.Unmarshal(genesisJSON, &genesis); err != nil {
		return network.Config{}, fmt.Errorf("failed to parse genesis: %w", err)
	}

	// Build initial stakers from loaded keys
	hrp := constants.GetHRP(networkID)
	numNodes := uint32(len(validatorKeys))

	initialStakers := make([]map[string]interface{}, numNodes)
	for i, vk := range validatorKeys {
		rewardAddr, err := address.Format("P", hrp, vk.PChainAddr[:])
		if err != nil {
			return network.Config{}, fmt.Errorf("couldn't format reward address for node %d: %w", i, err)
		}

		staker := map[string]interface{}{
			"nodeID":        vk.NodeID.String(),
			"rewardAddress": rewardAddr,
			"delegationFee": 20000,   // 2% delegation fee
			"weight":        GigaLux, // 1,000,000,000 LUX (1B) per validator
		}

		// Add BLS signer if available
		if len(vk.BLSPublicKey) > 0 && len(vk.BLSPoP) > 0 {
			staker["signer"] = map[string]interface{}{
				"publicKey":         vk.BLSPublicKeyHex(),
				"proofOfPossession": vk.BLSPoPHex(),
			}
		}

		initialStakers[i] = staker
	}

	// Update genesis with initial stakers (our validator NodeIDs)
	genesis["initialStakers"] = initialStakers

	// Generate allocations from validator keys (rather than expecting pre-existing allocations)
	// Each validator gets both X-chain and P-chain allocations
	// X-chain: 1B LUX immediately available
	// P-chain: 1B LUX with 1% unlock per year for 100 years starting Jan 1 2020
	allocations := make([]interface{}, 0, numNodes*2)

	// Constants for P-chain unlock schedule
	const (
		oneGigaLux       = uint64(1000000000000000000) // 1B LUX in nLUX
		onePercentPerYear = oneGigaLux / 100           // 1% = 10M LUX
		jan1_2020        = uint64(1577836800)          // Unix timestamp
		oneYear          = uint64(31536000)            // seconds
	)

	for i, vk := range validatorKeys {
		// Build P-chain address
		pChainAddr, err := address.Format("P", hrp, vk.PChainAddr[:])
		if err != nil {
			return network.Config{}, fmt.Errorf("failed to format P-chain address for validator %d: %w", i, err)
		}
		// Build X-chain address
		xChainAddr, err := address.Format("X", hrp, vk.PChainAddr[:])
		if err != nil {
			return network.Config{}, fmt.Errorf("failed to format X-chain address for validator %d: %w", i, err)
		}

		// X-chain allocation (1B LUX) - immediately available
		allocations = append(allocations, map[string]interface{}{
			"ethAddr":        vk.CChainAddrHex(),
			"luxAddr":        xChainAddr,
			"initialAmount":  oneGigaLux,
			"unlockSchedule": []map[string]interface{}{},
		})

		// P-chain allocation (1B LUX) - 1% per year for 100 years
		pchainUnlockSchedule := make([]map[string]interface{}, 100)
		for year := 0; year < 100; year++ {
			pchainUnlockSchedule[year] = map[string]interface{}{
				"amount":   onePercentPerYear,
				"locktime": jan1_2020 + (oneYear * uint64(year)),
			}
		}
		allocations = append(allocations, map[string]interface{}{
			"ethAddr":        vk.CChainAddrHex(),
			"luxAddr":        pChainAddr,
			"initialAmount":  uint64(0),
			"unlockSchedule": pchainUnlockSchedule,
		})

		fmt.Printf("  Validator %d: %s -> X:%s P:%s (1B each)\n", i+1, vk.NodeID.String(), xChainAddr, pChainAddr)
	}
	genesis["allocations"] = allocations

	// Set initialStakedFunds to EMPTY - we use explicit weight in initialStakers
	genesis["initialStakedFunds"] = []string{}

	// Check for LUX_MNEMONIC and add mnemonic-derived allocation with FREE funds
	// This enables subnet creation without needing the hardcoded treasury key
	// IMPORTANT: Add this AFTER setting initialStakedFunds so mnemonic funds are NOT staked
	if mnemonic := os.Getenv("LUX_MNEMONIC"); mnemonic != "" {
		fmt.Printf("🔑 LUX_MNEMONIC is set, adding mnemonic allocation to genesis\n")
		mnemonicAlloc := map[string]interface{}{
			"ethAddr":       "0x0406d56943a38ad8398a738527f27e2cf01731a8",
			"luxAddr":       "P-lux1qsrd262r5w9dswv2wwzj0un79ncpwvdgkpqzqu",
			"initialAmount": 0,
			"unlockSchedule": []map[string]interface{}{
				{
					"amount":   uint64(100000000000000), // 100,000 LUX
					"locktime": uint64(0),
				},
			},
		}

		// Append mnemonic allocation directly to our allocations slice
		fmt.Printf("   Adding mnemonic allocation (NOT staked) to %d existing allocations\n", len(allocations))
		allocations = append(allocations, mnemonicAlloc)
		genesis["allocations"] = allocations
	} else {
		fmt.Printf("⚠️  LUX_MNEMONIC not set, skipping mnemonic allocation\n")
	}

	// Check for LUX_PRIVATE_KEY and add allocations with correct HRP for this network
	// This enables deploy operations when using a specific private key
	if privKeyHex := os.Getenv("LUX_PRIVATE_KEY"); privKeyHex != "" {
		privKeyBytes, err := hex.DecodeString(privKeyHex)
		if err == nil && len(privKeyBytes) == 32 {
			// Derive address from private key
			luxPrivKey, err := luxcrypto.ToPrivateKey(privKeyBytes)
			if err == nil {
				pubKey := luxPrivKey.PublicKey()
				pChainAddr := ids.ShortID(pubKey.Address())

				// Format addresses with the correct HRP for this network
				xLuxAddr, errX := address.Format("X", hrp, pChainAddr[:])
				pLuxAddr, errP := address.Format("P", hrp, pChainAddr[:])
				if errX == nil && errP == nil {
					ethAddr := "0x" + hex.EncodeToString(pChainAddr[:])
					fmt.Printf("🔑 Adding LUX_PRIVATE_KEY allocations: X=%s, P=%s (2B each)\n", xLuxAddr, pLuxAddr)

					// X-chain allocation (2B LUX) - immediately available
					allocations = append(allocations, map[string]interface{}{
						"ethAddr":        ethAddr,
						"luxAddr":        xLuxAddr,
						"initialAmount":  uint64(2000000000000000000), // 2B LUX
						"unlockSchedule": []map[string]interface{}{},
					})

					// P-chain allocation (2B LUX) - immediately available for subnet creation
					allocations = append(allocations, map[string]interface{}{
						"ethAddr":       ethAddr,
						"luxAddr":       pLuxAddr,
						"initialAmount": uint64(0),
						"unlockSchedule": []map[string]interface{}{
							{
								"amount":   uint64(2000000000000000000), // 2B LUX
								"locktime": uint64(0),                   // Immediately available
							},
						},
					})
					genesis["allocations"] = allocations

					// Add to initialStakedFunds for P-chain access
					initialStakedFunds, _ := genesis["initialStakedFunds"].([]string)
					initialStakedFunds = append(initialStakedFunds, xLuxAddr)
					genesis["initialStakedFunds"] = initialStakedFunds
					fmt.Printf("🔑 Added %s to initialStakedFunds for P-chain access\n", xLuxAddr)
				}
			}
		}
	}

	// Update start time to now
	now := time.Now().Unix()
	genesis["startTime"] = uint64(now)

	// Re-serialize genesis
	updatedGenesis, err := json.MarshalIndent(genesis, "", "  ")
	if err != nil {
		return network.Config{}, fmt.Errorf("failed to serialize updated genesis: %w", err)
	}
	netConfig.Genesis = string(updatedGenesis)

	// Debug: write genesis to file for inspection
	if err := os.WriteFile("/tmp/debug_genesis.json", updatedGenesis, 0644); err != nil {
		fmt.Printf("Warning: could not write debug genesis: %v\n", err)
	} else {
		fmt.Printf("📄 Genesis written to /tmp/debug_genesis.json\n")
	}

	// Configure node configs with the loaded staking keys
	netConfig.NodeConfigs = make([]node.Config, numNodes)
	for i, vk := range validatorKeys {
		port := portBase + int(i)*2
		netConfig.NodeConfigs[i] = node.Config{
			Flags: map[string]interface{}{
				config.HTTPPortKey:    port,
				config.StakingPortKey: port + 1,
			},
			StakingKey:        string(vk.StakerKey),
			StakingCert:       string(vk.StakerCert),
			StakingSigningKey: base64.StdEncoding.EncodeToString(vk.BLSSecretKey),
			IsBeacon:          true,
			ChainConfigFiles:  map[string]string{},
			UpgradeConfigFiles: map[string]string{},
			PChainConfigFiles: map[string]string{},
		}
	}

	return netConfig, nil
}

// DefaultKeysPath returns the default path for pre-existing validator keys
func DefaultKeysPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".lux", "keys")
}

// NewMainnetConfigWithKeys creates a mainnet config using pre-existing validator keys
func NewMainnetConfigWithKeys(binaryPath string, keysDir string) (network.Config, error) {
	if keysDir == "" {
		keysDir = DefaultKeysPath()
	}
	return NewConfigWithPreExistingKeys(binaryPath, constants.MainnetID, keysDir)
}

// NewTestnetConfigWithKeys creates a testnet config using pre-existing validator keys
func NewTestnetConfigWithKeys(binaryPath string, keysDir string) (network.Config, error) {
	if keysDir == "" {
		keysDir = DefaultKeysPath()
	}
	return NewConfigWithPreExistingKeys(binaryPath, constants.TestnetID, keysDir)
}

// validatorKeysDir returns the directory path for persisted validator keys
func validatorKeysDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".lux", "keys")
}

// NewConfigFromMnemonic creates a network config by deriving validator keys from LUX_MNEMONIC.
// This is the preferred method for starting mainnet/testnet.
//
// IMPORTANT: Keys are derived from mnemonic ONCE and persisted to disk (~/.lux/netrunner-validators/).
// On subsequent runs, keys are loaded from disk to maintain stable NodeIDs.
// This follows Avalanche's pattern where identity = persistent staking keys.
//
// The mnemonic is used to derive:
// - EC keys (for P-chain allocations) - deterministic from mnemonic
// - TLS staking certs (for NodeID) - generated once, then persisted
// - BLS keys (for consensus) - deterministic from mnemonic
func NewConfigFromMnemonic(binaryPath string, networkID uint32, numNodes uint32) (network.Config, error) {
	fmt.Println(">>> ENTERED NewConfigFromMnemonic <<<")
	mnemonic := os.Getenv("LUX_MNEMONIC")
	if mnemonic == "" {
		return network.Config{}, fmt.Errorf("LUX_MNEMONIC environment variable not set")
	}

	// Check if persisted validator keys exist
	keysDir := os.Getenv("LUX_KEYS_DIR")
	if keysDir == "" {
		keysDir = validatorKeysDir()
	}
	ks := keys.NewKeyStore(keysDir)

	var validatorKeys []*keys.ValidatorKey
	var err error

	// Try to load existing keys in order: node0, node1, ..., node{n-1}
	// Keys must have EC private key (not just TLS certs) to be considered valid
	validatorKeys = make([]*keys.ValidatorKey, numNodes)
	allExist := true
	for i := uint32(0); i < numNodes; i++ {
		name := fmt.Sprintf("node%d", i)
		vk, err := ks.Load(name)
		if err != nil || vk == nil {
			allExist = false
			break
		}
		// Verify EC key exists - LoadFromDir auto-generates TLS certs but not EC keys
		if len(vk.ECPrivateKey) == 0 {
			fmt.Printf("   %s: missing EC private key, will re-derive from mnemonic\n", name)
			allExist = false
			break
		}
		validatorKeys[i] = vk
	}

	if allExist {
		fmt.Printf("🔴 DEBUG_MARKER: allExist=true\n")
		fmt.Printf("🔑 Loading %d validators from %s (stable NodeIDs)...\n", numNodes, keysDir)
		for i, vk := range validatorKeys {
			fmt.Printf("   node%d: %s\n", i, vk.NodeID.String())
		}
	} else {
		// No existing keys - derive from mnemonic and persist
		fmt.Printf("🔑 Deriving %d validators from LUX_MNEMONIC (first run)...\n", numNodes)

		validatorKeys, err = keys.DeriveValidatorsFromMnemonic(mnemonic, int(numNodes))
		if err != nil {
			return network.Config{}, fmt.Errorf("failed to derive validator keys: %w", err)
		}

		// Persist keys to disk for stable NodeIDs on future runs
		fmt.Printf("📁 Persisting validator keys to %s...\n", keysDir)
		for i, vk := range validatorKeys {
			name := fmt.Sprintf("node%d", i)
			if err := ks.Save(name, vk); err != nil {
				return network.Config{}, fmt.Errorf("failed to save validator key %d: %w", i, err)
			}
			fmt.Printf("   Saved %s: NodeID=%s\n", name, vk.NodeID.String())
		}
	}

	fmt.Println("🔍 DEBUG: About to derive wallet key from mnemonic...")
	// CRITICAL: Always derive the wallet key from mnemonic and ensure it has allocations.
	// The wallet (used by deploy, fundPChainFromXChain, etc.) uses keys.DeriveValidatorFromMnemonic(mnemonic, 0),
	// which may differ from validators loaded from ~/.lux/keys.
	walletKey, err := keys.DeriveValidatorFromMnemonic(mnemonic, 0)
	if err != nil {
		return network.Config{}, fmt.Errorf("failed to derive wallet key from mnemonic: %w", err)
	}

	// Check if wallet key is different from all validator keys
	walletNeedsAllocation := true
	for _, vk := range validatorKeys {
		if vk.PChainAddr == walletKey.PChainAddr {
			walletNeedsAllocation = false
			fmt.Printf("🔑 Wallet key matches validator key (no extra allocation needed)\n")
			break
		}
	}
	if walletNeedsAllocation {
		fmt.Printf("🔑 Wallet key differs from validators - adding allocation for %s\n", walletKey.PChainAddr.String())
	}

	// Get base genesis
	genesisJSON, err := configs.GetGenesis(networkID)
	if err != nil {
		return network.Config{}, fmt.Errorf("failed to get genesis for network %d: %w", networkID, err)
	}

	// Start with default config
	netConfig := NewDefaultConfig(binaryPath)

	// Parse genesis to modify it
	var genesis map[string]interface{}
	if err := json.Unmarshal(genesisJSON, &genesis); err != nil {
		return network.Config{}, fmt.Errorf("failed to parse genesis: %w", err)
	}

	hrp := constants.GetHRP(networkID)

	// Build initial stakers from derived keys
	initialStakers := make([]map[string]interface{}, numNodes)
	// Each validator gets BOTH X-chain AND P-chain allocations (2 entries per validator)
	allocations := make([]interface{}, 0, numNodes*2)

	for i, vk := range validatorKeys {
		fmt.Printf("🔍 Validator %d: PChainAddr ShortID = %s\n", i, vk.PChainAddr.String())

		// Build P-chain address for staker rewards and P-chain operations
		pChainAddr, err := address.Format("P", hrp, vk.PChainAddr[:])
		if err != nil {
			return network.Config{}, fmt.Errorf("failed to format P-chain address for validator %d: %w", i, err)
		}
		fmt.Printf("🔍 Validator %d: P-chain bech32 = %s\n", i, pChainAddr)

		// Build X-chain address for asset transfers
		xChainAddr, err := address.Format("X", hrp, vk.PChainAddr[:])
		if err != nil {
			return network.Config{}, fmt.Errorf("failed to format X-chain address for validator %d: %w", i, err)
		}
		fmt.Printf("🔍 Validator %d: X-chain bech32 = %s\n", i, xChainAddr)

		// Initial staker entry
		staker := map[string]interface{}{
			"nodeID":        vk.NodeID.String(),
			"rewardAddress": xChainAddr, // Rewards go to X-chain address
			"delegationFee": 20000,       // 2% delegation fee
			"weight":        GigaLux,     // 1B LUX per validator
		}
		if len(vk.BLSPublicKey) > 0 && len(vk.BLSPoP) > 0 {
			staker["signer"] = map[string]interface{}{
				"publicKey":         vk.BLSPublicKeyHex(),
				"proofOfPossession": vk.BLSPoPHex(),
			}
		}
		initialStakers[i] = staker

		// X-chain allocation (2B LUX) - for asset transfers
		allocations = append(allocations, map[string]interface{}{
			"ethAddr":        vk.CChainAddrHex(),
			"luxAddr":        xChainAddr,
			"initialAmount":  uint64(2000000000000000000), // 2B LUX
			"unlockSchedule": []map[string]interface{}{},
		})

		// P-chain allocation (2B LUX) - for chain creation, validators
		// P-chain allocation - MUST use unlockSchedule for P-chain UTXOs
		// The builder processes unlockSchedule for P-chain, initialAmount for X-chain
		allocations = append(allocations, map[string]interface{}{
			"ethAddr":       vk.CChainAddrHex(),
			"luxAddr":       pChainAddr,
			"initialAmount": uint64(0),
			"unlockSchedule": []map[string]interface{}{
				{
					"amount":   uint64(2000000000000000000), // 2B LUX
					"locktime": uint64(0),                   // locktime 0 = immediately unlocked
				},
			},
		})

		fmt.Printf("  Validator %d: %s -> X:%s P:%s (2B each)\n", i+1, vk.NodeID.String(), xChainAddr, pChainAddr)
	}

	// Add wallet key allocation if different from validators
	if walletNeedsAllocation {
		// Build X-chain address for wallet
		walletXAddr, err := address.Format("X", hrp, walletKey.PChainAddr[:])
		if err != nil {
			return network.Config{}, fmt.Errorf("failed to format wallet X-chain address: %w", err)
		}
		// Build P-chain address for wallet
		walletPAddr, err := address.Format("P", hrp, walletKey.PChainAddr[:])
		if err != nil {
			return network.Config{}, fmt.Errorf("failed to format wallet P-chain address: %w", err)
		}

		// X-chain allocation for wallet (2B LUX)
		allocations = append(allocations, map[string]interface{}{
			"ethAddr":        walletKey.CChainAddrHex(),
			"luxAddr":        walletXAddr,
			"initialAmount":  uint64(2000000000000000000), // 2B LUX
			"unlockSchedule": []map[string]interface{}{},
		})

		// P-chain allocation for wallet (2B LUX) - MUST use unlockSchedule
		allocations = append(allocations, map[string]interface{}{
			"ethAddr":       walletKey.CChainAddrHex(),
			"luxAddr":       walletPAddr,
			"initialAmount": uint64(0),
			"unlockSchedule": []map[string]interface{}{
				{
					"amount":   uint64(2000000000000000000), // 2B LUX
					"locktime": uint64(0),                   // locktime 0 = immediately unlocked
				},
			},
		})

		fmt.Printf("  Wallet: %s -> X:%s P:%s (2B each)\n", walletKey.PChainAddr.String(), walletXAddr, walletPAddr)
	}

	// Also add allocations for LUX_PRIVATE_KEY if set and different from wallet key
	// This is needed when getDefaultKey() in blockchain.go uses LUX_PRIVATE_KEY
	if privKeyHex := os.Getenv("LUX_PRIVATE_KEY"); privKeyHex != "" {
		privKeyBytes, err := hex.DecodeString(privKeyHex)
		if err == nil && len(privKeyBytes) == 32 {
			luxPrivKey, err := luxcrypto.ToPrivateKey(privKeyBytes)
			if err == nil {
				pubKey := luxPrivKey.PublicKey()
				privKeyAddr := ids.ShortID(pubKey.Address())

				// Check if this is different from wallet key
				if privKeyAddr != walletKey.PChainAddr {
					privKeyXAddr, errX := address.Format("X", hrp, privKeyAddr[:])
					privKeyPAddr, errP := address.Format("P", hrp, privKeyAddr[:])
					if errX == nil && errP == nil {
						fmt.Printf("🔑 Adding LUX_PRIVATE_KEY allocations (with %s HRP): X=%s P=%s (2B each)\n", hrp, privKeyXAddr, privKeyPAddr)
						ethAddr := "0x" + hex.EncodeToString(privKeyAddr[:])

						// X-chain allocation
						allocations = append(allocations, map[string]interface{}{
							"ethAddr":        ethAddr,
							"luxAddr":        privKeyXAddr,
							"initialAmount":  uint64(2000000000000000000), // 2B LUX
							"unlockSchedule": []map[string]interface{}{},
						})

						// P-chain allocation - MUST use unlockSchedule
						allocations = append(allocations, map[string]interface{}{
							"ethAddr":       ethAddr,
							"luxAddr":       privKeyPAddr,
							"initialAmount": uint64(0),
							"unlockSchedule": []map[string]interface{}{
								{
									"amount":   uint64(2000000000000000000), // 2B LUX
									"locktime": uint64(0),                   // locktime 0 = immediately unlocked
								},
							},
						})
					}
				} else {
					fmt.Printf("🔑 LUX_PRIVATE_KEY matches wallet key, no extra allocation needed\n")
				}
			}
		}
	}

	genesis["initialStakers"] = initialStakers
	genesis["allocations"] = allocations

	// CRITICAL: Also update xChainGenesis.allocations!
	// The X-chain reads allocations from the embedded xChainGenesis JSON string,
	// NOT from the top-level allocations array. We must update both.
	if xChainGenesisStr, ok := genesis["xChainGenesis"].(string); ok {
		var xChainGenesis map[string]interface{}
		if err := json.Unmarshal([]byte(xChainGenesisStr), &xChainGenesis); err == nil {
			// Build X-chain specific allocations (only X-chain addresses, use avaxAddr format)
			xAllocations := make([]map[string]interface{}, 0)
			for _, alloc := range allocations {
				allocMap := alloc.(map[string]interface{})
				luxAddr, _ := allocMap["luxAddr"].(string)
				// Only include X-chain allocations (start with "X-")
				if strings.HasPrefix(luxAddr, "X-") {
					initialAmount, _ := allocMap["initialAmount"].(uint64)
					if initialAmount > 0 {
						xAllocations = append(xAllocations, map[string]interface{}{
							"avaxAddr":       luxAddr, // X-chain uses avaxAddr field name
							"ethAddr":        allocMap["ethAddr"],
							"initialAmount":  initialAmount,
							"unlockSchedule": []interface{}{},
						})
					}
				}
			}
			xChainGenesis["allocations"] = xAllocations
			if updatedXChain, err := json.Marshal(xChainGenesis); err == nil {
				genesis["xChainGenesis"] = string(updatedXChain)
				fmt.Printf("✅ Updated xChainGenesis with %d allocations\n", len(xAllocations))
			}
		}
	}

	// IMPORTANT: Set initialStakedFunds to EMPTY so that P-chain allocations
	// are NOT filtered by the builder. The builder filters allocations where
	// the underlying ShortID matches initialStakedFunds, which would incorrectly
	// filter P-chain allocations that share the same address as X-chain validators.
	// We use explicit weight in initialStakers instead of deriving from stakes.
	genesis["initialStakedFunds"] = []string{}

	// Update start time to now
	now := time.Now().Unix()
	genesis["startTime"] = uint64(now)

	// Re-serialize genesis
	updatedGenesis, err := json.MarshalIndent(genesis, "", "  ")
	if err != nil {
		return network.Config{}, fmt.Errorf("failed to serialize genesis: %w", err)
	}
	netConfig.Genesis = string(updatedGenesis)

	// Debug: write genesis to file
	if err := os.WriteFile("/tmp/mnemonic_genesis.json", updatedGenesis, 0644); err != nil {
		fmt.Printf("Warning: could not write debug genesis: %v\n", err)
	} else {
		fmt.Printf("📄 Genesis written to /tmp/mnemonic_genesis.json\n")
	}

	// Configure node configs with derived keys
	// Start with default flags from netConfig.Flags and add per-node overrides
	netConfig.NodeConfigs = make([]node.Config, numNodes)
	for i, vk := range validatorKeys {
		port := 9630 + int(i)*2
		// Copy default network flags to each node
		nodeFlags := make(map[string]interface{})
		for k, v := range netConfig.Flags {
			nodeFlags[k] = v
		}
		// Add per-node specific flags
		nodeFlags[config.HTTPPortKey] = port
		nodeFlags[config.StakingPortKey] = port + 1
		// Enable sybil protection for mainnet - validators have proper keys
		// The genesis validators have matching NodeIDs from deterministic TLS certs
		nodeFlags[config.SybilProtectionEnabledKey] = true

		netConfig.NodeConfigs[i] = node.Config{
			Flags:              nodeFlags,
			StakingKey:         string(vk.StakerKey),
			StakingCert:        string(vk.StakerCert),
			StakingSigningKey:  base64.StdEncoding.EncodeToString(vk.BLSSecretKey),
			IsBeacon:           true,
			ChainConfigFiles:   map[string]string{},
			UpgradeConfigFiles: map[string]string{},
			PChainConfigFiles:  map[string]string{},
			RedirectStdout:     true,
			RedirectStderr:     true,
		}
	}

	fmt.Printf("✅ Network config ready with %d validators\n", numNodes)
	return netConfig, nil
}

// NewMainnetConfigFromMnemonic creates mainnet config from LUX_MNEMONIC
func NewMainnetConfigFromMnemonic(binaryPath string, numNodes uint32) (network.Config, error) {
	return NewConfigFromMnemonic(binaryPath, constants.MainnetID, numNodes)
}

// NewTestnetConfigFromMnemonic creates testnet config from LUX_MNEMONIC
func NewTestnetConfigFromMnemonic(binaryPath string, numNodes uint32) (network.Config, error) {
	return NewConfigFromMnemonic(binaryPath, constants.TestnetID, numNodes)
}

// NewLocalConfigFromMnemonic creates local network config from LUX_MNEMONIC
// This uses network ID 1337 with "custom" HRP, which is simpler for testing
func NewLocalConfigFromMnemonic(binaryPath string, numNodes uint32) (network.Config, error) {
	return NewConfigFromMnemonic(binaryPath, configs.CustomID, numNodes)
}
