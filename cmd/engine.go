// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/luxfi/netrunner/orchestrator"
	"github.com/spf13/cobra"
)

var (
	engineType   string
	networkID    uint32
	httpPort     uint16
	stakingPort  uint16
	dataDir      string
	logLevel     string
	stackFile    string
)

// engineCmd represents the engine command group
var engineCmd = &cobra.Command{
	Use:   "engine",
	Short: "Manage consensus engines",
	Long:  `Start, stop, and manage multiple consensus engines (luxd, luxd, etc.)`,
}

// startCmd starts an engine
var startEngineCmd = &cobra.Command{
	Use:   "start [name]",
	Short: "Start a consensus engine",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		
		host := orchestrator.NewHost()
		config := &orchestrator.EngineOptions{
			NetworkID:   networkID,
			HTTPPort:    httpPort,
			StakingPort: stakingPort,
			DataDir:     dataDir,
			LogLevel:    logLevel,
		}
		
		if err := host.StartEngine(context.Background(), name, engineType, config); err != nil {
			return fmt.Errorf("failed to start engine: %w", err)
		}
		
		fmt.Printf("Started %s engine '%s'\n", engineType, name)
		fmt.Printf("RPC: http://localhost:%d\n", httpPort)
		return nil
	},
}

// stopCmd stops an engine
var stopEngineCmd = &cobra.Command{
	Use:   "stop [name]",
	Short: "Stop a consensus engine",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		
		host := orchestrator.NewHost()
		if err := host.StopEngine(context.Background(), name); err != nil {
			return fmt.Errorf("failed to stop engine: %w", err)
		}
		
		fmt.Printf("Stopped engine '%s'\n", name)
		return nil
	},
}

// listCmd lists running engines
var listEnginesCmd = &cobra.Command{
	Use:   "list",
	Short: "List running engines",
	RunE: func(cmd *cobra.Command, args []string) error {
		host := orchestrator.NewHost()
		engines := host.ListEngines()
		
		if len(engines) == 0 {
			fmt.Println("No engines running")
			return nil
		}
		
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tTYPE\tNETWORK\tSTATUS\tRPC\tUPTIME")
		
		for _, e := range engines {
			status := "unhealthy"
			if e.Healthy {
				status = "healthy"
			}
			fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%s\n",
				e.Name,
				e.Type,
				e.NetworkID,
				status,
				e.RPC,
				formatDuration(e.Uptime),
			)
		}
		
		return w.Flush()
	},
}

// stackCmd manages stacks
var stackCmd = &cobra.Command{
	Use:   "stack",
	Short: "Manage engine stacks",
}

// startStackCmd starts a stack
var startStackCmd = &cobra.Command{
	Use:   "start",
	Short: "Start an engine stack from manifest",
	RunE: func(cmd *cobra.Command, args []string) error {
		if stackFile == "" {
			return fmt.Errorf("--file is required")
		}
		
		manifest, err := orchestrator.LoadManifest(stackFile)
		if err != nil {
			return fmt.Errorf("failed to load manifest: %w", err)
		}
		
		host := orchestrator.NewHost()
		if err := host.StartStack(context.Background(), manifest); err != nil {
			return fmt.Errorf("failed to start stack: %w", err)
		}
		
		fmt.Printf("Started stack '%s'\n", manifest.Name)
		fmt.Println("\nRunning engines:")
		
		for _, e := range host.ListEngines() {
			fmt.Printf("  %s (%s): %s\n", e.Name, e.Type, e.RPC)
		}
		
		return nil
	},
}

// stopStackCmd stops a stack
var stopStackCmd = &cobra.Command{
	Use:   "stop [name]",
	Short: "Stop an engine stack",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		
		host := orchestrator.NewHost()
		if err := host.StopStack(context.Background(), name); err != nil {
			return fmt.Errorf("failed to stop stack: %w", err)
		}
		
		fmt.Printf("Stopped stack '%s'\n", name)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(engineCmd)
	
	// Engine commands
	engineCmd.AddCommand(startEngineCmd)
	engineCmd.AddCommand(stopEngineCmd)
	engineCmd.AddCommand(listEnginesCmd)
	
	// Engine start flags
	startEngineCmd.Flags().StringVar(&engineType, "type", "lux", "Engine type (lux, lux, geth, op)")
	startEngineCmd.Flags().Uint32Var(&networkID, "network-id", 96369, "Network ID")
	startEngineCmd.Flags().Uint16Var(&httpPort, "http-port", 9630, "HTTP RPC port")
	startEngineCmd.Flags().Uint16Var(&stakingPort, "staking-port", 9631, "P2P staking port")
	startEngineCmd.Flags().StringVar(&dataDir, "data-dir", "", "Data directory")
	startEngineCmd.Flags().StringVar(&logLevel, "log-level", "info", "Log level")
	
	// Stack commands
	engineCmd.AddCommand(stackCmd)
	stackCmd.AddCommand(startStackCmd)
	stackCmd.AddCommand(stopStackCmd)
	
	// Stack flags
	startStackCmd.Flags().StringVar(&stackFile, "file", "", "Stack manifest file")
	startStackCmd.MarkFlagRequired("file")
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}