// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package local

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"maps"

	constants "github.com/luxfi/const"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/bls/signer/localsigner"
	luxcrypto "github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/genesis/configs"
	"github.com/luxfi/ids"
	"github.com/luxfi/keys"
	"github.com/luxfi/netrunner/network"
	"github.com/luxfi/netrunner/network/node"
	"github.com/luxfi/netrunner/utils"
	"github.com/luxfi/node/config"
	"github.com/luxfi/node/vms/platformvm/signer"

	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/utils/formatting/address"
)

// patchGenesisPreservingRaw patches a top-level genesis JSON document while guaranteeing that
// selected keys are preserved byte-for-byte as raw JSON values (including quotes/escapes for strings).
//
// This is the correct approach for EVM genesis fields (C-Chain + any subnet EVM genesis) because
// parsing via interface{} will corrupt large numeric fields and/or change encoding.
//
// CRITICAL: Never use map[string]interface{} for genesis - it causes float64 precision loss!
func patchGenesisPreservingRaw(
	in []byte,
	preserveKeys []string,
	mutate func(m map[string]json.RawMessage) error,
) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(in, &m); err != nil {
		return nil, fmt.Errorf("unmarshal genesis envelope: %w", err)
	}

	// Snapshot preserved raw values.
	preserved := make(map[string]json.RawMessage, len(preserveKeys))
	for _, k := range preserveKeys {
		if v, ok := m[k]; ok {
			// deep copy
			preserved[k] = append(json.RawMessage(nil), v...)
		}
	}

	// Apply edits.
	if err := mutate(m); err != nil {
		return nil, err
	}

	// Restore preserved values (even if mutate touched them by accident).
	for k, v := range preserved {
		m[k] = v
	}

	// Marshal output.
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal genesis envelope: %w", err)
	}

	// Sanity check: ensure preserved keys are still byte-identical.
	var m2 map[string]json.RawMessage
	if err := json.Unmarshal(out, &m2); err != nil {
		return nil, fmt.Errorf("re-unmarshal genesis envelope: %w", err)
	}
	for k, v := range preserved {
		if vv, ok := m2[k]; ok && !bytes.Equal(vv, v) {
			return nil, fmt.Errorf("FATAL: preserved genesis key %q mutated (byte mismatch)", k)
		}
	}

	return out, nil
}

