package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	log "github.com/luxfi/log"
	"github.com/luxfi/netrunner/local"
	"github.com/luxfi/netrunner/network"
)

const (
	healthyTimeout   = 2 * time.Minute
	operationTimeout = 5 * time.Minute
)

// Blocks until a signal is received on [signalChan], upon which
// [n.Stop()] is called. If [signalChan] is closed, does nothing.
// Closes [closedOnShutdownChan] and [signalChan] when done shutting down network.
// This function should only be called once.
func shutdownOnSignal(
	logger log.Logger,
	n network.Network,
	signalChan chan os.Signal,
	closedOnShutdownChan chan struct{},
) {
	sig := <-signalChan
	logger.Info("got OS signal", "signal", sig.String())
	if err := n.Stop(context.Background()); err != nil {
		logger.Error("error stopping network", log.Err(err))
	}
	signal.Reset()
	close(signalChan)
	close(closedOnShutdownChan)
}

// Zoo chain genesis configuration - uses the exact genesis from zoo-mainnet RLP export
// Genesis hash expected: 0x7c548af47de27560779ccc67dda32a540944accc71dac3343da3b9cd18f14933
func createZooGenesis() ([]byte, error) {
	zooGenesis := map[string]interface{}{
		"alloc": map[string]interface{}{
			"0200000000000000000000000000000000000005": map[string]interface{}{
				"balance": "0x0",
				"nonce":   "0x1",
				"code":    "0x01",
			},
			"9011E888251AB053B7bD1cdB598Db4f9DEd94714": map[string]interface{}{
				"balance": "0x193e5939a08ce9dbd480000000",
			},
		},
		"baseFeePerGas": "0x5d21dba00",
		"config": map[string]interface{}{
			"berlinBlock":         0,
			"byzantiumBlock":      0,
			"chainId":             200200,
			"constantinopleBlock": 0,
			"eip150Block":         0,
			"eip155Block":         0,
			"eip158Block":         0,
			"homesteadBlock":      0,
			"istanbulBlock":       0,
			"londonBlock":         0,
			"petersburgBlock":     0,
			"evmTimestamp":  0,
			"durangoTimestamp":    0,
			"feeConfig": map[string]interface{}{
				"gasLimit":                 12000000,
				"targetBlockRate":          2,
				"minBaseFee":               25000000000,
				"targetGas":                15000000,
				"baseFeeChangeDenominator": 36,
				"minBlockGasCost":          0,
				"maxBlockGasCost":          1000000,
				"blockGasCostStep":         200000,
			},
		},
		"difficulty": "0x0",
		"gasLimit":   "0xb71b00",
		"timestamp":  "0x6727e9c3",
	}
	return json.Marshal(zooGenesis)
}

func main() {
	logger := log.New()

	// Check for BINARY_PATH env var or use default
	binaryPath := os.Getenv("BINARY_PATH")
	if binaryPath == "" {
		home, _ := os.UserHomeDir()
		binaryPath = home + "/.lux/bin/luxd/luxd"
	}

	if err := run(logger, binaryPath); err != nil {
		logger.Crit("fatal error", log.Err(err))
		os.Exit(1)
	}
}

func run(logger log.Logger, binaryPath string) error {
	// Check for MNEMONIC environment variable
	mnemonic := os.Getenv("MNEMONIC")
	if mnemonic == "" {
		return fmt.Errorf("MNEMONIC environment variable must be set")
	}

	// Create the network config from mnemonic
	// Use MAINNET network (ID 96369) with proper genesis
	logger.Info("Creating MAINNET network config from mnemonic...")
	netConfig, err := local.NewMainnetConfigFromMnemonic(binaryPath, 5)
	if err != nil {
		return fmt.Errorf("failed to create mainnet config: %w", err)
	}

	// Create the network
	logger.Info("Starting local network with mnemonic-derived validators...")
	nw, err := local.NewNetwork(logger, netConfig, "", "", true)
	if err != nil {
		return err
	}
	defer func() { // Stop the network when this function returns
		if err := nw.Stop(context.Background()); err != nil {
			logger.Error("error stopping network", log.Err(err))
		}
	}()

	// When we get a SIGINT or SIGTERM, stop the network and close [closedOnShutdownCh]
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
	logger.Info("waiting for all nodes to report healthy...")
	if err := nw.Healthy(ctx); err != nil {
		return err
	}

	logger.Info("All nodes healthy!")

	// Create Zoo blockchain
	logger.Info("Creating Zoo blockchain...")
	zooGenesis, err := createZooGenesis()
	if err != nil {
		return fmt.Errorf("failed to create zoo genesis: %w", err)
	}

	zooChainSpec := []network.ChainSpec{
		{
			VMName:         "evm",
			Genesis:        zooGenesis,
			ChainConfig:    nil,
			BlockchainName: "zoo",
		},
	}

	ctxOp, cancelOp := context.WithTimeout(context.Background(), operationTimeout)
	defer cancelOp()

	chainIDs, err := nw.CreateChains(ctxOp, zooChainSpec)
	if err != nil {
		return fmt.Errorf("failed to create zoo chain: %w", err)
	}

	chainIDStrs := make([]string, len(chainIDs))
	for i, id := range chainIDs {
		chainIDStrs[i] = id.String()
	}
	logger.Info("Zoo blockchain created!", "chainIDs", chainIDStrs)
	fmt.Printf("\n✅ Zoo blockchain created successfully!\n")
	fmt.Printf("   Chain ID: %s\n\n", chainIDs[0])

	logger.Info("Network will run until you CTRL + C to exit...")
	// Wait until done shutting down network after SIGINT/SIGTERM
	<-closedOnShutdownCh
	return nil
}
