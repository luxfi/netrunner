// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package zapwire provides a 100%-native ZAP client/server for the
// netrunner control protocol. No protobuf, no gRPC, no codegen.
//
// This package replaces the legacy netrunner/client/client.go (gRPC)
// and netrunner/server/server.go (gRPC) with a hand-written ZAP layer
// over luxfi/api/zap. The migration happens RPC-by-RPC; consumers
// switch to zapwire.Client when the server-side handler for a given
// op has been converted.
package zapwire

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/luxfi/api/zap"
	"github.com/luxfi/netrunner/zapwire/types"
)

// Wire-protocol version. Bumped on breaking changes to encoding.
const ProtocolVersion uint32 = 1

// Client is the netrunner control RPC client over ZAP.
type Client struct {
	conn *zap.Conn
}

// Dial opens a ZAP control connection to a netrunner server.
func Dial(ctx context.Context, addr string) (*Client, error) {
	conn, err := zap.Dial(ctx, addr, nil)
	if err != nil {
		return nil, fmt.Errorf("zapwire: dial %s: %w", addr, err)
	}
	return &Client{conn: conn}, nil
}

// Close releases the underlying connection.
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// call serializes req, performs the RPC, and decodes the response into
// resp. The on-the-wire payload format is:
//
//	[reqID:uint32 (added by zap.Conn.Call)] [req-body]
//
// and the response (post-flag-strip) is:
//
//	[reqID:uint32] [resp-body]
//
// zap.Conn.Call handles the request ID transparently.
func (c *Client) call(
	ctx context.Context,
	op zap.MessageType,
	req types.Encoder,
	resp types.Decoder,
) error {
	buf := zap.GetBuffer()
	defer zap.PutBuffer(buf)
	req.Encode(buf)
	body := append([]byte(nil), buf.Bytes()...)

	_, payload, err := c.conn.Call(ctx, op, body)
	if err != nil {
		return err
	}
	r := zap.NewReader(payload)
	return resp.Decode(r)
}

// callSub is the same as call but writes a SubOpcode byte before the
// request body. Used for opcodes that fanout multiple RPCs through
// shared opcode (see opcodes.go for the rationale).
func (c *Client) callSub(
	ctx context.Context,
	op zap.MessageType,
	sub SubOpcode,
	req types.Encoder,
	resp types.Decoder,
) error {
	buf := zap.GetBuffer()
	defer zap.PutBuffer(buf)
	buf.WriteUint8(uint8(sub))
	req.Encode(buf)
	body := append([]byte(nil), buf.Bytes()...)

	_, payload, err := c.conn.Call(ctx, op, body)
	if err != nil {
		return err
	}
	r := zap.NewReader(payload)
	return resp.Decode(r)
}

// ---- Public RPC methods ----

// Ping returns the netrunner server's PID.
func (c *Client) Ping(ctx context.Context) (*types.PingResponse, error) {
	resp := &types.PingResponse{}
	if err := c.call(ctx, OpPing, &types.PingRequest{}, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// RPCVersion returns the netrunner control wire-protocol version.
func (c *Client) RPCVersion(ctx context.Context) (*types.RPCVersionResponse, error) {
	resp := &types.RPCVersionResponse{}
	if err := c.call(ctx, OpRPCVersion, &types.RPCVersionRequest{}, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// Health returns a cluster-health snapshot.
func (c *Client) Health(ctx context.Context) (*types.HealthResponse, error) {
	resp := &types.HealthResponse{}
	if err := c.callSub(ctx, OpStart, SubHealth, &types.HealthRequest{}, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// URIs returns the public RPC endpoints of every running node.
func (c *Client) URIs(ctx context.Context) ([]string, error) {
	resp := &types.URIsResponse{}
	if err := c.callSub(ctx, OpStart, SubURIs, &types.URIsRequest{}, resp); err != nil {
		return nil, err
	}
	return resp.URIs, nil
}

// Status returns the full cluster status.
func (c *Client) Status(ctx context.Context) (*types.StatusResponse, error) {
	resp := &types.StatusResponse{}
	if err := c.callSub(ctx, OpStart, SubStatus, &types.StatusRequest{}, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// Stop terminates the cluster and returns the final snapshot.
func (c *Client) Stop(ctx context.Context) (*types.StopResponse, error) {
	resp := &types.StopResponse{}
	if err := c.call(ctx, OpStop, &types.StopRequest{}, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// ---- Compatibility helpers ----

// reqIDFromPayload is unused on the client side (zap.Conn handles IDs)
// but kept here as the canonical reader for tests + server-side code.
func reqIDFromPayload(payload []byte) (uint32, []byte, error) {
	if len(payload) < 4 {
		return 0, nil, errors.New("zapwire: payload too short for request ID")
	}
	return binary.BigEndian.Uint32(payload[:4]), payload[4:], nil
}

// peelSubOpcode reads the leading SubOpcode byte off a payload and
// returns the remainder. Server-side use only.
func peelSubOpcode(body []byte) (SubOpcode, []byte, error) {
	if len(body) < 1 {
		return 0, nil, errors.New("zapwire: missing sub-opcode byte")
	}
	return SubOpcode(body[0]), body[1:], nil
}

// readerFromBody returns a zap.Reader positioned at the start of the
// post-(reqID, sub-opcode) message body. Server handlers receive a
// payload that already had the request ID stripped by the transport,
// so they only need to peel the sub-opcode.
func readerFromBody(body []byte) *zap.Reader {
	return zap.NewReader(body)
}

// ensure the package doesn't drop the bytes import accidentally.
var _ = bytes.NewReader
