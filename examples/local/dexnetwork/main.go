// Copyright (C) 2021-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"fmt"
	"go/build"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/luxfi/netrunner/local"
	"github.com/luxfi/netrunner/network"
	luxlog "github.com/luxfi/log"
	"github.com/luxfi/log/level"
	"go.uber.org/zap"
)

const (
	healthyTimeout      = 2 * time.Minute
	createBlockchainTimeout = 5 * time.Minute
)

var goPath = os.ExpandEnv("$GOPATH")

// Blocks until a signal is received on [signalChan], upon which
// [n.Stop()] is called. If [signalChan] is closed, does nothing.
// Closes [closedOnShutdownChan] and [signalChan] when done shutting down network.
// This function should only be called once.
func shutdownOnSignal(
	log luxlog.Logger,
	n network.Network,
	signalChan chan os.Signal,
	closedOnShutdownChan chan struct{},
) {
	sig := <-signalChan
	log.Info("got OS signal", zap.Stringer("signal", sig))
	if err := n.Stop(context.Background()); err != nil {
		log.Info("error stopping network", zap.Error(err))
	}
	signal.Reset()
	close(signalChan)
	close(closedOnShutdownChan)
}

// dexGenesisJSON returns the genesis configuration for the DEX VM.
// This configures initial trading pairs, fee settings, and block parameters.
func dexGenesisJSON() []byte {
	return []byte(`{
		"blockInterval": "1ms",
		"maxOrdersPerBlock": 10000,
		"tradingPairs": [
			{"base": "LUX", "quote": "USDT", "minOrderSize": "0.001", "tickSize": "0.01"},
			{"base": "ETH", "quote": "USDT", "minOrderSize": "0.0001", "tickSize": "0.01"},
			{"base": "BTC", "quote": "USDT", "minOrderSize": "0.00001", "tickSize": "0.01"}
		],
		"fees": {
			"makerFee": "0.001",
			"takerFee": "0.002",
			"liquidationFee": "0.005"
		},
		"perpetuals": {
			"enabled": true,
			"maxLeverage": 100,
			"fundingInterval": "8h",
			"maintenanceMarginRatio": "0.01"
		}
	}`)
}

// Shows example usage of the Lux Network Runner with DEX VM.
// Creates a local five node Lux network with DEX blockchain
// and waits for all nodes to become healthy.
// The network runs until the user provides a SIGINT or SIGTERM.
func main() {
	// Create the logger
	logFactory := luxlog.NewFactoryWithConfig(luxlog.Config{
		DisplayLevel: level.Info,
		LogLevel:     level.Debug,
	})
	log, err := logFactory.Make("main")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	if goPath == "" {
		goPath = build.Default.GOPATH
	}
	binaryPath := fmt.Sprintf("%s%s", goPath, "/src/github.com/luxfi/node/build/node")
	if err := run(log, binaryPath); err != nil {
		log.Fatal("fatal error", zap.Error(err))
		os.Exit(1)
	}
}

func run(log luxlog.Logger, binaryPath string) error {
	// Create the network with 5 nodes
	nw, err := local.NewDefaultNetwork(log, binaryPath, true)
	if err != nil {
		return err
	}
	defer func() { // Stop the network when this function returns
		if err := nw.Stop(context.Background()); err != nil {
			log.Info("error stopping network", zap.Error(err))
		}
	}()

	// When we get a SIGINT or SIGTERM, stop the network and close [closedOnShutdownCh]
	signalsChan := make(chan os.Signal, 1)
	signal.Notify(signalsChan, syscall.SIGINT)
	signal.Notify(signalsChan, syscall.SIGTERM)
	closedOnShutdownCh := make(chan struct{})
	go func() {
		shutdownOnSignal(log, nw, signalsChan, closedOnShutdownCh)
	}()

	// Wait until the nodes in the network are ready
	ctx, cancel := context.WithTimeout(context.Background(), healthyTimeout)
	defer cancel()
	log.Info("waiting for all nodes to report healthy...")
	if err := nw.Healthy(ctx); err != nil {
		return err
	}
	log.Info("All nodes healthy")

	// Create DEX blockchain on a new subnet
	log.Info("Creating DEX blockchain...")
	createCtx, createCancel := context.WithTimeout(context.Background(), createBlockchainTimeout)
	defer createCancel()

	// DEX blockchain specification
	dexBlockchainSpec := []network.BlockchainSpec{
		{
			VMName:          "dexvm",          // Matches constants.DexVMName
			Genesis:         dexGenesisJSON(), // DEX genesis configuration
			BlockchainAlias: "dex",            // Alias for easier access
			// SubnetID not specified = creates new subnet with all nodes as validators
		},
	}

	chainIDs, err := nw.CreateBlockchains(createCtx, dexBlockchainSpec)
	if err != nil {
		return fmt.Errorf("failed to create DEX blockchain: %w", err)
	}

	log.Info("DEX blockchain created successfully",
		zap.String("blockchain-id", chainIDs[0].String()),
		zap.String("alias", "dex"),
	)

	// Print connection info
	nodes, err := nw.GetAllNodes()
	if err != nil {
		return err
	}
	log.Info("DEX network ready. Connect to any node:")
	for name, node := range nodes {
		log.Info("  Node available",
			zap.String("name", name),
			zap.String("url", fmt.Sprintf("http://%s:%d/ext/bc/dex", node.GetURL(), node.GetAPIPort())),
		)
	}

	log.Info("Network running. Press CTRL+C to exit...")
	// Wait until done shutting down network after SIGINT/SIGTERM
	<-closedOnShutdownCh
	return nil
}
