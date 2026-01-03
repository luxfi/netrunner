// Copyright (C) 2021-2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package ping

import (
	"context"
	"time"

	"github.com/luxfi/log"
	"github.com/luxfi/log/level"
	"github.com/luxfi/netrunner/client"
	"github.com/luxfi/netrunner/utils/constants"
	"github.com/luxfi/netrunner/ux"
	"github.com/spf13/cobra"
)

var (
	logLevel       string
	endpoint       string
	dialTimeout    time.Duration
	requestTimeout time.Duration
)

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ping [options]",
		Short: "Ping the server.",
		RunE:  pingFunc,
	}

	cmd.PersistentFlags().StringVar(&logLevel, "log-level", level.Info.String(), "log level")
	cmd.PersistentFlags().StringVar(&endpoint, "endpoint", "0.0.0.0:9000", "server endpoint")
	cmd.PersistentFlags().DurationVar(&dialTimeout, "dial-timeout", 10*time.Second, "server dial timeout")
	cmd.PersistentFlags().DurationVar(&requestTimeout, "request-timeout", 10*time.Second, "client request timeout")

	return cmd
}

func pingFunc(*cobra.Command, []string) error {
	lvl, err := log.ToLevel(logLevel)
	if err != nil {
		return err
	}
	lcfg := log.Config{
		DisplayLevel: lvl,
		LogLevel:     level.Off,
	}
	logFactory := log.NewFactoryWithConfig(lcfg)
	logger, err := logFactory.Make(constants.LogNameControl)
	if err != nil {
		return err
	}

	cli, err := client.New(client.Config{
		Endpoint:    endpoint,
		DialTimeout: dialTimeout,
	}, logger)
	if err != nil {
		return err
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	resp, err := cli.Ping(ctx)
	cancel()
	if err != nil {
		return err
	}

	logString := "ping response: " + log.Green.Wrap("%s")
	ux.Print(logger, logString, resp)
	return nil
}
