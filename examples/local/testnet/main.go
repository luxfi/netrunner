package main

import (
	"context"
	"encoding/json"
	"fmt"
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

// Zoo chain genesis for testnet (chain ID 200201)
func createZooTestnetGenesis() ([]byte, error) {
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
			"chainId":             200201, // Testnet chain ID
			"constantinopleBlock": 0,
			"eip150Block":         0,
			"eip155Block":         0,
			"eip158Block":         0,
			"homesteadBlock":      0,
			"istanbulBlock":       0,
			"londonBlock":         0,
			"petersburgBlock":     0,
			"subnetEVMTimestamp":  0,
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

	binaryPath := os.Getenv("LUX_BINARY_PATH")
	if binaryPath == "" {
		home, _ := os.UserHomeDir()
		binaryPath = home + "/.lux/bin/luxd/luxd"
	}

	if err := run(logger, binaryPath); err != nil {
		fmt.Printf("🔴 FATAL ERROR: %v\n", err)
		logger.Crit("fatal error", log.Err(err))
		os.Exit(1)
	}
}

func run(logger log.Logger, binaryPath string) error {
	mnemonic := os.Getenv("LUX_MNEMONIC")
	if mnemonic == "" {
		return fmt.Errorf("LUX_MNEMONIC environment variable must be set")
	}

	// Create TESTNET network config (network ID 2)
	logger.Info("Creating TESTNET network config from mnemonic...")
	netConfig, err := local.NewTestnetConfigFromMnemonic(binaryPath, 5)
	if err != nil {
		return fmt.Errorf("failed to create testnet config: %w", err)
	}

	// Override ports to avoid conflict with mainnet (use 9640-9649)
	// and update bootstrap IPs/ports accordingly
	// IMPORTANT: Port values must be integers, not strings!
	for i := range netConfig.NodeConfigs {
		netConfig.NodeConfigs[i].Flags["http-port"] = 9640 + (i * 2)
		netConfig.NodeConfigs[i].Flags["staking-port"] = 9641 + (i * 2)

		// Update bootstrap ports for non-beacon nodes
		if !netConfig.NodeConfigs[i].IsBeacon {
			netConfig.NodeConfigs[i].Flags["bootstrap-ips"] = "[::1]:9641"
		}
	}

	// Use separate root directory for testnet
	testnetRootDir := "/tmp/testnet-runner-root-data"

	// Create the root directory if it doesn't exist
	if err := os.MkdirAll(testnetRootDir, 0755); err != nil {
		return fmt.Errorf("failed to create testnet root dir: %w", err)
	}

	logger.Info("Starting TESTNET local network with mnemonic-derived validators...")
	logger.Info("Using root dir", "dir", testnetRootDir)
	nw, err := local.NewNetwork(logger, netConfig, testnetRootDir, "", true)
	if err != nil {
		fmt.Printf("🔴 Network creation failed: %v\n", err)
		logger.Error("Failed to create network", log.Err(err))
		return fmt.Errorf("failed to create network: %w", err)
	}
	defer func() {
		if err := nw.Stop(context.Background()); err != nil {
			logger.Error("error stopping network", log.Err(err))
		}
	}()

	signalsChan := make(chan os.Signal, 1)
	signal.Notify(signalsChan, syscall.SIGINT)
	signal.Notify(signalsChan, syscall.SIGTERM)
	closedOnShutdownCh := make(chan struct{})
	go func() {
		shutdownOnSignal(logger, nw, signalsChan, closedOnShutdownCh)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), healthyTimeout)
	defer cancel()
	logger.Info("waiting for all nodes to report healthy...")
	if err := nw.Healthy(ctx); err != nil {
		return err
	}

	logger.Info("All nodes healthy!")

	// Create Zoo blockchain on testnet
	logger.Info("Creating Zoo TESTNET blockchain...")
	zooGenesis, err := createZooTestnetGenesis()
	if err != nil {
		return fmt.Errorf("failed to create zoo testnet genesis: %w", err)
	}

	zooChainSpec := []network.ChainSpec{
		{
			VMName:         "evm",
			Genesis:        zooGenesis,
			ChainConfig:    nil,
			BlockchainName: "zootest",
		},
	}

	ctxOp, cancelOp := context.WithTimeout(context.Background(), operationTimeout)
	defer cancelOp()

	chainIDs, err := nw.CreateChains(ctxOp, zooChainSpec)
	if err != nil {
		return fmt.Errorf("failed to create zoo testnet chain: %w", err)
	}

	chainIDStrs := make([]string, len(chainIDs))
	for i, id := range chainIDs {
		chainIDStrs[i] = id.String()
	}
	logger.Info("Zoo TESTNET blockchain created!", "chainIDs", chainIDStrs)
	fmt.Printf("\n✅ Zoo TESTNET blockchain created successfully!\n")
	fmt.Printf("   Chain ID: %s\n", chainIDs[0])
	fmt.Printf("   RPC: http://localhost:9640/ext/bc/zootest/rpc\n\n")

	logger.Info("TESTNET will run until you CTRL + C to exit...")
	<-closedOnShutdownCh
	return nil
}
