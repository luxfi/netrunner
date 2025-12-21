// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package local

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/bls/signer/localsigner"
	"github.com/luxfi/genesis/configs"
	"github.com/luxfi/ids"
	"github.com/luxfi/keys"
	"github.com/luxfi/netrunner/network"
	"github.com/luxfi/netrunner/network/node"
	"github.com/luxfi/node/config"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/utils/formatting/address"
	"github.com/luxfi/node/vms/platformvm/signer"
	"golang.org/x/exp/maps"
)

// NewConfigForNetwork creates a network config for the specified network ID.
// This uses the proper genesis configuration from github.com/luxfi/genesis/configs.
// For non-local networks (mainnet/testnet), it dynamically injects initial stakers
// based on the generated node staking keys.
//
// When LUX_MNEMONIC environment variable is set, validator keys are derived
// deterministically from the mnemonic, ensuring consistent node identities
// and P-Chain allocations across network restarts.
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

	// Start with the default config structure
	netConfig := NewDefaultConfig(binaryPath)

	// For local network (1337), use the genesis as-is since it already has stakers
	// matching the pre-generated node keys
	// Note: configs.LocalID is 1337, constants.LocalID is 31337 - use configs.LocalID
	if networkID == configs.LocalID {
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

	// For mainnet/testnet, we need to generate new staking keys and inject them
	// as initial stakers in the genesis

	// Parse genesis to modify it
	var genesis map[string]interface{}
	if err := json.Unmarshal(genesisJSON, &genesis); err != nil {
		return network.Config{}, fmt.Errorf("failed to parse genesis: %w", err)
	}

	// Generate staking keys for all nodes and collect staker info
	type stakerInfo struct {
		nodeID     ids.NodeID
		stakingKey string
		stakingCrt string
		signerKey  string
		pop        *signer.ProofOfPossession
	}
	stakers := make([]stakerInfo, numNodes)

	for i := uint32(0); i < numNodes; i++ {
		// Generate new staking cert/key
		stakingCert, stakingKey, err := staking.NewCertAndKeyBytes()
		if err != nil {
			return network.Config{}, fmt.Errorf("couldn't generate staking Cert/Key for node %d: %w", i, err)
		}

		// Parse the cert to get NodeID
		tlsCert, err := tls.X509KeyPair(stakingCert, stakingKey)
		if err != nil {
			return network.Config{}, fmt.Errorf("couldn't parse TLS cert for node %d: %w", i, err)
		}

		// Convert to ids.Certificate for NodeID computation
		if len(tlsCert.Certificate) == 0 {
			return network.Config{}, fmt.Errorf("no certificate data for node %d", i)
		}
		idsCert := &ids.Certificate{
			Raw:       tlsCert.Certificate[0],
			PublicKey: tlsCert.PrivateKey,
		}
		nodeID := ids.NodeIDFromCert(idsCert)

		// Generate BLS signer key and proof of possession
		blsKey, err := localsigner.New()
		if err != nil {
			return network.Config{}, fmt.Errorf("couldn't generate BLS key for node %d: %w", i, err)
		}

		pop, err := signer.NewProofOfPossession(blsKey)
		if err != nil {
			return network.Config{}, fmt.Errorf("couldn't generate proof of possession for node %d: %w", i, err)
		}

		stakers[i] = stakerInfo{
			nodeID:     nodeID,
			stakingKey: string(stakingKey),
			stakingCrt: string(stakingCert),
			signerKey:  base64.StdEncoding.EncodeToString(blsKey.ToBytes()),
			pop:        pop,
		}
	}

	// Create initial stakers for genesis
	hrp := constants.GetHRP(networkID)

	// Treasury address short ID (derived from 0x9011E888251AB053B7bD1cdB598Db4f9DEd94714)
	// P-Chain Address: P-lux1c7wevm4667l4umtzh93r25wpxlpsadkhka6gv6
	var treasuryShortID ids.ShortID
	treasuryBytes, _ := hex.DecodeString("c79d966ebad7bf5e6d62b9623551c137c30eb6d7")
	copy(treasuryShortID[:], treasuryBytes)
	rewardAddr, err := address.Format("P", hrp, treasuryShortID[:])
	if err != nil {
		return network.Config{}, fmt.Errorf("couldn't format reward address: %w", err)
	}

	initialStakers := make([]map[string]interface{}, numNodes)
	for i, s := range stakers {
		initialStakers[i] = map[string]interface{}{
			"nodeID":        s.nodeID.String(),
			"rewardAddress": rewardAddr,
			"delegationFee": 20000, // 2% delegation fee
			"signer": map[string]interface{}{
				"publicKey":         "0x" + hex.EncodeToString(s.pop.PublicKey[:]),
				"proofOfPossession": "0x" + hex.EncodeToString(s.pop.ProofOfPossession[:]),
			},
		}
	}

	// Update genesis with initial stakers
	genesis["initialStakers"] = initialStakers

	// Check if genesis already has allocations from the configs package
	// (which may include dynamic allocations from ~/.lux/genesis/{network}/pchain.json)
	existingAllocs, hasAllocs := genesis["allocations"].([]interface{})
	if hasAllocs && len(existingAllocs) > 0 {
		// Use existing allocations from genesis - don't overwrite
		// This preserves dynamic P-Chain allocations configured via:
		// - LUX_PCHAIN_ALLOCS environment variable
		// - LUX_PCHAIN_ALLOCS_FILE environment variable
		// - ~/.lux/genesis/{network}/pchain.json file
	} else {
		// No allocations in genesis - auto-generate based on numNodes (--num-validators)
		// Each validator gets 1M LUX (DefaultValidatorStake) immediately available
		// First validator gets 10M LUX for fees, chain creation, etc.
		validatorAllocs, _, err := GenerateValidatorAllocations(numNodes, hrp)
		if err != nil {
			return network.Config{}, fmt.Errorf("failed to generate validator allocations: %w", err)
		}

		// Convert to []interface{} for JSON marshaling
		allocations := make([]interface{}, len(validatorAllocs))
		for i, alloc := range validatorAllocs {
			allocations[i] = alloc
		}
		genesis["allocations"] = allocations
	}

	// C-chain genesis is immutable - don't modify it
	// The C-chain genesis from configs package contains the real mainnet state
	now := time.Now().Unix()

	// Check if initialStakedFunds already exists in genesis from configs package
	// If not, extract from the second allocation's address
	if _, hasStakedFunds := genesis["initialStakedFunds"]; !hasStakedFunds {
		// Extract second allocation address for initialStakedFunds
		if allocs, ok := genesis["allocations"].([]interface{}); ok && len(allocs) > 1 {
			if secondAlloc, ok := allocs[1].(map[string]interface{}); ok {
				if luxAddr, ok := secondAlloc["luxAddr"].(string); ok {
					genesis["initialStakedFunds"] = []string{luxAddr}
				}
			}
		}
		// Fall back to empty if we couldn't extract
		if _, hasStakedFunds := genesis["initialStakedFunds"]; !hasStakedFunds {
			genesis["initialStakedFunds"] = []string{}
		}
	}

	// Update start time to now
	genesis["startTime"] = uint64(now)

	// Re-serialize genesis
	updatedGenesis, err := json.MarshalIndent(genesis, "", "  ")
	if err != nil {
		return network.Config{}, fmt.Errorf("failed to serialize updated genesis: %w", err)
	}
	netConfig.Genesis = string(updatedGenesis)

	// Configure node configs with the generated staking keys
	netConfig.NodeConfigs = make([]node.Config, numNodes)
	for i := uint32(0); i < numNodes; i++ {
		port := 9630 + int(i)*2
		netConfig.NodeConfigs[i] = node.Config{
			Flags: map[string]interface{}{
				config.HTTPPortKey:    port,
				config.StakingPortKey: port + 1,
			},
			StakingKey:           stakers[i].stakingKey,
			StakingCert:          stakers[i].stakingCrt,
			StakingSigningKey:    stakers[i].signerKey,
			IsBeacon:             true,
			ChainConfigFiles:     map[string]string{},
			UpgradeConfigFiles:   map[string]string{},
			PChainConfigFiles:    map[string]string{},
		}
	}

	return netConfig, nil
}

