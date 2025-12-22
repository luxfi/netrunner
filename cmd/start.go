package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/luxfi/keys"
	"github.com/luxfi/log"
	"github.com/luxfi/netrunner/local"
	"github.com/luxfi/netrunner/multinet"
	"github.com/luxfi/netrunner/network"
	"github.com/spf13/cobra"
)

var (
	networks     []string // Networks to start (e.g., "mainnet", "testnet", "all")
	sharedDB     bool     // Enable shared DB for cross-chain transactions
	parallelMode bool     // Run networks in parallel
	keysDir      string   // Directory containing pre-existing validator keys
	binaryPath   string   // Path to luxd binary
	startLogger  = log.NoLog{}
)

// init registers the start command flags
func init() {
	// Add multi-network flags to the start command
	startCmd.Flags().StringSliceVar(&networks, "networks", []string{"mainnet"}, "Networks to start (mainnet,testnet,all)")
	startCmd.Flags().BoolVar(&sharedDB, "shared-db", false, "Enable shared BadgerDB for cross-chain ACID transactions")
	startCmd.Flags().BoolVar(&parallelMode, "parallel", false, "Run networks in parallel validation mode")
	startCmd.Flags().StringVar(&keysDir, "keys-dir", local.DefaultKeysPath(), "Directory containing validator keys")
	startCmd.Flags().StringVar(&binaryPath, "binary", "luxd", "Path to luxd binary")

	// Add start command to root
	rootCmd.AddCommand(startCmd)
}

// startCmd is the enhanced start command that handles both single and multi-network
var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start network(s)",
	Long:  `Start one or more networks. Supports parallel validation and cross-chain transactions.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Check if multi-network mode is requested
		if len(networks) > 1 || contains(networks, "all") || parallelMode {
			return runMultiNetwork(cmd, args)
		}

		// Single network mode (existing behavior)
		return runSingleNetwork(cmd, args)
	},
}

func runMultiNetwork(cmd *cobra.Command, args []string) error {
	fmt.Println("🚀 Starting networks in parallel mode...")

	dbPath := "/tmp/netrunner-shared-db"
	if sharedDB {
		fmt.Println("📦 Using shared BadgerDB for cross-chain transactions")
	}

	// Create multi-network manager
	manager, err := multinet.NewMultiNetworkManager(startLogger, dbPath)
	if err != nil {
		return fmt.Errorf("failed to create multi-network manager: %w", err)
	}
	defer manager.Shutdown()

	// Determine which networks to start
	networksToStart := networks
	if contains(networks, "all") {
		networksToStart = []string{"mainnet", "testnet"}
	}

	// Configure networks
	for _, network := range networksToStart {
		config := getNetworkConfig(network)
		if err := manager.AddNetwork(config); err != nil {
			return fmt.Errorf("failed to add network %s: %w", network, err)
		}
	}

	// Start all networks
	if err := manager.StartAll(); err != nil {
		return fmt.Errorf("failed to start networks: %w", err)
	}

	fmt.Println("\n✅ Networks started successfully!")
	printNetworkEndpoints(networksToStart)

	fmt.Println("\nPress Ctrl+C to stop...")
	select {}

	return nil
}

func runSingleNetwork(cmd *cobra.Command, args []string) error {
	networkName := networks[0]
	fmt.Printf("🚀 Starting %s network with pre-existing keys from %s...\n", networkName, keysDir)

	// Load validator keys
	ks := keys.NewKeyStore(keysDir)
	validatorKeys, err := ks.LoadAll()
	if err != nil {
		return fmt.Errorf("failed to load validator keys: %w", err)
	}
	if len(validatorKeys) == 0 {
		return fmt.Errorf("no validator keys found in %s", keysDir)
	}

	fmt.Printf("📋 Loaded %d validator keys:\n", len(validatorKeys))
	for i, vk := range validatorKeys {
		fmt.Printf("   %d. %s (P-Chain: %s)\n", i+1, vk.NodeID.String(), vk.PChainAddr.String())
	}

	// Get network config with pre-existing keys
	var netConfig network.Config
	switch networkName {
	case "mainnet":
		netConfig, err = local.NewMainnetConfigWithKeys(binaryPath, keysDir)
	case "testnet":
		netConfig, err = local.NewTestnetConfigWithKeys(binaryPath, keysDir)
	default:
		return fmt.Errorf("unknown network: %s", networkName)
	}
	if err != nil {
		return fmt.Errorf("failed to create network config: %w", err)
	}

	// Debug: Print node config details
	fmt.Printf("\n📋 Network config has %d node configs:\n", len(netConfig.NodeConfigs))
	for i, nc := range netConfig.NodeConfigs {
		fmt.Printf("   Node %d: StakingKey len=%d, StakingCert len=%d\n", i+1, len(nc.StakingKey), len(nc.StakingCert))
	}

	// Start the network
	fmt.Println("\n🔧 Starting network nodes...")
	ln, err := local.NewNetwork(
		startLogger,
		netConfig,
		"/tmp/netrunner/"+networkName,
		"",   // snapshot dir
		true, // reassign ports if busy
	)
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}

	// Print network info
	fmt.Printf("\n✅ %s network started!\n", networkName)
	fmt.Println("\n📊 Node Endpoints:")
	nodes, _ := ln.GetAllNodes()
	for name, n := range nodes {
		fmt.Printf("   %s: http://localhost:%d\n", name, n.GetAPIPort())
	}

	// Wait for interrupt
	fmt.Println("\n⏳ Press Ctrl+C to stop...")
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\n🛑 Shutting down...")
	if err := ln.Stop(cmd.Context()); err != nil {
		fmt.Printf("Warning: error stopping network: %v\n", err)
	}

	return nil
}

func getNetworkConfig(network string) multinet.NetworkConfig {
	switch network {
	case "mainnet":
		return multinet.NetworkConfig{
			NetworkID:   96369,
			Name:        "Lux Mainnet",
			Type:        multinet.NetworkTypePrimary,
			HTTPPort:    9630,
			StakingPort: 9631,
			DataDir:     "/tmp/netrunner/mainnet",
			Validators:  5,
		}
	case "testnet":
		return multinet.NetworkConfig{
			NetworkID:   96368,
			Name:        "Lux Testnet",
			Type:        multinet.NetworkTypePrimary,
			HTTPPort:    9620,
			StakingPort: 9621,
			DataDir:     "/tmp/netrunner/testnet",
			Validators:  3,
		}
	default:
		// Could be a chain name
		return multinet.NetworkConfig{}
	}
}

func printNetworkEndpoints(networks []string) {
	fmt.Println("\n📊 Network Endpoints:")
	for _, network := range networks {
		switch network {
		case "mainnet":
			fmt.Println("  Lux Mainnet: http://localhost:9630")
		case "testnet":
			fmt.Println("  Lux Testnet: http://localhost:9620")
		}
	}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}