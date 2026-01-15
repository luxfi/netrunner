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

func createZooTestnetGenesis() ([]byte, error) {
	data, err := os.ReadFile("/Users/z/work/lux/genesis/configs/zoo-testnet/genesis.json")
	if err != nil {
		return nil, err
	}
	return activateEVMUpgrades(data)
}

func createCorethDebugConfig(importPath string) ([]byte, error) {
	cfg := map[string]interface{}{
		"admin-api-enabled": true,
		"eth-apis": []string{
			"eth",
			"eth-filter",
			"net",
			"web3",
			"debug",
			"internal-eth",
			"internal-blockchain",
			"internal-transaction",
			"internal-account",
			"admin",
		},
		"import-chain-data": importPath,
		"log-level": "debug",
	}
	return json.Marshal(cfg)
}

func activateEVMUpgrades(genesisJSON []byte) ([]byte, error) {
	// Force all upgrade timestamps to 0 so upgrades are active at genesis.
	var genesis map[string]interface{}
	if err := json.Unmarshal(genesisJSON, &genesis); err != nil {
		return nil, err
	}
	if cfg, ok := genesis["config"].(map[string]interface{}); ok {
		for _, key := range []string{
			"banffBlockTimestamp",
			"cortinaBlockTimestamp",
			"durangoTimestamp",
			"durangoBlockTimestamp",
			"etnaTimestamp",
			"fortunaTimestamp",
			"graniteTimestamp",
			"cancunTime",
			"shanghaiTime",
		} {
			if _, exists := cfg[key]; exists {
				cfg[key] = 0
			}
		}
	}
	return json.Marshal(genesis)
}

func activateCChainUpgrades(genesisJSON string) (string, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(genesisJSON), &top); err != nil {
		return "", err
	}
	raw, ok := top["cChainGenesis"]
	if !ok {
		return genesisJSON, nil
	}
	var cChainGenesis string
	if err := json.Unmarshal(raw, &cChainGenesis); err != nil {
		return "", err
	}
	updated, err := activateEVMUpgrades([]byte(cChainGenesis))
	if err != nil {
		return "", err
	}
	updatedRaw, err := json.Marshal(string(updated))
	if err != nil {
		return "", err
	}
	top["cChainGenesis"] = updatedRaw
	out, err := json.Marshal(top)
	if err != nil {
		return "", err
	}
	return string(out), nil
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
	updatedGenesis, err := activateCChainUpgrades(netConfig.Genesis)
	if err != nil {
		return fmt.Errorf("failed to activate C-chain upgrades: %w", err)
	}
	netConfig.Genesis = updatedGenesis
	netConfig.Flags["track-all-chains"] = true

	corethConfig, err := createCorethDebugConfig("/Users/z/work/lux/state/rlp/lux-testnet/lux-testnet-96368.rlp")
	if err != nil {
		return fmt.Errorf("failed to create coreth debug config: %w", err)
	}
	netConfig.ChainConfigFiles["C"] = string(corethConfig)

	// Override ports to avoid conflict with mainnet (use 9640-9649)
	// and update bootstrap IPs/ports accordingly
	// IMPORTANT: Port values must be integers, not strings!
	for i := range netConfig.NodeConfigs {
		netConfig.NodeConfigs[i].Flags["http-port"] = 9640 + (i * 2)
		netConfig.NodeConfigs[i].Flags["staking-port"] = 9641 + (i * 2)
		netConfig.NodeConfigs[i].Flags["track-all-chains"] = true

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
	zooCorethConfig, err := createCorethDebugConfig("/Users/z/work/lux/state/rlp/zoo-testnet/zoo-testnet-200201.rlp")
	if err != nil {
		return fmt.Errorf("failed to create zoo testnet coreth config: %w", err)
	}

	zooChainSpec := []network.ChainSpec{
		{
			VMName:         "evm",
			Genesis:        zooGenesis,
			ChainConfig:    zooCorethConfig,
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
