// Copyright (C) 2021-2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package zaprpc

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/luxfi/zap"
)

// ClientConfig configures a netrunner ZAP client.
type ClientConfig struct {
	// NodeID is this client's identifier (sent during handshake).
	NodeID string
	// ServerAddr is "host:port" of the netrunner ZAP server.
	ServerAddr string
	// DialTimeout bounds the initial TCP+handshake.
	DialTimeout time.Duration
	Logger      *slog.Logger
}

// Client is a typed RPC client against a netrunner ZAP server.
//
// Wire layer only — domain-specific methods live in netrunner/client.
type Client struct {
	node       *zap.Node
	serverID   string
	closed     chan struct{}
	closeOnce  sync.Once
	dialedOnce sync.Once
}

// NewClient connects to the netrunner ZAP server at cfg.ServerAddr.
func NewClient(cfg ClientConfig) (*Client, error) {
	if cfg.NodeID == "" {
		cfg.NodeID = "netrunner-client"
	}
	if cfg.DialTimeout == 0 {
		cfg.DialTimeout = 10 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	node := zap.NewNode(zap.NodeConfig{
		NodeID:      cfg.NodeID,
		ServiceType: "_netrunner._tcp",
		Port:        0,
		Logger:      cfg.Logger,
		NoDiscovery: true,
	})
	if err := node.Start(); err != nil {
		return nil, fmt.Errorf("zaprpc client start: %w", err)
	}

	dialCtx, cancel := context.WithTimeout(context.Background(), cfg.DialTimeout)
	defer cancel()
	if err := connectWithDeadline(dialCtx, node, cfg.ServerAddr); err != nil {
		node.Stop()
		return nil, err
	}

	// One netrunner client talks to exactly one server — pick that peer's
	// node ID once. ConnectDirect handshake learned it.
	peers := node.Peers()
	if len(peers) == 0 {
		node.Stop()
		return nil, fmt.Errorf("zaprpc client: no peer after connecting to %s", cfg.ServerAddr)
	}

	return &Client{
		node:     node,
		serverID: peers[0],
		closed:   make(chan struct{}),
	}, nil
}

// Call sends a typed request and waits for the typed response. ctx bounds
// the round trip. JSON encode/decode + ZAP framing happen inside.
func Call[Req, Resp any](ctx context.Context, c *Client, msgType MsgType, req *Req) (*Resp, error) {
	raw, err := Encode(msgType, req, "")
	if err != nil {
		return nil, err
	}
	msg, err := zap.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("zaprpc call: parse outgoing: %w", err)
	}

	respMsg, err := c.node.Call(ctx, c.serverID, msg)
	if err != nil {
		return nil, err
	}

	var resp Resp
	if err := Decode(respMsg, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CallVoid is Call's sibling for methods whose request struct is empty —
// avoids forcing the caller to type `&rpcpb.HealthRequest{}` for every ping.
func CallVoid[Resp any](ctx context.Context, c *Client, msgType MsgType) (*Resp, error) {
	type empty struct{}
	return Call[empty, Resp](ctx, c, msgType, &empty{})
}

// Close stops the underlying ZAP node.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
		c.node.Stop()
	})
	return nil
}

// Done returns a channel closed when the client is shut down.
func (c *Client) Done() <-chan struct{} {
	return c.closed
}

// Node returns the underlying ZAP node — used by streaming subscriptions
// that need to install a server-push handler before issuing the subscribe call.
func (c *Client) Node() *zap.Node {
	return c.node
}

// ServerID returns the connected server's node ID. Useful for explicit
// targeting in custom ZAP messages.
func (c *Client) ServerID() string {
	return c.serverID
}

// connectWithDeadline retries ConnectDirect until the context expires.
// First-boot races are common — the server's listener may not be up yet
// even though the client has been told to dial.
func connectWithDeadline(ctx context.Context, n *zap.Node, addr string) error {
	var lastErr error
	for {
		if err := n.ConnectDirect(addr); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("zaprpc client: dial %s timed out: %w", addr, lastErr)
		case <-time.After(100 * time.Millisecond):
		}
	}
}
