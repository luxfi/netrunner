package main

import (
	"context"
	"encoding/json"
	"fmt"
	"go/build"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/luxfi/log"
	"github.com/luxfi/netrunner/local"
	"github.com/luxfi/netrunner/network"
)

const (
	healthyTimeout   = 2 * time.Minute
	operationTimeout = 5 * time.Minute
)

var goPath = os.ExpandEnv("$GOPATH")

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

// Zoo chain genesis configuration
func createZooGenesis() ([]byte, error) {
	zooGenesis := map[string]interface{}{
		"config": map[string]interface{}{
			"chainId":             200200,
			"homesteadBlock":      0,
			"eip150Block":         0,
			"eip155Block":         0,
			"eip158Block":         0,
			"byzantiumBlock":      0,
			"constantinopleBlock": 0,
			"petersburgBlock":     0,
			"istanbulBlock":       0,
			"muirGlacierBlock":    0,
			"berlinBlock":         0,
			"londonBlock":         0,
			"arrowGlacierBlock":   0,
			"grayGlacierBlock":    0,
			"feeConfig": map[string]interface{}{
				"gasLimit":                 15000000,
				"targetBlockRate":          2,
				"minBaseFee":               25000000000,
				"targetGas":                15000000,
				"baseFeeChangeDenominator": 36,
				"minBlockGasCost":          0,
				"maxBlockGasCost":          1000000,
				"blockGasCostStep":         200000,
			},
		},
		"nonce":      "0x0",
		"timestamp":  "0x0",
		"extraData":  "0x00",
		"gasLimit":   "0xe4e1c0",
		"difficulty": "0x0",
		"mixHash":    "0x0000000000000000000000000000000000000000000000000000000000000000",
		"coinbase":   "0x0000000000000000000000000000000000000000",
		"alloc":      map[string]interface{}{},
		"number":     "0x0",
		"gasUsed":    "0x0",
		"parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
	}
	return json.Marshal(zooGenesis)
}

func main() {
	logger := log.New()
	if goPath == "" {
		goPath = build.Default.GOPATH
	}
	binaryPath := fmt.Sprintf("%s%s", goPath, "/src/github.com/luxfi/node/build/node")
	if err := run(logger, binaryPath); err != nil {
		logger.Crit("fatal error", log.Err(err))
		os.Exit(1)
	}
}

func run(logger log.Logger, binaryPath string) error {
	// Check for LUX_MNEMONIC environment variable
	mnemonic := os.Getenv("LUX_MNEMONIC")
	if mnemonic == "" {
		return fmt.Errorf("LUX_MNEMONIC environment variable must be set")
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
