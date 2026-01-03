// Copyright (C) 2021-2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/luxfi/log"
	"github.com/luxfi/log/level"
	"github.com/luxfi/netrunner/local"
	"github.com/luxfi/netrunner/network"
	"github.com/luxfi/node/config"
)

const (
	healthyTimeout          = 3 * time.Minute
	createBlockchainTimeout = 5 * time.Minute
)

func getLuxdBinaryPath() string {
	// Check LUXD_PATH env var first
	if p := os.Getenv("LUXD_PATH"); p != "" {
		return p
	}
	// Default to $GOPATH/bin/luxd or $HOME/go/bin/luxd
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		return filepath.Join(gopath, "bin", "luxd")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "go", "bin", "luxd")
}

func getPluginDir() string {
	// Check LUXD_PLUGIN_DIR env var first
	if p := os.Getenv("LUXD_PLUGIN_DIR"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".lux", "plugins")
}

func getGenesisDir() string {
	// Check LUX_GENESIS_DIR env var first
	if p := os.Getenv("LUX_GENESIS_DIR"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "work", "lux", "genesis")
}

// L2Chain defines configuration for deploying an L2 chain
type L2Chain struct {
	Name        string
	ChainID     uint64
	VMName      string
	Alias       string
	GenesisFile string // relative to genesis dir
}

// L2 Chain configurations
var l2Chains = []L2Chain{
	{
		Name:        "Zoo",
		ChainID:     200200,
		VMName:      "evm",
		Alias:       "zoo",
		GenesisFile: "chains/zoo/genesis.json",
	},
	{
		Name:        "SPC",
		ChainID:     36911,
		VMName:      "evm",
		Alias:       "spc",
		GenesisFile: "chains/spc/genesis.json",
	},
	{
		Name:        "Hanzo AI",
		ChainID:     36963,
		VMName:      "evm",
		Alias:       "hanzo",
		GenesisFile: "chains/ai/genesis.json",
	},
}

func shutdownOnSignal(
	logger log.Logger,
	n network.Network,
	signalChan chan os.Signal,
	closedOnShutdownChan chan struct{},
) {
	sig := <-signalChan
	logger.Info("got OS signal", log.Stringer("signal", sig))
	if err := n.Stop(context.Background()); err != nil {
		logger.Info("error stopping network", log.Err(err))
	}
	signal.Reset()
	close(signalChan)
	close(closedOnShutdownChan)
}

func main() {
	logFactory := log.NewFactoryWithConfig(log.Config{
		DisplayLevel: level.Info,
		LogLevel:     level.Debug,
	})
	logger, err := logFactory.Make("l2chains")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if err := run(logger); err != nil {
		logger.Fatal("fatal error", log.Err(err))
		os.Exit(1)
	}
}

func run(logger log.Logger) error {
	luxdPath := getLuxdBinaryPath()
	pluginPath := getPluginDir()
	genesisPath := getGenesisDir()

	logger.Info("Configuration",
		log.String("luxd", luxdPath),
		log.String("plugins", pluginPath),
		log.String("genesis", genesisPath),
	)

	// Create mainnet config for 5-node network
	logger.Info("Creating mainnet configuration...")
	netConfig, err := local.NewMainnetConfig(luxdPath, 5)
	if err != nil {
		return fmt.Errorf("failed to create mainnet config: %w", err)
	}

	// Add plugin directory and allow private IPs (required for local testing)
	// Also enable output redirection to see errors
	for i := range netConfig.NodeConfigs {
		netConfig.NodeConfigs[i].Flags[config.PluginDirKey] = pluginPath
		netConfig.NodeConfigs[i].Flags[config.NetworkAllowPrivateIPsKey] = true
		netConfig.NodeConfigs[i].RedirectStdout = true
		netConfig.NodeConfigs[i].RedirectStderr = true
	}

	// Create network
	logger.Info("Starting 5-node mainnet network...")
	nw, err := local.NewNetwork(logger, netConfig, "", "", true)
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}
	defer func() {
		if err := nw.Stop(context.Background()); err != nil {
			logger.Info("error stopping network", log.Err(err))
		}
	}()

	// Setup signal handler
	signalsChan := make(chan os.Signal, 1)
	signal.Notify(signalsChan, syscall.SIGINT)
	signal.Notify(signalsChan, syscall.SIGTERM)
	closedOnShutdownCh := make(chan struct{})
	go func() {
		shutdownOnSignal(logger, nw, signalsChan, closedOnShutdownCh)
	}()

	// Wait until the nodes in the network are ready
	ctx, cancel := context.WithTimeout(context.Background(), healthyTimeout)
	defer cancel()
	logger.Info("Waiting for all nodes to report healthy...")
	if err := nw.Healthy(ctx); err != nil {
		return fmt.Errorf("network failed to become healthy: %w", err)
	}
	logger.Info("All nodes healthy")

	// Get network info
	nodes, err := nw.GetAllNodes()
	if err != nil {
		return fmt.Errorf("failed to get nodes: %w", err)
	}
	logger.Info("Network ready", log.Int("nodes", len(nodes)))

	// Deploy L2 chains
	for _, chain := range l2Chains {
		genesisFile := filepath.Join(genesisPath, chain.GenesisFile)
		logger.Info("Deploying L2 chain...",
			log.String("name", chain.Name),
			log.Uint64("chainId", chain.ChainID),
			log.String("alias", chain.Alias),
			log.String("genesis", genesisFile),
		)

		genesis, err := os.ReadFile(genesisFile)
		if err != nil {
			log.Error("Failed to read genesis file",
				log.String("chain", chain.Name),
				log.String("path", genesisFile),
				log.Err(err),
			)
			continue
		}

		createCtx, createCancel := context.WithTimeout(context.Background(), createBlockchainTimeout)
		chainSpec := []network.ChainSpec{
			{
				VMName:         chain.VMName,
				Genesis:        genesis,
				Alias:          chain.Alias,
				BlockchainName: chain.Alias, // Use unique name for each chain
			},
		}

		chainIDs, err := nw.CreateChains(createCtx, chainSpec)
		createCancel()
		if err != nil {
			log.Error("Failed to create chain",
				log.String("chain", chain.Name),
				log.String("error", err.Error()),
			)
			continue
		}

		logger.Info("L2 chain deployed successfully",
			log.String("name", chain.Name),
			log.String("blockchain-id", chainIDs[0].String()),
			log.String("alias", chain.Alias),
		)
	}

	// Print connection info
	logger.Info("L2 chains deployed. Connect to any node:")
	for name, node := range nodes {
		logger.Info("Node available",
			log.String("name", name),
			log.String("url", fmt.Sprintf("http://%s:%d", node.GetURL(), node.GetAPIPort())),
		)
	}

	logger.Info("Network running. Press CTRL+C to exit...")
	<-closedOnShutdownCh
	return nil
}
