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

	// captured request payloads for round-trip-integrity assertions
	lastAddNode    *types.AddNodeRequest
	lastRemoveNode *types.RemoveNodeRequest
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

// addNodeLast records the last AddNode request seen, for round-trip
// integrity assertions in tests.
func (b *stubBackend) AddNode(ctx context.Context, req *types.AddNodeRequest) (*types.AddNodeResponse, error) {
	b.lastAddNode = req
	return &types.AddNodeResponse{
		ClusterInfo: &types.ClusterInfo{
			NodeNames: []string{"alpha", req.Name},
			NodeInfos: map[string]*types.NodeInfo{
				req.Name: {Name: req.Name, ExecPath: req.ExecPath},
			},
			PID:         42,
			RootDataDir: "/tmp/netrunner",
			Healthy:     true,
			NetworkID:   12345,
		},
	}, nil
}

func (b *stubBackend) RemoveNode(ctx context.Context, req *types.RemoveNodeRequest) (*types.RemoveNodeResponse, error) {
	b.lastRemoveNode = req
	return &types.RemoveNodeResponse{
		ClusterInfo: &types.ClusterInfo{
			NodeNames:   []string{"alpha"},
			NodeInfos:   map[string]*types.NodeInfo{},
			PID:         42,
			RootDataDir: "/tmp/netrunner",
			Healthy:     true,
			NetworkID:   12345,
		},
	}, nil
}

func (b *stubBackend) Stop(ctx context.Context) (*types.StopResponse, error) {
	return &types.StopResponse{
		ClusterInfo: &types.ClusterInfo{
			NodeNames:   []string{},
			NodeInfos:   map[string]*types.NodeInfo{},
			PID:         42,
			RootDataDir: "/tmp/netrunner",
			Healthy:     false,
			NetworkID:   12345,
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

func TestE2EAddNode(t *testing.T) {
	be := &stubBackend{}
	srv, teardown := runServer(t, be)
	defer teardown()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := Dial(ctx, srv.Addr())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	req := &types.AddNodeRequest{
		Name:          "gamma",
		ExecPath:      "/usr/local/bin/luxd",
		NodeConfig:    `{"http-port":9650}`,
		NodeConfigSet: true,
		ChainConfigs: map[string]string{
			"C": `{"foo":"bar"}`,
			"X": `{"baz":"qux"}`,
		},
		UpgradeConfigs:   map[string]string{"C": `{}`},
		ChainConfigFiles: map[string]string{"chain-1": `{}`},
		PluginDir:        "/usr/local/lib/lux/plugins",
	}
	resp, err := client.AddNode(ctx, req)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if resp.ClusterInfo == nil {
		t.Fatal("ClusterInfo: nil")
	}
	if be.lastAddNode == nil {
		t.Fatal("server did not see AddNode request")
	}
	if be.lastAddNode.Name != "gamma" {
		t.Fatalf("Name: got %q", be.lastAddNode.Name)
	}
	if be.lastAddNode.NodeConfig != `{"http-port":9650}` || !be.lastAddNode.NodeConfigSet {
		t.Fatalf("NodeConfig round-trip: %+v", be.lastAddNode)
	}
	if len(be.lastAddNode.ChainConfigs) != 2 ||
		be.lastAddNode.ChainConfigs["C"] != `{"foo":"bar"}` ||
		be.lastAddNode.ChainConfigs["X"] != `{"baz":"qux"}` {
		t.Fatalf("ChainConfigs round-trip: %+v", be.lastAddNode.ChainConfigs)
	}
	if be.lastAddNode.PluginDir != "/usr/local/lib/lux/plugins" {
		t.Fatalf("PluginDir: %q", be.lastAddNode.PluginDir)
	}
}

func TestE2ERemoveNode(t *testing.T) {
	be := &stubBackend{}
	srv, teardown := runServer(t, be)
	defer teardown()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := Dial(ctx, srv.Addr())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	resp, err := client.RemoveNode(ctx, &types.RemoveNodeRequest{Name: "beta"})
	if err != nil {
		t.Fatalf("RemoveNode: %v", err)
	}
	if resp.ClusterInfo == nil {
		t.Fatal("ClusterInfo: nil")
	}
	if be.lastRemoveNode == nil || be.lastRemoveNode.Name != "beta" {
		t.Fatalf("Name round-trip: %+v", be.lastRemoveNode)
	}
}

func TestE2EStop(t *testing.T) {
	srv, teardown := runServer(t, &stubBackend{})
	defer teardown()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := Dial(ctx, srv.Addr())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	resp, err := client.Stop(ctx)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if resp.ClusterInfo == nil {
		t.Fatal("ClusterInfo: nil")
	}
	if resp.ClusterInfo.Healthy {
		t.Fatal("Healthy: should be false post-stop")
	}
	if resp.ClusterInfo.NetworkID != 12345 {
		t.Fatalf("NetworkID: got %d, want 12345", resp.ClusterInfo.NetworkID)
	}
}
