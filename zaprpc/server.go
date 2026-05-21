// Copyright (C) 2021-2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package zaprpc

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/luxfi/zap"
)

// ServerConfig configures a netrunner ZAP server.
type ServerConfig struct {
	NodeID string // human-readable identifier, e.g. "netrunner-server"
	Port   int    // TCP port to listen on
	Logger *slog.Logger
}

// Server is a thin wrapper around zap.Node that hosts a Dispatcher.
type Server struct {
	node *zap.Node
	disp *Dispatcher
}

// NewServer constructs a ZAP server that will dispatch to the handlers
// registered on the given Dispatcher.
func NewServer(cfg ServerConfig, disp *Dispatcher) *Server {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	node := zap.NewNode(zap.NodeConfig{
		NodeID:      cfg.NodeID,
		ServiceType: "_netrunner._tcp",
		Port:        cfg.Port,
		Logger:      cfg.Logger,
		NoDiscovery: true, // netrunner clients dial directly, no mDNS needed
	})
	disp.AttachTo(node)
	return &Server{node: node, disp: disp}
}

// Start binds the listener. Run inside its own goroutine — call Stop to wind down.
func (s *Server) Start() error {
	if err := s.node.Start(); err != nil {
		return fmt.Errorf("zaprpc server start: %w", err)
	}
	return nil
}

// Stop gracefully closes the listener and all peer connections.
func (s *Server) Stop() {
	s.node.Stop()
}

// Wait blocks until the server's context is canceled. Useful in main().
func (s *Server) Wait(ctx context.Context) {
	<-ctx.Done()
}

// Node returns the underlying ZAP node — primarily for tests and for
// server-side pushes to subscribed clients.
func (s *Server) Node() *zap.Node {
	return s.node
}
