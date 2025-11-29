// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package local

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/luxfi/genesis/configs"
	"github.com/luxfi/ids"
	"github.com/luxfi/netrunner/network"
	"github.com/luxfi/netrunner/network/node"
	"github.com/luxfi/node/config"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/crypto/bls/signer/localsigner"
	"github.com/luxfi/node/utils/formatting/address"
	"github.com/luxfi/node/vms/platformvm/signer"
	"golang.org/x/exp/maps"
)

// NewConfigForNetwork creates a network config for the specified network ID.
// This uses the proper genesis configuration from github.com/luxfi/genesis/configs.
// For non-local networks (mainnet/testnet), it dynamically injects initial stakers
// based on the generated node staking keys.
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
	rewardAddr, err := address.Format("X", hrp, ids.GenerateTestShortID().Bytes())
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

	// Ensure there's a staked funds entry
	stakeAddr, err := address.Format("X", hrp, ids.GenerateTestShortID().Bytes())
	if err != nil {
		return network.Config{}, fmt.Errorf("couldn't format stake address: %w", err)
	}

	// Update allocations to include staked funds if not present
	allocations, ok := genesis["allocations"].([]interface{})
	if !ok {
		allocations = []interface{}{}
	}

	// Add stake allocation for validators
	now := time.Now().Unix()
	stakeAllocation := map[string]interface{}{
		"ethAddr":       "0x0000000000000000000000000000000000000000",
		"luxAddr":       stakeAddr,
		"initialAmount": 0,
		"unlockSchedule": []map[string]interface{}{
			{
				"amount":   uint64(numNodes) * 1000000000000, // 1M LUX per validator
				"locktime": uint64(now + 7*24*3600),          // 1 week lockup
			},
		},
	}
	allocations = append(allocations, stakeAllocation)
	genesis["allocations"] = allocations

	// Set initial staked funds
	genesis["initialStakedFunds"] = []string{stakeAddr}

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
			SubnetConfigFiles:    map[string]string{},
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
