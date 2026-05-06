// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zapwire

import (
	"context"
	"testing"
	"time"

	"github.com/luxfi/netrunner/zapwire/types"
)

// runServer starts a zapwire server on 127.0.0.1:0 and returns a
// teardown that the test must call (via defer or t.Cleanup). It also
// gives a context whose cancel is wired into the teardown, so handlers
// see ctx.Done() before the listener closes.
func runServer(t *testing.T, be Backend) (*Server, func()) {
	t.Helper()
	srv, err := NewServer("127.0.0.1:0", be)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = srv.Serve(ctx)
		close(done)
	}()
	// Wait for listener to actually be accepting.
	time.Sleep(20 * time.Millisecond)
	teardown := func() {
		cancel()
		_ = srv.Close()
		<-done
	}
	return srv, teardown
}

// stubBackend is a minimal Backend implementation for e2e tests.
type stubBackend struct {
	pingPID int32
}

func (b *stubBackend) Ping(ctx context.Context) (*types.PingResponse, error) {
	return &types.PingResponse{PID: b.pingPID}, nil
}

func (b *stubBackend) RPCVersion(ctx context.Context) (*types.RPCVersionResponse, error) {
	return &types.RPCVersionResponse{Version: ProtocolVersion}, nil
}

func (b *stubBackend) Health(ctx context.Context) (*types.HealthResponse, error) {
	return &types.HealthResponse{
		ClusterInfo: &types.ClusterInfo{
			NodeNames:   []string{"alpha", "beta"},
			NodeInfos:   map[string]*types.NodeInfo{},
			PID:         42,
			RootDataDir: "/tmp/netrunner",
			Healthy:     true,
			NetworkID:   12345,
		},
	}, nil
}

func (b *stubBackend) URIs(ctx context.Context) (*types.URIsResponse, error) {
	return &types.URIsResponse{
		URIs: []string{"http://127.0.0.1:9650", "http://127.0.0.1:9652"},
	}, nil
}

func (b *stubBackend) Status(ctx context.Context) (*types.StatusResponse, error) {
	return &types.StatusResponse{
		ClusterInfo: &types.ClusterInfo{
			NodeNames: []string{"alpha"},
			NodeInfos: map[string]*types.NodeInfo{
				"alpha": {
					Name:               "alpha",
					ExecPath:           "/usr/local/bin/luxd",
					URI:                "http://127.0.0.1:9650",
					ID:                 "NodeID-1",
					LogDir:             "/tmp/log",
					DBDir:              "/tmp/db",
					Config:             []byte(`{"network-id":1}`),
					PluginDir:          "/usr/local/lib/lux/plugins",
					WhitelistedSubnets: "",
					Paused:             false,
				},
			},
			PID:         42,
			RootDataDir: "/tmp/netrunner",
			Healthy:     true,
			CustomChains: map[string]*types.ChainInfo{
				"chain-1": {
					ChainName: "C",
					VMID:      "mgj786NP7uDwBCcq6YwThhaN8FLyybkCa4zBWTQbNgmK6k9A6",
					VMName:    "evm",
					ChainID:   "chain-1",
					SubnetID:  "primary",
					Genesis:   []byte(`{"chainId":96369}`),
				},
			},
			Subnets:   []string{"primary"},
			NetworkID: 96369,
		},
	}, nil
}

func TestE2EPing(t *testing.T) {
	srv, teardown := runServer(t, &stubBackend{pingPID: 42})
	defer teardown()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := Dial(ctx, srv.Addr())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	resp, err := client.Ping(ctx)
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if resp.PID != 42 {
		t.Fatalf("PID: got %d, want 42", resp.PID)
	}
}

func TestE2ERPCVersion(t *testing.T) {
	srv, teardown := runServer(t, &stubBackend{})
	defer teardown()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := Dial(ctx, srv.Addr())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	resp, err := client.RPCVersion(ctx)
	if err != nil {
		t.Fatalf("RPCVersion: %v", err)
	}
	if resp.Version != ProtocolVersion {
		t.Fatalf("Version: got %d, want %d", resp.Version, ProtocolVersion)
	}
}

func TestE2EHealth(t *testing.T) {
	srv, teardown := runServer(t, &stubBackend{})
	defer teardown()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := Dial(ctx, srv.Addr())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	resp, err := client.Health(ctx)
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if resp.ClusterInfo == nil {
		t.Fatal("ClusterInfo: nil")
	}
	if !resp.ClusterInfo.Healthy {
		t.Fatal("Healthy: false")
	}
	if resp.ClusterInfo.NetworkID != 12345 {
		t.Fatalf("NetworkID: got %d, want 12345", resp.ClusterInfo.NetworkID)
	}
	if len(resp.ClusterInfo.NodeNames) != 2 {
		t.Fatalf("NodeNames: got %d, want 2", len(resp.ClusterInfo.NodeNames))
	}
}

func TestE2EURIs(t *testing.T) {
	srv, teardown := runServer(t, &stubBackend{})
	defer teardown()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := Dial(ctx, srv.Addr())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	uris, err := client.URIs(ctx)
	if err != nil {
		t.Fatalf("URIs: %v", err)
	}
	if len(uris) != 2 {
		t.Fatalf("URIs: got %d, want 2", len(uris))
	}
}

func TestE2EStatus(t *testing.T) {
	srv, teardown := runServer(t, &stubBackend{})
	defer teardown()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := Dial(ctx, srv.Addr())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	resp, err := client.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if resp.ClusterInfo == nil {
		t.Fatal("ClusterInfo: nil")
	}
	ni := resp.ClusterInfo.NodeInfos["alpha"]
	if ni == nil || ni.Name != "alpha" {
		t.Fatalf("NodeInfos[alpha] missing or wrong: %+v", ni)
	}
	chain := resp.ClusterInfo.CustomChains["chain-1"]
	if chain == nil {
		t.Fatal("CustomChains[chain-1]: nil")
	}
	if chain.VMID != "mgj786NP7uDwBCcq6YwThhaN8FLyybkCa4zBWTQbNgmK6k9A6" {
		t.Fatalf("VMID: got %s", chain.VMID)
	}
	if string(chain.Genesis) != `{"chainId":96369}` {
		t.Fatalf("Genesis round-trip mangled: %s", chain.Genesis)
	}
}
