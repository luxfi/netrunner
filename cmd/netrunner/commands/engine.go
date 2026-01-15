// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	log "github.com/luxfi/log"

	"github.com/luxfi/netrunner/engines"
	_ "github.com/luxfi/netrunner/engines/geth"
	_ "github.com/luxfi/netrunner/engines/lux"
	"github.com/spf13/cobra"
)

// NewEngineCmd creates the engine management command
func NewEngineCmd(logger log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "engine",
		Short: "Manage individual consensus engines",
		Long:  "Start, stop, and manage individual blockchain consensus engines",
	}

	cmd.AddCommand(
		newEngineStartCmd(logger),
		newEngineStopCmd(logger),
		newEngineStatusCmd(logger),
		newEngineListCmd(logger),
		newEngineTestCmd(logger),
	)

	return cmd
}

func newEngineStartCmd(logger log.Logger) *cobra.Command {
	var (
		engineType  string
		networkID   uint32
		httpPort    uint16
		wsPort      uint16
		stakingPort uint16
		dataDir     string
		binary      string
		logLevel    string

		// OP Stack specific
		l1RPC     string
		sequencer bool

		// Eth2 specific
		consensusClient string
		executionClient string
		validator       bool
	)

	cmd := &cobra.Command{
		Use:   "start [name]",
		Short: "Start a consensus engine",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			// Create engine configuration
			config := &engines.NodeConfig{
				NetworkID:   networkID,
				HTTPPort:    httpPort,
				WSPort:      wsPort,
				StakingPort: stakingPort,
				DataDir:     dataDir,
				LogLevel:    logLevel,
				Extra:       make(map[string]interface{}),
			}

			// Add engine-specific configuration
			switch engineType {
			case "op":
				config.Extra["l1_rpc"] = l1RPC
				config.Extra["sequencer"] = sequencer
			case "eth2":
				config.Extra["consensus_client"] = consensusClient
				config.Extra["execution_client"] = executionClient
				config.Extra["validator_enabled"] = validator
			}

			// Create the engine
			engine, err := createEngine(engineType, name, binary)
			if err != nil {
				return fmt.Errorf("failed to create engine: %w", err)
			}

			// Start the engine
			ctx := cmd.Context()
			logger.Info("Starting engine",
				log.String("name", name),
				log.String("type", engineType),
				log.Uint32("network_id", networkID))

			if err := engine.Start(ctx, config); err != nil {
				return fmt.Errorf("failed to start engine: %w", err)
			}

			// Wait for engine to be healthy
			logger.Info("Waiting for engine to be healthy...")
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()

			timeout := time.After(2 * time.Minute)
			for {
				select {
				case <-ticker.C:
					health, err := engine.Health(ctx)
					if err == nil && health.Healthy {
						logger.Info("Engine is healthy",
							log.String("name", name),
							log.String("version", health.Version))

						fmt.Printf("✅ Engine '%s' started successfully\n", name)
						fmt.Printf("  Type: %s\n", engineType)
						fmt.Printf("  RPC: %s\n", engine.RPCEndpoint())
						fmt.Printf("  WebSocket: %s\n", engine.WSEndpoint())
						if p2p := engine.P2PEndpoint(); p2p != "" {
							fmt.Printf("  P2P: %s\n", p2p)
						}
						return nil
					}
				case <-timeout:
					return fmt.Errorf("engine failed to become healthy within 2 minutes")
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		},
	}

	// General flags
	cmd.Flags().StringVarP(&engineType, "type", "t", "lux", "Engine type (lux, lux, geth, op, eth2)")
	cmd.Flags().Uint32Var(&networkID, "network-id", 1337, "Network ID")
	cmd.Flags().Uint16Var(&httpPort, "http-port", 8545, "HTTP RPC port")
	cmd.Flags().Uint16Var(&wsPort, "ws-port", 8546, "WebSocket port")
	cmd.Flags().Uint16Var(&stakingPort, "staking-port", 9651, "Staking/P2P port")
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "Data directory (auto-generated if empty)")
	cmd.Flags().StringVar(&binary, "binary", "", "Path to engine binary")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "Log level")

	// OP Stack flags
	cmd.Flags().StringVar(&l1RPC, "l1-rpc", "", "L1 RPC endpoint (OP Stack only)")
	cmd.Flags().BoolVar(&sequencer, "sequencer", false, "Enable sequencer mode (OP Stack only)")

	// Eth2 flags
	cmd.Flags().StringVar(&consensusClient, "consensus-client", "lighthouse", "Consensus client (eth2 only)")
	cmd.Flags().StringVar(&executionClient, "execution-client", "geth", "Execution client (eth2 only)")
	cmd.Flags().BoolVar(&validator, "validator", false, "Enable validator (eth2 only)")

	return cmd
}

func newEngineStopCmd(logger log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "stop [name]",
		Short: "Stop a running engine",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			// TODO: Implement engine registry to track running engines
			fmt.Printf("⏹ Stopping engine '%s'...\n", name)

			return nil
		},
	}
}

func newEngineStatusCmd(logger log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "status [name]",
		Short: "Get status of an engine",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			// TODO: Implement engine registry
			fmt.Printf("📊 Status of engine '%s':\n", name)

			return nil
		},
	}
}

