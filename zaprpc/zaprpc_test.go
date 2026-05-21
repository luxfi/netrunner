// Copyright (C) 2021-2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package zaprpc_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/luxfi/netrunner/zaprpc"
)

// Echo is the smallest possible RPC for the round-trip test.
type EchoReq struct {
	Message string `json:"message"`
}

type EchoResp struct {
	Echo string `json:"echo"`
	N    int    `json:"n"`
}

// TestRoundTrip wires a dispatcher with one bound method and proves the
// envelope+JSON+ZAP path round-trips end to end.
func TestRoundTrip(t *testing.T) {
	// MsgTypes use the upper 8 bits of the ZAP flags field, so values must
	// fit in uint8. Pick something outside the production range (1..26).
	const echoMsg zaprpc.MsgType = 200

	disp := zaprpc.NewDispatcher()
	zaprpc.Bind[EchoReq, EchoResp](disp, echoMsg, func(_ context.Context, req *EchoReq) (*EchoResp, error) {
		return &EchoResp{Echo: req.Message, N: len(req.Message)}, nil
	})

	port := freePort(t)
	srv := zaprpc.NewServer(zaprpc.ServerConfig{
		NodeID: "test-server",
		Port:   port,
	}, disp)
	if err := srv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer srv.Stop()

	cli, err := zaprpc.NewClient(zaprpc.ClientConfig{
		NodeID:      "test-client",
		ServerAddr:  fmt.Sprintf("127.0.0.1:%d", port),
		DialTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := zaprpc.Call[EchoReq, EchoResp](ctx, cli, echoMsg, &EchoReq{Message: "hello"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if resp.Echo != "hello" || resp.N != 5 {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

// TestErrorPropagation exercises the errStr return path.
func TestErrorPropagation(t *testing.T) {
	const failMsg zaprpc.MsgType = 201

	disp := zaprpc.NewDispatcher()
	zaprpc.Bind[EchoReq, EchoResp](disp, failMsg, func(_ context.Context, _ *EchoReq) (*EchoResp, error) {
		return nil, fmt.Errorf("deliberate failure")
	})

	port := freePort(t)
	srv := zaprpc.NewServer(zaprpc.ServerConfig{NodeID: "err-server", Port: port}, disp)
	if err := srv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer srv.Stop()

	cli, err := zaprpc.NewClient(zaprpc.ClientConfig{
		NodeID:      "err-client",
		ServerAddr:  fmt.Sprintf("127.0.0.1:%d", port),
		DialTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err = zaprpc.Call[EchoReq, EchoResp](ctx, cli, failMsg, &EchoReq{Message: "ignored"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "deliberate failure" {
		t.Fatalf("expected 'deliberate failure', got %q", err.Error())
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return port
}
