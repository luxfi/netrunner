package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/luxfi/log"
	"github.com/luxfi/netrunner/multinet"
	"github.com/spf13/cobra"
)

var (
	sharedDBPath   string
	networkConfigs string
	logger         = log.NoLog{} // Use no-op logger for now
)

// multinetCmd represents the multinet command
var multinetCmd = &cobra.Command{
	Use:   "multinet",
	Short: "Manage multiple heterogeneous networks in parallel",
	Long: `Run and validate multiple networks simultaneously with shared BadgerDB
for cross-chain ACID transactions. Supports both primary networks (mainnet/testnet)
and their chains (L1s/L2s).`,
}

// startCmd starts multiple networks
var startMultiCmd = &cobra.Command{
	Use:   "start",
	Short: "Start multiple networks in parallel",
	Long: `Start configured networks with parallel consensus validation and
shared database for cross-chain transactions.`,
	RunE: runStartMulti,
}

// configureCmd configures networks
var configureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Configure networks for parallel validation",
	Long:  `Set up mainnet, testnet, and their chains for parallel validation.`,
	RunE:  runConfigure,
}

// statusCmd shows status of all networks
var statusMultiCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status of all networks",
	RunE:  runStatusMulti,
}

// crosschainCmd handles cross-chain transactions
var crosschainCmd = &cobra.Command{
	Use:   "crosschain",
	Short: "Submit cross-chain transaction",
	Long:  `Submit a transaction that moves assets between different networks atomically.`,
	RunE:  runCrosschain,
}

func init() {
	rootCmd.AddCommand(multinetCmd)

	multinetCmd.AddCommand(startMultiCmd)
	multinetCmd.AddCommand(configureCmd)
	multinetCmd.AddCommand(statusMultiCmd)
	multinetCmd.AddCommand(crosschainCmd)

	multinetCmd.PersistentFlags().StringVar(&sharedDBPath, "shared-db", "/tmp/multinet-shared-db", "Path to shared BadgerDB")
	multinetCmd.PersistentFlags().StringVar(&networkConfigs, "configs", "", "Path to network configurations JSON")
}

func runStartMulti(cmd *cobra.Command, args []string) error {
	// Create multi-network manager
	manager, err := multinet.NewMultiNetworkManager(logger, sharedDBPath)
	if err != nil {
		return fmt.Errorf("failed to create multi-network manager: %w", err)
	}
	defer manager.Shutdown()

	// Load default configuration if no config file provided
	if networkConfigs == "" {
		if err := loadDefaultNetworks(manager); err != nil {
			return err
		}
	} else {
		if err := loadNetworksFromFile(manager, networkConfigs); err != nil {
			return err
		}
	}

	// Start all networks
	if err := manager.StartAll(); err != nil {
		return fmt.Errorf("failed to start networks: %w", err)
	}

	fmt.Println("✅ All networks started successfully!")
	fmt.Println("\nNetwork endpoints:")
	fmt.Println("  Lux Mainnet: http://localhost:9630")
	fmt.Println("  Lux Testnet: http://localhost:9620")
	fmt.Println("\nChains are accessible through their parent networks.")
	fmt.Println("\nPress Ctrl+C to stop all networks...")

	// Wait for interrupt
	select {}
}

