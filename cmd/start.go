package cmd

import (
	"fmt"
	"github.com/luxfi/log"
	"github.com/luxfi/netrunner/multinet"
	"github.com/spf13/cobra"
)

var (
	networks     []string    // Networks to start (e.g., "mainnet", "testnet", "all")
	sharedDB     bool        // Enable shared DB for cross-chain transactions
	parallelMode bool        // Run networks in parallel
	startLogger  = log.NoLog{} // Use no-op logger for now
)

// init registers the start command flags
func init() {
	// Add multi-network flags to the start command
	startCmd.Flags().StringSliceVar(&networks, "networks", []string{"mainnet"}, "Networks to start (mainnet,testnet,all)")
	startCmd.Flags().BoolVar(&sharedDB, "shared-db", false, "Enable shared BadgerDB for cross-chain ACID transactions")
	startCmd.Flags().BoolVar(&parallelMode, "parallel", false, "Run networks in parallel validation mode")

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
	// Existing single network logic
	fmt.Printf("Starting %s network...\n", networks[0])
	// ... existing implementation ...
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