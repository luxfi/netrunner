// Copyright (C) 2021-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ping

import (
	"context"
	"time"

	"github.com/luxfi/netrunner/client"
	"github.com/luxfi/netrunner/utils/constants"
	"github.com/luxfi/netrunner/ux"
	"github.com/luxfi/node/utils/logging"
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

	cmd.PersistentFlags().StringVar(&logLevel, "log-level", logging.Info.String(), "log level")
	cmd.PersistentFlags().StringVar(&endpoint, "endpoint", "0.0.0.0:8080", "server endpoint")
	cmd.PersistentFlags().DurationVar(&dialTimeout, "dial-timeout", 10*time.Second, "server dial timeout")
	cmd.PersistentFlags().DurationVar(&requestTimeout, "request-timeout", 10*time.Second, "client request timeout")

	return cmd
}

func pingFunc(*cobra.Command, []string) error {
	lvl, err := logging.ToLevel(logLevel)
	if err != nil {
		return err
	}
	lcfg := logging.Config{
		DisplayLevel: lvl,
		LogLevel:     logging.Off,
	}
	logFactory := logging.NewFactory(lcfg)
	log, err := logFactory.Make(constants.LogNameControl)
	if err != nil {
		return err
	}

	cli, err := client.New(client.Config{
		Endpoint:    endpoint,
		DialTimeout: dialTimeout,
	}, log)
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

	logString := "ping response: " + logging.Green.Wrap("%s")
	ux.Print(log, logString, resp)
	return nil
}