func loadDefaultNetworks(manager *multinet.MultiNetworkManager) error {
	// Lux Mainnet configuration
	mainnetConfig := multinet.NetworkConfig{
		NetworkID:   96369,
		Name:        "Lux Mainnet",
		Type:        multinet.NetworkTypePrimary,
		HTTPPort:    9630,
		StakingPort: 9631,
		DataDir:     "/tmp/multinetwork/mainnet",
		Validators:  5,
		ChainConfig: []multinet.ChainConfig{
			{
				ChainID: "2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM",
				VMID:    "rWhpuQPF1kb72esV2momhMuTYGkEb1oL29pt2EBXWmSy4kxnT", // PlatformVM
			},
			{
				ChainID: "2ebCneCbwthjQ1rYT41nhd7M76Hc6YmosMAQrTFhBq8qeqh6tt",
				VMID:    "jvYyfQTxGMJLuGWa55kdP2p2zSUYsQ5Raupu4TW34ZAUBAbtq", // AVM
			},
			{
				ChainID: "2bXe2MhhAnXg6WGj6G8oDk55AKT1dMMsN72S8te7JdvzfZX1zM",
				VMID:    "mgj786NP7uDwBCcq6YwThhaN8FLyybkCa4zBWTQbNgmK6k9A6", // EVM
				IsEVM:   true,
			},
		},
	}

	// Lux Testnet configuration
	testnetConfig := multinet.NetworkConfig{
		NetworkID:   96368,
		Name:        "Lux Testnet",
		Type:        multinet.NetworkTypePrimary,
		HTTPPort:    9620,
		StakingPort: 9621,
		DataDir:     "/tmp/multinetwork/testnet",
		Validators:  3,
		ChainConfig: []multinet.ChainConfig{
			{
				ChainID: "2JVSBoinj9C2J33VntvzYtVJNZdN2NKiwwKjcumHUWEb5DbBrm",
				VMID:    "rWhpuQPF1kb72esV2momhMuTYGkEb1oL29pt2EBXWmSy4kxnT", // PlatformVM
			},
			{
				ChainID: "2K33xS9AyP9oCDiHYKVrHe7F54h2La5D8erpTChaAhdzeSu2RX",
				VMID:    "jvYyfQTxGMJLuGWa55kdP2p2zSUYsQ5Raupu4TW34ZAUBAbtq", // AVM
			},
			{
				ChainID: "2sdADEgBC3NjLM4inKc1hY1PQpCT3JVyGVJxdmcq6sqrDndjFG",
				VMID:    "mgj786NP7uDwBCcq6YwThhaN8FLyybkCa4zBWTQbNgmK6k9A6", // EVM
				IsEVM:   true,
			},
		},
	}

	// Zoo Chain on Mainnet
	zooMainnetConfig := multinet.NetworkConfig{
		NetworkID:   200200,
		Name:        "Zoo Network (Mainnet Chain)",
		Type:        multinet.NetworkTypeChain,
		ParentID:    96369,
		HTTPPort:    0, // Uses parent network's port
		StakingPort: 0, // Uses parent network's port
		DataDir:     "/tmp/multinetwork/mainnet/chains/zoo",
		Validators:  5,
	}

	// Hanzo Chain on Mainnet
	hanzoMainnetConfig := multinet.NetworkConfig{
		NetworkID:   36963,
		Name:        "Hanzo Network (Mainnet Chain)",
		Type:        multinet.NetworkTypeChain,
		ParentID:    96369,
		HTTPPort:    0,
		StakingPort: 0,
		DataDir:     "/tmp/multinetwork/mainnet/chains/hanzo",
		Validators:  5,
	}

	// Add all networks
	if err := manager.AddNetwork(mainnetConfig); err != nil {
		return err
	}
	if err := manager.AddNetwork(testnetConfig); err != nil {
		return err
	}
	if err := manager.AddNetwork(zooMainnetConfig); err != nil {
		return err
	}
	if err := manager.AddNetwork(hanzoMainnetConfig); err != nil {
		return err
	}

	return nil
}

func loadNetworksFromFile(manager *multinet.MultiNetworkManager, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	var configs []multinet.NetworkConfig
	if err := json.NewDecoder(file).Decode(&configs); err != nil {
		return fmt.Errorf("failed to decode config: %w", err)
	}

	for _, config := range configs {
		if err := manager.AddNetwork(config); err != nil {
			return fmt.Errorf("failed to add network %s: %w", config.Name, err)
		}
	}

	return nil
}

