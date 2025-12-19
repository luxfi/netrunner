//go:build mainnet
// +build mainnet

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package local_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/luxfi/genesis/configs"
	"github.com/luxfi/netrunner/local"
	luxlog "github.com/luxfi/log"
	"github.com/luxfi/log/level"
	"github.com/stretchr/testify/require"
)

const (
	luxdBinaryPath   = "/Users/z/go/bin/luxd"
	healthyTimeout   = 3 * time.Minute
	mainnetNetworkID = uint32(96369)
)

func TestMainnetConfigGeneration(t *testing.T) {
	// Test that we can generate a valid mainnet configuration
	netConfig, err := local.NewMainnetConfig(luxdBinaryPath, 5)
	require.NoError(t, err, "Failed to create mainnet config")

	// Verify network ID in genesis
	var genesis map[string]interface{}
	err = json.Unmarshal([]byte(netConfig.Genesis), &genesis)
	require.NoError(t, err, "Failed to parse genesis JSON")

	// Network ID should be mainnet (96369)
	networkIDFloat, ok := genesis["networkID"].(float64)
	require.True(t, ok, "networkID should be a number")
	require.Equal(t, mainnetNetworkID, uint32(networkIDFloat), "Expected mainnet network ID")

	// Should have 5 node configs
	require.Len(t, netConfig.NodeConfigs, 5, "Expected 5 node configs")

	// Each node should have staking keys configured
	for i, nodeConfig := range netConfig.NodeConfigs {
		require.NotEmpty(t, nodeConfig.StakingKey, "Node %d should have staking key", i)
		require.NotEmpty(t, nodeConfig.StakingCert, "Node %d should have staking cert", i)
		require.NotEmpty(t, nodeConfig.StakingSigningKey, "Node %d should have BLS signing key", i)
	}

	// Should have initial stakers in genesis
	initialStakers, ok := genesis["initialStakers"].([]interface{})
	require.True(t, ok, "Genesis should have initialStakers")
	require.Len(t, initialStakers, 5, "Should have 5 initial stakers")

	// Verify each staker has required fields
	for i, staker := range initialStakers {
		stakerMap, ok := staker.(map[string]interface{})
		require.True(t, ok, "Staker %d should be a map", i)
		require.NotEmpty(t, stakerMap["nodeID"], "Staker %d should have nodeID", i)
		require.NotEmpty(t, stakerMap["rewardAddress"], "Staker %d should have rewardAddress", i)
		require.NotNil(t, stakerMap["delegationFee"], "Staker %d should have delegationFee", i)
		require.NotNil(t, stakerMap["signer"], "Staker %d should have signer", i)
	}

	t.Logf("Successfully generated mainnet config with %d nodes", len(netConfig.NodeConfigs))
}

func TestMainnetGenesisFromConfigs(t *testing.T) {
	// Test loading genesis from configs package
	genesisData, err := configs.GetGenesis(mainnetNetworkID)
	require.NoError(t, err, "Failed to load mainnet genesis")
	require.NotEmpty(t, genesisData, "Genesis data should not be empty")

	var genesis map[string]interface{}
	err = json.Unmarshal(genesisData, &genesis)
	require.NoError(t, err, "Failed to parse genesis JSON")

	// Verify network ID
	networkIDFloat, ok := genesis["networkID"].(float64)
	require.True(t, ok, "networkID should be a number")
	require.Equal(t, mainnetNetworkID, uint32(networkIDFloat), "Expected mainnet network ID")

	// Verify C-chain genesis
	cChainGenesis, ok := genesis["cChainGenesis"].(string)
	require.True(t, ok, "cChainGenesis should be a string")
	require.NotEmpty(t, cChainGenesis, "cChainGenesis should not be empty")

	var cChain map[string]interface{}
	err = json.Unmarshal([]byte(cChainGenesis), &cChain)
	require.NoError(t, err, "Failed to parse cChainGenesis")

	// Verify C-chain chainId matches network ID
	configObj, ok := cChain["config"].(map[string]interface{})
	require.True(t, ok, "cChain should have config")
	chainIDFloat, ok := configObj["chainId"].(float64)
	require.True(t, ok, "config should have chainId")
	require.Equal(t, mainnetNetworkID, uint32(chainIDFloat), "C-chain chainId should match network ID")

	t.Logf("Mainnet genesis loaded successfully with network ID %d", uint32(networkIDFloat))
}

func TestMainnetFiveNodeNetwork(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create logger
	logFactory := luxlog.NewFactoryWithConfig(luxlog.Config{
		DisplayLevel: level.Info,
		LogLevel:     level.Debug,
	})
	log, err := logFactory.Make("test")
	require.NoError(t, err, "Failed to create logger")

	// Create mainnet config for 5 nodes
	netConfig, err := local.NewMainnetConfig(luxdBinaryPath, 5)
	require.NoError(t, err, "Failed to create mainnet config")

	// Create network
	nw, err := local.NewNetwork(log, netConfig, "", "", true)
	require.NoError(t, err, "Failed to create network")
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = nw.Stop(ctx)
	}()

	// Wait for network to be healthy
	ctx, cancel := context.WithTimeout(context.Background(), healthyTimeout)
	defer cancel()

	t.Log("Waiting for network to become healthy...")
	err = nw.Healthy(ctx)
	require.NoError(t, err, "Network failed to become healthy")

	// Get network info
	nodes, err := nw.GetAllNodes()
	require.NoError(t, err, "Failed to get nodes")
	require.Len(t, nodes, 5, "Should have 5 nodes")

	t.Logf("Network healthy with %d nodes", len(nodes))

	// Log node URIs
	for name, node := range nodes {
		t.Logf("Node %s: %s", name, node.GetURL())
	}
}

func TestMainnetConsensusValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create logger
	logFactory := luxlog.NewFactoryWithConfig(luxlog.Config{
		DisplayLevel: level.Info,
		LogLevel:     level.Debug,
	})
	log, err := logFactory.Make("test")
	require.NoError(t, err, "Failed to create logger")

	// Create mainnet config for 5 nodes
	netConfig, err := local.NewMainnetConfig(luxdBinaryPath, 5)
	require.NoError(t, err, "Failed to create mainnet config")

	// Create network
	nw, err := local.NewNetwork(log, netConfig, "", "", true)
	require.NoError(t, err, "Failed to create network")
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = nw.Stop(ctx)
	}()

	// Wait for network to be healthy
	ctx, cancel := context.WithTimeout(context.Background(), healthyTimeout)
	defer cancel()

	t.Log("Waiting for network to become healthy...")
	err = nw.Healthy(ctx)
	require.NoError(t, err, "Network failed to become healthy")

	// Verify each node is running with correct network ID
	nodes, err := nw.GetAllNodes()
	require.NoError(t, err, "Failed to get nodes")

	for name, node := range nodes {
		// Get info from node
		client := node.GetAPIClient()
		require.NotNil(t, client, "Node %s should have API client", name)

		info := client.InfoAPI()
		require.NotNil(t, info, "Node %s should have Info API", name)

		// Get network ID from node
		nodeNetworkID, err := info.GetNetworkID(ctx)
		require.NoError(t, err, "Failed to get network ID from node %s", name)
		require.Equal(t, mainnetNetworkID, nodeNetworkID, "Node %s should have mainnet network ID", name)

		t.Logf("Node %s: Network ID = %d", name, nodeNetworkID)
	}

	t.Log("All nodes validated with correct mainnet configuration")
}