// NewMainnetConfig creates a network config for LUX Mainnet (network ID 96369)
func NewMainnetConfig(binaryPath string, numNodes uint32) (network.Config, error) {
	return NewConfigForNetwork(binaryPath, numNodes, configs.LuxMainnetID)
}

// NewTestnetConfig creates a network config for LUX Testnet (network ID 96368)
func NewTestnetConfig(binaryPath string, numNodes uint32) (network.Config, error) {
	return NewConfigForNetwork(binaryPath, numNodes, configs.LuxTestnetID)
}

// NewLocalConfig creates a network config for local development (network ID 1337)
// This is equivalent to NewDefaultConfigNNodes but uses the configs package.
func NewLocalConfig(binaryPath string, numNodes uint32) (network.Config, error) {
	return NewConfigForNetwork(binaryPath, numNodes, configs.LocalID)
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
			"delegationFee": 20000, // 2% delegation fee
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

	// Update genesis with initial stakers
	genesis["initialStakers"] = initialStakers

	// Generate P-Chain allocations from the loaded keys
	allocBuilder := keys.NewAllocationBuilder(networkID, validatorKeys).
		WithAmount(100 * keys.MegaLux).  // 100M LUX per validator
		WithFeeAccount(0, 10*keys.MegaLux). // First validator gets extra for fees
		WithImmediateUnlock()

	keyAllocations, err := allocBuilder.Build()
	if err != nil {
		return network.Config{}, fmt.Errorf("failed to build allocations: %w", err)
	}

	// Convert P-Chain allocations to genesis format
	pchainAllocs := make([]interface{}, len(keyAllocations.PChainAllocations))
	for i, alloc := range keyAllocations.PChainAllocations {
		unlockSchedule := make([]map[string]interface{}, len(alloc.UnlockSchedule))
		for j, unlock := range alloc.UnlockSchedule {
			unlockSchedule[j] = map[string]interface{}{
				"amount":   unlock.Amount,
				"locktime": unlock.Locktime,
			}
		}
		pchainAllocs[i] = map[string]interface{}{
			"ethAddr":        alloc.ETHAddr,
			"luxAddr":        alloc.LUXAddr,
			"initialAmount":  alloc.InitialAmount,
			"unlockSchedule": unlockSchedule,
		}
	}
	genesis["allocations"] = pchainAllocs

	// Set initial staked funds
	genesis["initialStakedFunds"] = keyAllocations.InitialStakedFunds

	// Update start time to now
	now := time.Now().Unix()
	genesis["startTime"] = uint64(now)

	// Re-serialize genesis
	updatedGenesis, err := json.MarshalIndent(genesis, "", "  ")
	if err != nil {
		return network.Config{}, fmt.Errorf("failed to serialize updated genesis: %w", err)
	}
	netConfig.Genesis = string(updatedGenesis)

	// Configure node configs with the loaded staking keys
	netConfig.NodeConfigs = make([]node.Config, numNodes)
	for i, vk := range validatorKeys {
		port := 9630 + int(i)*2
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
	return filepath.Join(home, "work", "lux", "keys")
}

// NewMainnetConfigWithKeys creates a mainnet config using pre-existing validator keys
func NewMainnetConfigWithKeys(binaryPath string, keysDir string) (network.Config, error) {
	if keysDir == "" {
		keysDir = DefaultKeysPath()
	}
	return NewConfigWithPreExistingKeys(binaryPath, configs.LuxMainnetID, keysDir)
}

// NewTestnetConfigWithKeys creates a testnet config using pre-existing validator keys
func NewTestnetConfigWithKeys(binaryPath string, keysDir string) (network.Config, error) {
	if keysDir == "" {
		keysDir = DefaultKeysPath()
	}
	return NewConfigWithPreExistingKeys(binaryPath, configs.LuxTestnetID, keysDir)
}