func runConfigure(cmd *cobra.Command, args []string) error {
	// Create default configuration
	configs := []multinet.NetworkConfig{
		{
			NetworkID:   96369,
			Name:        "Lux Mainnet",
			Type:        multinet.NetworkTypePrimary,
			HTTPPort:    9630,
			StakingPort: 9631,
			DataDir:     "/tmp/multinetwork/mainnet",
			Validators:  5,
		},
		{
			NetworkID:   96368,
			Name:        "Lux Testnet",
			Type:        multinet.NetworkTypePrimary,
			HTTPPort:    9620,
			StakingPort: 9621,
			DataDir:     "/tmp/multinetwork/testnet",
			Validators:  3,
		},
		{
			NetworkID:   200200,
			Name:        "Zoo Network",
			Type:        multinet.NetworkTypeChain,
			ParentID:    96369,
			DataDir:     "/tmp/multinetwork/mainnet/chains/zoo",
			Validators:  5,
		},
		{
			NetworkID:   36963,
			Name:        "Hanzo Network",
			Type:        multinet.NetworkTypeChain,
			ParentID:    96369,
			DataDir:     "/tmp/multinetwork/mainnet/chains/hanzo",
			Validators:  5,
		},
	}

	// Write to file
	configFile := "multinetwork-config.json"
	file, err := os.Create(configFile)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(configs); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Printf("✅ Configuration saved to %s\n", configFile)
	fmt.Println("\nConfiguration includes:")
	fmt.Println("  • Lux Mainnet (96369) - Primary Network")
	fmt.Println("  • Lux Testnet (96368) - Primary Network")
	fmt.Println("  • Zoo Network (200200) - Chain on Mainnet")
	fmt.Println("  • Hanzo Network (36963) - Chain on Mainnet")

	return nil
}

func runStatusMulti(cmd *cobra.Command, args []string) error {
	manager, err := multinet.NewMultiNetworkManager(logger, sharedDBPath)
	if err != nil {
		return fmt.Errorf("failed to create manager: %w", err)
	}
	defer manager.Shutdown()

	status := manager.GetNetworkStatus()

	fmt.Println("📊 Network Status")
	fmt.Println("================")
	for networkID, networkStatus := range status {
		fmt.Printf("  Network %d: %s\n", networkID, networkStatus)
	}

	return nil
}

func runCrosschain(cmd *cobra.Command, args []string) error {
	if len(args) < 5 {
		return fmt.Errorf("usage: crosschain <source-net> <source-chain> <dest-net> <dest-chain> <amount>")
	}

	// Parse arguments (simplified for demo)
	tx := &multinet.CrossChainTx{
		ID:          fmt.Sprintf("tx-%d", time.Now().Unix()),
		SourceNet:   parseUint32(args[0]),
		SourceChain: args[1],
		DestNet:     parseUint32(args[2]),
		DestChain:   args[3],
		Amount:      parseUint64(args[4]),
		Asset:       "LUX",
	}

	manager, err := multinet.NewMultiNetworkManager(logger, sharedDBPath)
	if err != nil {
		return fmt.Errorf("failed to create manager: %w", err)
	}
	defer manager.Shutdown()

	if err := manager.SubmitCrossChainTx(tx); err != nil {
		return fmt.Errorf("failed to submit transaction: %w", err)
	}

	fmt.Printf("✅ Cross-chain transaction submitted!\n")
	fmt.Printf("  TX ID: %s\n", tx.ID)
	fmt.Printf("  From: Network %d Chain %s\n", tx.SourceNet, tx.SourceChain)
	fmt.Printf("  To: Network %d Chain %s\n", tx.DestNet, tx.DestChain)
	fmt.Printf("  Amount: %d %s\n", tx.Amount, tx.Asset)

	return nil
}

// Helper functions for parsing command line arguments
func parseUint32(s string) uint32 {
	val, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0
	}
	return uint32(val)
}

func parseUint64(s string) uint64 {
	val, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return val
}