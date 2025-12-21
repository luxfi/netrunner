// Copyright (C) 2021-2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package cmd

import (
	"fmt"
	"os"

	"github.com/luxfi/netrunner/cmd/control"
	"github.com/luxfi/netrunner/cmd/ping"
	"github.com/luxfi/netrunner/cmd/server"
	"github.com/spf13/cobra"
)

var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

var rootCmd = &cobra.Command{
	Use:        "netrunner",
	Short:      "netrunner commands",
	SuggestFor: []string{"network-runner"},
	Version:    fmt.Sprintf("%s (commit: %s, built: %s)", Version, Commit, BuildDate),
}

func init() {
	cobra.EnablePrefixMatching = true
}

func init() {
	rootCmd.AddCommand(
		server.NewCommand(),
		ping.NewCommand(),
		control.NewCommand(),
	)
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "netrunner failed %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}
