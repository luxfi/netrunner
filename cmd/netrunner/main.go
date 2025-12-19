// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/luxfi/netrunner/cmd/netrunner/commands"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	// Setup logger
	logger, err := zap.NewDevelopment()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	// Setup root command
	rootCmd := &cobra.Command{
		Use:   "netrunner",
		Short: "Multi-consensus blockchain orchestrator",
		Long: `Netrunner is a powerful orchestration tool for running multiple blockchain
consensus engines simultaneously. It supports Lux, Lux, Ethereum 2.0,
OP Stack, and more.`,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
	}

	// Add subcommands
	rootCmd.AddCommand(
		commands.NewEngineCmd(logger),
		commands.NewStackCmd(logger),
		commands.NewStatusCmd(logger),
		commands.NewLogsCmd(logger),
		commands.NewBridgeCmd(logger),
		commands.NewTestCmd(logger),
	)

	// Setup context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupts
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("Received interrupt signal, shutting down...")
		cancel()
	}()

	// Execute command
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		logger.Error("Command failed", zap.Error(err))
		os.Exit(1)
	}
}