func newEngineListCmd(logger log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all available engine types",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Available engine types:")
			fmt.Println()

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "TYPE\tDESCRIPTION\tSTATUS")
			fmt.Fprintln(w, "----\t-----------\t------")
			fmt.Fprintln(w, "lux\tLux primary network\t✅ Ready")
			fmt.Fprintln(w, "lux\tLux network\t✅ Ready")
			fmt.Fprintln(w, "geth\tEthereum (Geth)\t✅ Ready")
			fmt.Fprintln(w, "op\tOP Stack L2\t✅ Ready")
			fmt.Fprintln(w, "eth2\tEthereum 2.0\t✅ Ready")
			w.Flush()

			return nil
		},
	}
}

func newEngineTestCmd(logger log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "test [type]",
		Short: "Test an engine type",
		Long:  "Start a test instance of the specified engine type and verify it works",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			engineType := args[0]

			// Test configuration
			config := &engines.NodeConfig{
				NetworkID:   99999,
				HTTPPort:    18545,
				WSPort:      18546,
				StakingPort: 19651,
				DataDir:     fmt.Sprintf("/tmp/netrunner-test-%s-%d", engineType, time.Now().Unix()),
				LogLevel:    "debug",
				Extra:       make(map[string]interface{}),
			}

			// Add type-specific test config
			switch engineType {
			case "op":
				// For OP Stack test, we need a mock L1
				config.Extra["l1_rpc"] = "http://localhost:8545"
				config.Extra["sequencer"] = true
			case "eth2":
				config.Extra["consensus_client"] = "lighthouse"
				config.Extra["execution_client"] = "geth"
				config.Extra["network_name"] = "testnet"
			}

			// Create test engine
			name := fmt.Sprintf("test-%s-%d", engineType, time.Now().Unix())
			engine, err := createEngine(engineType, name, "")
			if err != nil {
				return fmt.Errorf("failed to create engine: %w", err)
			}

			// Start engine
			ctx := cmd.Context()
			logger.Info("Starting test engine",
				log.String("type", engineType),
				log.String("name", name))

			if err := engine.Start(ctx, config); err != nil {
				return fmt.Errorf("failed to start engine: %w", err)
			}

			// Defer cleanup
			defer func() {
				logger.Info("Cleaning up test engine")
				if err := engine.Stop(context.Background()); err != nil {
					logger.Error("Failed to stop engine", log.Err(err))
				}
				os.RemoveAll(config.DataDir)
			}()

			// Test health check
			fmt.Printf("🧪 Testing %s engine...\n", engineType)
			fmt.Println("  ⏳ Waiting for engine to be healthy...")

			// Poll for health
			maxAttempts := 30
			for i := 0; i < maxAttempts; i++ {
				health, err := engine.Health(ctx)
				if err == nil && health.Healthy {
					fmt.Println("  ✅ Engine is healthy!")
					fmt.Printf("  📊 Version: %s\n", health.Version)
					fmt.Printf("  🔗 RPC: %s\n", engine.RPCEndpoint())
					fmt.Printf("  🌐 WebSocket: %s\n", engine.WSEndpoint())
					fmt.Printf("  ⏱️ Uptime: %s\n", engine.Uptime())

					// Type-specific tests
					switch engineType {
					case "lux":
						fmt.Printf("  🆔 Network ID: %d\n", engine.NetworkID())
						fmt.Printf("  ⛓️ Chain ID: %s\n", engine.ChainID())
					case "op":
						if parent := engine.ParentChain(); parent != nil {
							fmt.Printf("  🔗 Parent Chain: %s\n", parent.ChainID)
						}
					case "eth2":
						metrics := engine.Metrics()
						if cc, ok := metrics["consensus_client"]; ok {
							fmt.Printf("  🏛️ Consensus: %s\n", cc)
						}
						if ec, ok := metrics["execution_client"]; ok {
							fmt.Printf("  ⚙️ Execution: %s\n", ec)
						}
					}

					fmt.Println("\n✅ Test passed!")
					return nil
				}

				time.Sleep(2 * time.Second)
			}

			return fmt.Errorf("engine failed to become healthy after %d attempts", maxAttempts)
		},
	}
}

// createEngine creates an engine instance based on type
func createEngine(engineType, name, binary string) (engines.Engine, error) {
	// Set default binaries if not specified
	if binary == "" {
		switch engineType {
		case "lux":
			binary = "luxd"
		case "geth":
			binary = "geth"
		case "op":
			binary = "op-node:op-geth"
		case "eth2":
			binary = "lighthouse:geth"
		default:
			return nil, fmt.Errorf("unknown engine type: %s", engineType)
		}
	}

	// For multi-binary engines, validate format
	if strings.Contains(engineType, "op") || strings.Contains(engineType, "eth2") {
		parts := strings.Split(binary, ":")
		if len(parts) != 2 {
			return nil, fmt.Errorf("%s requires two binaries separated by colon (e.g., 'op-node:op-geth')", engineType)
		}
	}

	// Create engine using factory
	return engines.New(engines.EngineType(engineType), name, binary)
}