// mustJSON is a helper for assigning scalars/structs back into RawMessage maps.
func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// evmGenesisKeys are the keys that MUST be preserved byte-for-byte in genesis.
// These correspond to EVM chain genesis configurations.
var evmGenesisKeys = []string{
	"cChainGenesis",
	// Add subnet EVM genesis keys if embedded in main genesis:
	// "hanzoGenesis", "spcGenesis", "zooGenesis",
}

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
	//
	// CRITICAL: Use patchGenesisPreservingRaw to ensure cChainGenesis bytes are NEVER modified.
	// Parsing via map[string]interface{} corrupts large numerics (float64 precision loss).

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

	// Patch genesis using RawMessage to preserve cChainGenesis byte-exact
	updatedGenesis, err := patchGenesisPreservingRaw(genesisJSON, evmGenesisKeys, func(m map[string]json.RawMessage) error {
		// Build P/X-chain allocations for wallet keys
		var newAllocations []map[string]interface{}

		// If LUX_MNEMONIC is set, create allocations for mnemonic-derived key
		if mnemonic := os.Getenv("LUX_MNEMONIC"); mnemonic != "" {
			validatorKeys, err := keys.DeriveValidatorsFromMnemonic(mnemonic, 1)
			if err == nil && len(validatorKeys) > 0 {
				vk := validatorKeys[0]
				addr := vk.PChainAddr
				xLuxAddr, errX := address.Format("X", hrp, addr[:])
				pLuxAddr, errP := address.Format("P", hrp, addr[:])
				if errX == nil && errP == nil {
					fmt.Printf("🔑 Setting X+P allocations for LUX_MNEMONIC: X=%s, P=%s (2B each)\n", xLuxAddr, pLuxAddr)
					// X-chain allocation
					newAllocations = append(newAllocations, map[string]interface{}{
						"ethAddr":        vk.CChainAddrHex(),
						"luxAddr":        xLuxAddr,
						"initialAmount":  uint64(2000000000000000000),
						"unlockSchedule": []interface{}{},
					})
					// P-chain allocation
					newAllocations = append(newAllocations, map[string]interface{}{
						"ethAddr":       vk.CChainAddrHex(),
						"luxAddr":       pLuxAddr,
						"initialAmount": uint64(0),
						"unlockSchedule": []map[string]interface{}{
							{"amount": uint64(2000000000000000000), "locktime": uint64(0)},
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
						newAllocations = append(newAllocations, map[string]interface{}{
							"ethAddr":        ethAddr,
							"luxAddr":        xLuxAddr,
							"initialAmount":  uint64(2000000000000000000),
							"unlockSchedule": []interface{}{},
						})
						newAllocations = append(newAllocations, map[string]interface{}{
							"ethAddr":       ethAddr,
							"luxAddr":       pLuxAddr,
							"initialAmount": uint64(0),
							"unlockSchedule": []map[string]interface{}{
								{"amount": uint64(2000000000000000000), "locktime": uint64(0)},
							},
						})
					}
				}
			}
		}

		// Update allocations if we have new ones
		if len(newAllocations) > 0 {
			m["allocations"] = mustJSON(newAllocations)
		}

		// ALWAYS replace initialStakers with embedded validator keys for local network testing.
		// The genesis file may have different validators than netrunner's embedded keys,
		// so we must replace them to ensure nodes can actually validate.
		{
			// Generate initialStakers from embedded validator keys
			var generatedStakers []map[string]interface{}
			for i := uint32(0); i < numEmbeddedToUse; i++ {
				embeddedConfig := defaultNetworkConfig.NodeConfigs[i]

				nodeID, err := utils.ToNodeID([]byte(embeddedConfig.StakingKey), []byte(embeddedConfig.StakingCert))
				if err != nil {
					return fmt.Errorf("failed to get NodeID for node %d: %w", i, err)
				}

				blsKeyBytes, err := base64.StdEncoding.DecodeString(embeddedConfig.StakingSigningKey)
				if err != nil {
					return fmt.Errorf("failed to decode BLS key for node %d: %w", i, err)
				}
				blsSecretKey, err := bls.SecretKeyFromBytes(blsKeyBytes)
				if err != nil {
					return fmt.Errorf("failed to parse BLS key for node %d: %w", i, err)
				}
				pop, err := signer.NewProofOfPossession(blsSecretKey)
				if err != nil {
					return fmt.Errorf("failed to create ProofOfPossession for node %d: %w", i, err)
				}

				staker := map[string]interface{}{
					"nodeID":        nodeID.String(),
					"rewardAddress": "P-test1yljhuvjkmtu0y5ls6kf4exsdd8gea9mp7faxl2",
					"delegationFee": uint32(20000),
					"signer": map[string]interface{}{
						"publicKey":         fmt.Sprintf("0x%x", pop.PublicKey[:]),
						"proofOfPossession": fmt.Sprintf("0x%x", pop.ProofOfPossession[:]),
					},
				}
				generatedStakers = append(generatedStakers, staker)
			}
			m["initialStakers"] = mustJSON(generatedStakers)
			fmt.Printf("🔧 Generated %d initialStakers from embedded validator keys\n", len(generatedStakers))
		}

		// Set initialStakedFunds to empty
		m["initialStakedFunds"] = mustJSON([]string{})

		// Update start time to now
		now := time.Now().Unix()
		m["startTime"] = mustJSON(uint64(now))

		return nil
	})
	if err != nil {
		return network.Config{}, fmt.Errorf("failed to patch genesis: %w", err)
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
//
// IMPORTANT: This function patches the genesis initialStakers with the BLS keys from the
// loaded validator keys. This ensures the validators' BLS Proof of Possession (PoP) in genesis
// matches the actual signer.key files the nodes will use.
func newCanonicalConfig(binaryPath string, numNodes uint32, networkID uint32, portBase int) (network.Config, error) {
	// Load CANONICAL genesis bytes - no parsing/re-serialization
	originalGenesisBytes, err := configs.GetCanonicalGenesisBytes(networkID)
	if err != nil {
		return network.Config{}, fmt.Errorf("failed to load canonical genesis: %w", err)
	}

	// Load validator keys from ~/.lux/keys/ FIRST so we can patch genesis with them
	keysDir := os.Getenv("LUX_KEYS_DIR")
	if keysDir == "" {
		keysDir = validatorKeysDir()
	}
	ks := keys.NewKeyStore(keysDir)

	// Load all validator keys and build initialStakers
	hrp := constants.GetHRP(networkID)
	validatorKeys := make([]*keys.ValidatorKey, numNodes)
	initialStakers := make([]map[string]interface{}, numNodes)

	for i := uint32(0); i < numNodes; i++ {
		name := fmt.Sprintf("node%d", i)
		vk, err := ks.Load(name)
		if err != nil {
			return network.Config{}, fmt.Errorf("failed to load validator key %s from %s: %w (run 'lux key generate' first)", name, keysDir, err)
		}
		validatorKeys[i] = vk

		// Build initialStaker entry with BLS keys from loaded validator
		rewardAddr, err := address.Format("P", hrp, vk.PChainAddr[:])
		if err != nil {
			return network.Config{}, fmt.Errorf("couldn't format reward address for node %d: %w", i, err)
		}

		staker := map[string]interface{}{
			"nodeID":        vk.NodeID.String(),
			"rewardAddress": rewardAddr,
			"delegationFee": 20000,
			"weight":        GigaLux, // 1 billion nLUX = 1000 LUX
		}

		if len(vk.BLSPublicKey) > 0 && len(vk.BLSPoP) > 0 {
			staker["signer"] = map[string]interface{}{
				"publicKey":         vk.BLSPublicKeyHex(),
				"proofOfPossession": vk.BLSPoPHex(),
			}
			fmt.Printf("  Validator %d: %s (BLS: %s...)\n", i, vk.NodeID.String(), vk.BLSPublicKeyHex()[:20])
		} else {
			fmt.Printf("  Validator %d: %s (no BLS key)\n", i, vk.NodeID.String())
		}

		initialStakers[i] = staker
	}

	// For LOCAL TESTING: Update startTime to now AND patch initialStakers with our keys
	// CRITICAL: Use patchGenesisPreservingRaw to preserve cChainGenesis exact bytes.
	now := time.Now().Unix()
	fmt.Printf("📅 Updated genesis startTime to %d (now) for local testing\n", now)
	fmt.Printf("🔑 Patching genesis initialStakers with %d validators from %s\n", numNodes, keysDir)

	genesisBytes, err := patchGenesisPreservingRaw(originalGenesisBytes, evmGenesisKeys, func(m map[string]json.RawMessage) error {
		m["startTime"] = mustJSON(uint64(now))
		m["initialStakers"] = mustJSON(initialStakers)
		return nil
	})
	if err != nil {
		return network.Config{}, fmt.Errorf("failed to patch genesis: %w", err)
	}

	// Start with default config
	netConfig := NewDefaultConfig(binaryPath)
	netConfig.Genesis = string(genesisBytes)

	// Build node configs from loaded keys
	nodeConfigs := make([]node.Config, numNodes)
	for i := uint32(0); i < numNodes; i++ {
		vk := validatorKeys[i]
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
	}
	netConfig.NodeConfigs = nodeConfigs

	fmt.Printf("✅ Loaded canonical genesis for network %d with %d validators (BLS keys patched)\n", networkID, numNodes)
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

	// Build P/X chain data BEFORE patching genesis
	hrp := constants.GetHRP(networkID)
	numNodes := uint32(len(validatorKeys))

	// Build initial stakers from loaded keys
	initialStakers := make([]map[string]interface{}, numNodes)
	for i, vk := range validatorKeys {
		rewardAddr, err := address.Format("P", hrp, vk.PChainAddr[:])
		if err != nil {
			return network.Config{}, fmt.Errorf("couldn't format reward address for node %d: %w", i, err)
		}

		staker := map[string]interface{}{
			"nodeID":        vk.NodeID.String(),
			"rewardAddress": rewardAddr,
			"delegationFee": 20000,
			"weight":        GigaLux,
		}

		if len(vk.BLSPublicKey) > 0 && len(vk.BLSPoP) > 0 {
			staker["signer"] = map[string]interface{}{
				"publicKey":         vk.BLSPublicKeyHex(),
				"proofOfPossession": vk.BLSPoPHex(),
			}
		}

		initialStakers[i] = staker
	}

	// Build allocations
	const (
		oneGigaLux        = uint64(1000000000000000000)
		onePercentPerYear = oneGigaLux / 100
		jan1_2020         = uint64(1577836800)
		oneYear           = uint64(31536000)
	)

	var allocations []map[string]interface{}
	for i, vk := range validatorKeys {
		pChainAddr, err := address.Format("P", hrp, vk.PChainAddr[:])
		if err != nil {
			return network.Config{}, fmt.Errorf("failed to format P-chain address for validator %d: %w", i, err)
		}
		xChainAddr, err := address.Format("X", hrp, vk.PChainAddr[:])
		if err != nil {
			return network.Config{}, fmt.Errorf("failed to format X-chain address for validator %d: %w", i, err)
		}

		allocations = append(allocations, map[string]interface{}{
			"ethAddr":        vk.CChainAddrHex(),
			"luxAddr":        xChainAddr,
			"initialAmount":  oneGigaLux,
			"unlockSchedule": []interface{}{},
		})

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

	// Add mnemonic allocation if set
	if mnemonic := os.Getenv("LUX_MNEMONIC"); mnemonic != "" {
		fmt.Printf("🔑 LUX_MNEMONIC is set, adding mnemonic allocation to genesis\n")
		allocations = append(allocations, map[string]interface{}{
			"ethAddr":       "0x0406d56943a38ad8398a738527f27e2cf01731a8",
			"luxAddr":       "P-lux1qsrd262r5w9dswv2wwzj0un79ncpwvdgkpqzqu",
			"initialAmount": 0,
			"unlockSchedule": []map[string]interface{}{
				{"amount": uint64(100000000000000), "locktime": uint64(0)},
			},
		})
	}

	// Add private key allocation if set
	var initialStakedFunds []string
	if privKeyHex := os.Getenv("LUX_PRIVATE_KEY"); privKeyHex != "" {
		privKeyBytes, err := hex.DecodeString(privKeyHex)
		if err == nil && len(privKeyBytes) == 32 {
			luxPrivKey, err := luxcrypto.ToPrivateKey(privKeyBytes)
			if err == nil {
				pubKey := luxPrivKey.PublicKey()
				pChainAddr := ids.ShortID(pubKey.Address())
				xLuxAddr, errX := address.Format("X", hrp, pChainAddr[:])
				pLuxAddr, errP := address.Format("P", hrp, pChainAddr[:])
				if errX == nil && errP == nil {
					ethAddr := "0x" + hex.EncodeToString(pChainAddr[:])
					fmt.Printf("🔑 Adding LUX_PRIVATE_KEY allocations: X=%s, P=%s (2B each)\n", xLuxAddr, pLuxAddr)
					allocations = append(allocations, map[string]interface{}{
						"ethAddr":        ethAddr,
						"luxAddr":        xLuxAddr,
						"initialAmount":  uint64(2000000000000000000),
						"unlockSchedule": []interface{}{},
					})
					allocations = append(allocations, map[string]interface{}{
						"ethAddr":       ethAddr,
						"luxAddr":       pLuxAddr,
						"initialAmount": uint64(0),
						"unlockSchedule": []map[string]interface{}{
							{"amount": uint64(2000000000000000000), "locktime": uint64(0)},
						},
					})
					initialStakedFunds = append(initialStakedFunds, xLuxAddr)
				}
			}
		}
	}

	// Patch genesis using RawMessage to preserve cChainGenesis byte-exact
	now := time.Now().Unix()
	updatedGenesis, err := patchGenesisPreservingRaw(genesisJSON, evmGenesisKeys, func(m map[string]json.RawMessage) error {
		m["initialStakers"] = mustJSON(initialStakers)
		m["allocations"] = mustJSON(allocations)
		m["initialStakedFunds"] = mustJSON(initialStakedFunds)
		m["startTime"] = mustJSON(uint64(now))
		return nil
	})
	if err != nil {
		return network.Config{}, fmt.Errorf("failed to patch genesis: %w", err)
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
			StakingKey:         string(vk.StakerKey),
			StakingCert:        string(vk.StakerCert),
			StakingSigningKey:  base64.StdEncoding.EncodeToString(vk.BLSSecretKey),
			IsBeacon:           true,
			ChainConfigFiles:   map[string]string{},
			UpgradeConfigFiles: map[string]string{},
			PChainConfigFiles:  map[string]string{},
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

	hrp := constants.GetHRP(networkID)

	// Build initial stakers from derived keys
	initialStakers := make([]map[string]interface{}, numNodes)
	var allocations []map[string]interface{}

	for i, vk := range validatorKeys {
		pChainAddr, err := address.Format("P", hrp, vk.PChainAddr[:])
		if err != nil {
			return network.Config{}, fmt.Errorf("failed to format P-chain address for validator %d: %w", i, err)
		}
		xChainAddr, err := address.Format("X", hrp, vk.PChainAddr[:])
		if err != nil {
			return network.Config{}, fmt.Errorf("failed to format X-chain address for validator %d: %w", i, err)
		}

		staker := map[string]interface{}{
			"nodeID":        vk.NodeID.String(),
			"rewardAddress": xChainAddr,
			"delegationFee": 20000,
			"weight":        GigaLux,
		}
		if len(vk.BLSPublicKey) > 0 && len(vk.BLSPoP) > 0 {
			staker["signer"] = map[string]interface{}{
				"publicKey":         vk.BLSPublicKeyHex(),
				"proofOfPossession": vk.BLSPoPHex(),
			}
		}
		initialStakers[i] = staker

		allocations = append(allocations, map[string]interface{}{
			"ethAddr":        vk.CChainAddrHex(),
			"luxAddr":        xChainAddr,
			"initialAmount":  uint64(2000000000000000000),
			"unlockSchedule": []interface{}{},
		})
		allocations = append(allocations, map[string]interface{}{
			"ethAddr":       vk.CChainAddrHex(),
			"luxAddr":       pChainAddr,
			"initialAmount": uint64(0),
			"unlockSchedule": []map[string]interface{}{
				{"amount": uint64(2000000000000000000), "locktime": uint64(0)},
			},
		})

		fmt.Printf("  Validator %d: %s -> X:%s P:%s (2B each)\n", i+1, vk.NodeID.String(), xChainAddr, pChainAddr)
	}

	// Add wallet key allocation if different from validators
	if walletNeedsAllocation {
		walletXAddr, err := address.Format("X", hrp, walletKey.PChainAddr[:])
		if err != nil {
			return network.Config{}, fmt.Errorf("failed to format wallet X-chain address: %w", err)
		}
		walletPAddr, err := address.Format("P", hrp, walletKey.PChainAddr[:])
		if err != nil {
			return network.Config{}, fmt.Errorf("failed to format wallet P-chain address: %w", err)
		}

		allocations = append(allocations, map[string]interface{}{
			"ethAddr":        walletKey.CChainAddrHex(),
			"luxAddr":        walletXAddr,
			"initialAmount":  uint64(2000000000000000000),
			"unlockSchedule": []interface{}{},
		})
		allocations = append(allocations, map[string]interface{}{
			"ethAddr":       walletKey.CChainAddrHex(),
			"luxAddr":       walletPAddr,
			"initialAmount": uint64(0),
			"unlockSchedule": []map[string]interface{}{
				{"amount": uint64(2000000000000000000), "locktime": uint64(0)},
			},
		})
		fmt.Printf("  Wallet: %s -> X:%s P:%s (2B each)\n", walletKey.PChainAddr.String(), walletXAddr, walletPAddr)
	}

	// Add LUX_PRIVATE_KEY allocations if set and different from wallet key
	if privKeyHex := os.Getenv("LUX_PRIVATE_KEY"); privKeyHex != "" {
		privKeyBytes, err := hex.DecodeString(privKeyHex)
		if err == nil && len(privKeyBytes) == 32 {
			luxPrivKey, err := luxcrypto.ToPrivateKey(privKeyBytes)
			if err == nil {
				pubKey := luxPrivKey.PublicKey()
				privKeyAddr := ids.ShortID(pubKey.Address())

				if privKeyAddr != walletKey.PChainAddr {
					privKeyXAddr, errX := address.Format("X", hrp, privKeyAddr[:])
					privKeyPAddr, errP := address.Format("P", hrp, privKeyAddr[:])
					if errX == nil && errP == nil {
						fmt.Printf("🔑 Adding LUX_PRIVATE_KEY allocations: X=%s P=%s (2B each)\n", privKeyXAddr, privKeyPAddr)
						ethAddr := "0x" + hex.EncodeToString(privKeyAddr[:])
						allocations = append(allocations, map[string]interface{}{
							"ethAddr":        ethAddr,
							"luxAddr":        privKeyXAddr,
							"initialAmount":  uint64(2000000000000000000),
							"unlockSchedule": []interface{}{},
						})
						allocations = append(allocations, map[string]interface{}{
							"ethAddr":       ethAddr,
							"luxAddr":       privKeyPAddr,
							"initialAmount": uint64(0),
							"unlockSchedule": []map[string]interface{}{
								{"amount": uint64(2000000000000000000), "locktime": uint64(0)},
							},
						})
					}
				}
			}
		}
	}

	// Patch genesis using RawMessage to preserve cChainGenesis byte-exact
	// CRITICAL: Never use map[string]interface{} for genesis - it causes float64 precision loss!
	now := time.Now().Unix()
	updatedGenesis, err := patchGenesisPreservingRaw(genesisJSON, evmGenesisKeys, func(m map[string]json.RawMessage) error {
		m["initialStakers"] = mustJSON(initialStakers)
		m["allocations"] = mustJSON(allocations)
		m["initialStakedFunds"] = mustJSON([]string{})
		m["startTime"] = mustJSON(uint64(now))
		return nil
	})
	if err != nil {
		return network.Config{}, fmt.Errorf("failed to patch genesis: %w", err)
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
		// Sybil protection MUST be enabled - validators come from genesis initialStakers
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

// NewDevnetConfigFromMnemonic creates devnet config from LUX_MNEMONIC
func NewDevnetConfigFromMnemonic(binaryPath string, numNodes uint32) (network.Config, error) {
	return NewConfigFromMnemonic(binaryPath, constants.DevnetID, numNodes)
}

// NewLocalConfigFromMnemonic creates local network config from LUX_MNEMONIC
// This uses network ID 1337 with "custom" HRP, which is simpler for testing
func NewLocalConfigFromMnemonic(binaryPath string, numNodes uint32) (network.Config, error) {
	return NewConfigFromMnemonic(binaryPath, configs.CustomID, numNodes)
}
