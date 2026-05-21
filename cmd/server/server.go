// Copyright (C) 2021-2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package server

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	log "github.com/luxfi/log"
	"github.com/luxfi/netrunner/server"
	"github.com/luxfi/netrunner/utils"
	"github.com/luxfi/netrunner/utils/constants"
	"github.com/spf13/cobra"
)

func init() {
	cobra.EnablePrefixMatching = true
}

const serverRootDirPrefix = "server"

var (
	logLevel           string
	logDir             string
	port               string
	gwPort             string
	dialTimeout        time.Duration
	disableNodesOutput bool
	snapshotsDir       string
)

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server [options]",
		Short: "Start a network runner server.",
		RunE:  serverFunc,
		Args:  cobra.ExactArgs(0),
	}

	cmd.PersistentFlags().StringVar(&logLevel, "log-level", log.InfoLevel.String(), "log level for server logs")
	cmd.PersistentFlags().StringVar(&logDir, "log-dir", "", "log directory")
	cmd.PersistentFlags().StringVar(&port, "port", ":9000", "ZAP server port")
	cmd.PersistentFlags().StringVar(&gwPort, "gateway-port", "", "reserved for the optional ZIP HTTP edge — empty disables")
	cmd.PersistentFlags().DurationVar(&dialTimeout, "dial-timeout", 10*time.Second, "server dial timeout")
	cmd.PersistentFlags().BoolVar(&disableNodesOutput, "disable-nodes-output", false, "true to disable nodes stdout/stderr")
	cmd.PersistentFlags().StringVar(&snapshotsDir, "snapshots-dir", "", "directory for snapshots")

	return cmd
}

func serverFunc(*cobra.Command, []string) (err error) {
	if logDir == "" {
		anrRootDir := filepath.Join(os.TempDir(), constants.RootDirPrefix)
		err = os.MkdirAll(anrRootDir, os.ModePerm)
		if err != nil {
			return err
		}
		serverRootDir := filepath.Join(anrRootDir, serverRootDirPrefix)
		logDir, err = utils.MkDirWithTimestamp(serverRootDir)
		if err != nil {
			return err
		}
	}

	logLevel, err := log.ToLevel(logLevel)
	if err != nil {
		return err
	}

	logFactory := log.NewFactoryWithConfig(log.Config{
		RotatingWriterConfig: log.RotatingWriterConfig{
			Directory: logDir,
		},
		DisplayLevel: logLevel,
		LogLevel:     logLevel,
	})
	logger, err := logFactory.Make(constants.LogNameMain)
	if err != nil {
		return err
	}

	s, err := server.New(server.Config{
		Port:                port,
		GwPort:              gwPort,
		DialTimeout:         dialTimeout,
		RedirectNodesOutput: !disableNodesOutput,
		SnapshotsDir:        snapshotsDir,
		LogLevel:            logLevel,
	}, logger)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errChan := make(chan error)
	go func() {
		errChan <- s.Run(ctx)
	}()

	// Relay SIGINT and SIGTERM to [sigChan]
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigChan:
		// Got a SIGINT or SIGTERM; stop the server and wait for it to finish.
		logger.Warn("signal received: closing server", log.String("signal", sig.String()))
		cancel()
		waitForServerStop := <-errChan
		logger.Warn("closed server", log.Err(waitForServerStop))
	case serverClosed := <-errChan:
		// The server stopped.
		logger.Warn("server closed", log.Err(serverClosed))
	}
	return nil
}
