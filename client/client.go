// Copyright (C) 2021-2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

// Package client is the netrunner control-plane RPC client. Wire is ZAP
// (luxfi/zap) — no gRPC, no grpc-gateway.
package client

import (
	"context"
	"sync"
	"time"

	log "github.com/luxfi/log"
	"github.com/luxfi/netrunner/local"
	"github.com/luxfi/netrunner/rpcpb"
	"github.com/luxfi/netrunner/zaprpc"
)

type Config struct {
	// Endpoint is "host:port" of the netrunner ZAP server, e.g. "127.0.0.1:8546".
	Endpoint    string
	DialTimeout time.Duration
}

type Client interface {
	Ping(ctx context.Context) (*rpcpb.PingResponse, error)
	RPCVersion(ctx context.Context) (*rpcpb.RPCVersionResponse, error)
	Start(ctx context.Context, execPath string, opts ...OpOption) (*rpcpb.StartResponse, error)
	// CreateChains creates chains with VMs and genesis (uses rpcpb.CreateBlockchains)
	CreateChains(ctx context.Context, chainSpecs []*rpcpb.BlockchainSpec) (*rpcpb.CreateBlockchainsResponse, error)
	// CreateParticipantGroups creates participant groups (uses rpcpb.CreateChains)
	CreateParticipantGroups(ctx context.Context, participantsSpecs []*rpcpb.ChainSpec) (*rpcpb.CreateChainsResponse, error)
	TransformElasticChains(ctx context.Context, elasticChainSpecs []*rpcpb.ElasticChainSpec) (*rpcpb.TransformElasticChainsResponse, error)
	AddPermissionlessValidator(ctx context.Context, validatorSpec []*rpcpb.PermissionlessValidatorSpec) (*rpcpb.AddPermissionlessValidatorResponse, error)
	RemoveChainValidator(ctx context.Context, validatorSpec []*rpcpb.RemoveChainValidatorSpec) (*rpcpb.RemoveChainValidatorResponse, error)
	Health(ctx context.Context) (*rpcpb.HealthResponse, error)
	WaitForHealthy(ctx context.Context) (*rpcpb.WaitForHealthyResponse, error)
	URIs(ctx context.Context) ([]string, error)
	Status(ctx context.Context) (*rpcpb.StatusResponse, error)
	StreamStatus(ctx context.Context, pushInterval time.Duration) (<-chan *rpcpb.ClusterInfo, error)
	RemoveNode(ctx context.Context, name string) (*rpcpb.RemoveNodeResponse, error)
	PauseNode(ctx context.Context, name string) (*rpcpb.PauseNodeResponse, error)
	ResumeNode(ctx context.Context, name string) (*rpcpb.ResumeNodeResponse, error)
	RestartNode(ctx context.Context, name string, opts ...OpOption) (*rpcpb.RestartNodeResponse, error)
	AddNode(ctx context.Context, name string, execPath string, opts ...OpOption) (*rpcpb.AddNodeResponse, error)
	Stop(ctx context.Context) (*rpcpb.StopResponse, error)
	AttachPeer(ctx context.Context, nodeName string) (*rpcpb.AttachPeerResponse, error)
	SendOutboundMessage(ctx context.Context, nodeName string, peerID string, op uint32, msgBody []byte) (*rpcpb.SendOutboundMessageResponse, error)
	Close() error
	SaveSnapshot(ctx context.Context, snapshotName string) (*rpcpb.SaveSnapshotResponse, error)
	SaveHotSnapshot(ctx context.Context, snapshotName string) (*rpcpb.SaveSnapshotResponse, error)
	LoadSnapshot(ctx context.Context, snapshotName string, opts ...OpOption) (*rpcpb.LoadSnapshotResponse, error)
	RemoveSnapshot(ctx context.Context, snapshotName string) (*rpcpb.RemoveSnapshotResponse, error)
	GetSnapshotNames(ctx context.Context) ([]string, error)
}

type client struct {
	cfg    Config
	logger log.Logger

	zc *zaprpc.Client

	closed    chan struct{}
	closeOnce sync.Once
}

func New(cfg Config, logger log.Logger) (Client, error) {
	if cfg.DialTimeout == 0 {
		cfg.DialTimeout = 10 * time.Second
	}
	log.Debug("dialing netrunner ZAP server", log.String("endpoint", cfg.Endpoint))

	zc, err := zaprpc.NewClient(zaprpc.ClientConfig{
		NodeID:      "netrunner-client",
		ServerAddr:  cfg.Endpoint,
		DialTimeout: cfg.DialTimeout,
	})
	if err != nil {
		return nil, err
	}

	return &client{
		cfg:    cfg,
		logger: logger,
		zc:     zc,
		closed: make(chan struct{}),
	}, nil
}

func (c *client) Ping(ctx context.Context) (*rpcpb.PingResponse, error) {
	c.logger.Info("ping")
	return zaprpc.Call[rpcpb.PingRequest, rpcpb.PingResponse](ctx, c.zc, zaprpc.MsgPing, &rpcpb.PingRequest{})
}

func (c *client) RPCVersion(ctx context.Context) (*rpcpb.RPCVersionResponse, error) {
	c.logger.Info("rpc version")
	return zaprpc.Call[rpcpb.RPCVersionRequest, rpcpb.RPCVersionResponse](ctx, c.zc, zaprpc.MsgRPCVersion, &rpcpb.RPCVersionRequest{})
}

func (c *client) Start(ctx context.Context, execPath string, opts ...OpOption) (*rpcpb.StartResponse, error) {
	ret := &Op{numNodes: local.DefaultNumNodes}
	ret.applyOpts(opts)

	req := &rpcpb.StartRequest{
		ExecPath:         execPath,
		NumNodes:         &ret.numNodes,
		ChainConfigs:     ret.chainConfigs,
		UpgradeConfigs:   ret.upgradeConfigs,
		ChainConfigFiles: ret.chainConfigs,
	}
	if ret.trackChains != "" {
		req.WhitelistedChains = &ret.trackChains
	}
	if ret.rootDataDir != "" {
		req.RootDataDir = &ret.rootDataDir
	}
	if ret.pluginDir != "" {
		req.PluginDir = ret.pluginDir
	}
	if len(ret.chainSpecs) > 0 {
		req.BlockchainSpecs = ret.chainSpecs
	}
	if ret.globalNodeConfig != "" {
		req.GlobalNodeConfig = &ret.globalNodeConfig
	}
	if ret.customNodeConfigs != nil {
		req.CustomNodeConfigs = ret.customNodeConfigs
	}
	req.ReassignPortsIfUsed = &ret.reassignPortsIfUsed
	req.DynamicPorts = &ret.dynamicPorts

	c.logger.Info("start")
	return zaprpc.Call[rpcpb.StartRequest, rpcpb.StartResponse](ctx, c.zc, zaprpc.MsgStart, req)
}

func (c *client) CreateChains(ctx context.Context, chainSpecs []*rpcpb.BlockchainSpec) (*rpcpb.CreateBlockchainsResponse, error) {
	c.logger.Info("create chains")
	return zaprpc.Call[rpcpb.CreateBlockchainsRequest, rpcpb.CreateBlockchainsResponse](ctx, c.zc, zaprpc.MsgCreateBlockchains, &rpcpb.CreateBlockchainsRequest{BlockchainSpecs: chainSpecs})
}

func (c *client) CreateParticipantGroups(ctx context.Context, participantsSpecs []*rpcpb.ChainSpec) (*rpcpb.CreateChainsResponse, error) {
	c.logger.Info("create participant groups")
	return zaprpc.Call[rpcpb.CreateChainsRequest, rpcpb.CreateChainsResponse](ctx, c.zc, zaprpc.MsgCreateChains, &rpcpb.CreateChainsRequest{ChainSpecs: participantsSpecs})
}

func (c *client) TransformElasticChains(ctx context.Context, elasticChainSpecs []*rpcpb.ElasticChainSpec) (*rpcpb.TransformElasticChainsResponse, error) {
	c.logger.Info("transform chains")
	return zaprpc.Call[rpcpb.TransformElasticChainsRequest, rpcpb.TransformElasticChainsResponse](ctx, c.zc, zaprpc.MsgTransformElasticChains, &rpcpb.TransformElasticChainsRequest{ElasticChainSpec: elasticChainSpecs})
}

func (c *client) AddPermissionlessValidator(ctx context.Context, validatorSpec []*rpcpb.PermissionlessValidatorSpec) (*rpcpb.AddPermissionlessValidatorResponse, error) {
	c.logger.Info("add permissionless validators to elastic chains")
	return zaprpc.Call[rpcpb.AddPermissionlessValidatorRequest, rpcpb.AddPermissionlessValidatorResponse](ctx, c.zc, zaprpc.MsgAddPermissionlessValidator, &rpcpb.AddPermissionlessValidatorRequest{ValidatorSpec: validatorSpec})
}

func (c *client) RemoveChainValidator(ctx context.Context, validatorSpec []*rpcpb.RemoveChainValidatorSpec) (*rpcpb.RemoveChainValidatorResponse, error) {
	c.logger.Info("remove chain validator")
	return zaprpc.Call[rpcpb.RemoveChainValidatorRequest, rpcpb.RemoveChainValidatorResponse](ctx, c.zc, zaprpc.MsgRemoveChainValidator, &rpcpb.RemoveChainValidatorRequest{ValidatorSpec: validatorSpec})
}

func (c *client) Health(ctx context.Context) (*rpcpb.HealthResponse, error) {
	c.logger.Info("health")
	return zaprpc.Call[rpcpb.HealthRequest, rpcpb.HealthResponse](ctx, c.zc, zaprpc.MsgHealth, &rpcpb.HealthRequest{})
}

func (c *client) WaitForHealthy(ctx context.Context) (*rpcpb.WaitForHealthyResponse, error) {
	c.logger.Info("wait for healthy")
	return zaprpc.Call[rpcpb.WaitForHealthyRequest, rpcpb.WaitForHealthyResponse](ctx, c.zc, zaprpc.MsgWaitForHealthy, &rpcpb.WaitForHealthyRequest{})
}

func (c *client) URIs(ctx context.Context) ([]string, error) {
	c.logger.Info("uris")
	resp, err := zaprpc.Call[rpcpb.URIsRequest, rpcpb.URIsResponse](ctx, c.zc, zaprpc.MsgURIs, &rpcpb.URIsRequest{})
	if err != nil {
		return nil, err
	}
	return resp.Uris, nil
}

func (c *client) Status(ctx context.Context) (*rpcpb.StatusResponse, error) {
	c.logger.Info("status")
	return zaprpc.Call[rpcpb.StatusRequest, rpcpb.StatusResponse](ctx, c.zc, zaprpc.MsgStatus, &rpcpb.StatusRequest{})
}

// StreamStatus emits one ClusterInfo per pushInterval into the returned
// channel until ctx or the client is closed. ZAP doesn't model
// server-streams natively, so this is a client-driven Status() poll —
// observably identical to the previous gRPC server-streaming RPC,
// strictly less plumbing.
func (c *client) StreamStatus(ctx context.Context, pushInterval time.Duration) (<-chan *rpcpb.ClusterInfo, error) {
	if pushInterval <= 0 {
		pushInterval = time.Second
	}
	ch := make(chan *rpcpb.ClusterInfo, 1)
	go func() {
		defer close(ch)
		ticker := time.NewTicker(pushInterval)
		defer ticker.Stop()
		for {
			resp, err := c.Status(ctx)
			if err != nil {
				c.logger.Debug("status poll error", log.Err(err))
				return
			}
			select {
			case ch <- resp.ClusterInfo:
			case <-ctx.Done():
				return
			case <-c.closed:
				return
			}
			select {
			case <-ticker.C:
			case <-ctx.Done():
				return
			case <-c.closed:
				return
			}
		}
	}()
	return ch, nil
}

func (c *client) Stop(ctx context.Context) (*rpcpb.StopResponse, error) {
	c.logger.Info("stop")
	return zaprpc.Call[rpcpb.StopRequest, rpcpb.StopResponse](ctx, c.zc, zaprpc.MsgStop, &rpcpb.StopRequest{})
}

func (c *client) AddNode(ctx context.Context, name string, execPath string, opts ...OpOption) (*rpcpb.AddNodeResponse, error) {
	ret := &Op{}
	ret.applyOpts(opts)

	req := &rpcpb.AddNodeRequest{
		Name:             name,
		ExecPath:         execPath,
		NodeConfig:       &ret.globalNodeConfig,
		ChainConfigs:     ret.chainConfigs,
		UpgradeConfigs:   ret.upgradeConfigs,
		ChainConfigFiles: ret.chainConfigs,
	}
	if ret.pluginDir != "" {
		req.PluginDir = ret.pluginDir
	}

	c.logger.Info("add node", log.String("name", name))
	return zaprpc.Call[rpcpb.AddNodeRequest, rpcpb.AddNodeResponse](ctx, c.zc, zaprpc.MsgAddNode, req)
}

func (c *client) RemoveNode(ctx context.Context, name string) (*rpcpb.RemoveNodeResponse, error) {
	c.logger.Info("remove node", log.String("name", name))
	return zaprpc.Call[rpcpb.RemoveNodeRequest, rpcpb.RemoveNodeResponse](ctx, c.zc, zaprpc.MsgRemoveNode, &rpcpb.RemoveNodeRequest{Name: name})
}

func (c *client) PauseNode(ctx context.Context, name string) (*rpcpb.PauseNodeResponse, error) {
	c.logger.Info("pause node", log.String("name", name))
	return zaprpc.Call[rpcpb.PauseNodeRequest, rpcpb.PauseNodeResponse](ctx, c.zc, zaprpc.MsgPauseNode, &rpcpb.PauseNodeRequest{Name: name})
}

func (c *client) ResumeNode(ctx context.Context, name string) (*rpcpb.ResumeNodeResponse, error) {
	c.logger.Info("resume node", log.String("name", name))
	return zaprpc.Call[rpcpb.ResumeNodeRequest, rpcpb.ResumeNodeResponse](ctx, c.zc, zaprpc.MsgResumeNode, &rpcpb.ResumeNodeRequest{Name: name})
}

func (c *client) RestartNode(ctx context.Context, name string, opts ...OpOption) (*rpcpb.RestartNodeResponse, error) {
	ret := &Op{}
	ret.applyOpts(opts)

	req := &rpcpb.RestartNodeRequest{Name: name}
	if ret.execPath != "" {
		req.ExecPath = &ret.execPath
	}
	if ret.pluginDir != "" {
		req.PluginDir = ret.pluginDir
	}
	if ret.trackChains != "" {
		req.WhitelistedChains = &ret.trackChains
	}
	req.ChainConfigs = ret.chainConfigs
	req.UpgradeConfigs = ret.upgradeConfigs
	req.ChainConfigFiles = ret.chainConfigs

	c.logger.Info("restart node", log.String("name", name))
	return zaprpc.Call[rpcpb.RestartNodeRequest, rpcpb.RestartNodeResponse](ctx, c.zc, zaprpc.MsgRestartNode, req)
}

func (c *client) AttachPeer(ctx context.Context, nodeName string) (*rpcpb.AttachPeerResponse, error) {
	c.logger.Info("attaching peer", log.String("name", nodeName))
	return zaprpc.Call[rpcpb.AttachPeerRequest, rpcpb.AttachPeerResponse](ctx, c.zc, zaprpc.MsgAttachPeer, &rpcpb.AttachPeerRequest{NodeName: nodeName})
}

func (c *client) SendOutboundMessage(ctx context.Context, nodeName string, peerID string, op uint32, msgBody []byte) (*rpcpb.SendOutboundMessageResponse, error) {
	c.logger.Info("sending outbound message", log.String("name", nodeName), log.String("peer-ID", peerID))
	return zaprpc.Call[rpcpb.SendOutboundMessageRequest, rpcpb.SendOutboundMessageResponse](ctx, c.zc, zaprpc.MsgSendOutboundMessage, &rpcpb.SendOutboundMessageRequest{
		NodeName: nodeName,
		PeerId:   peerID,
		Op:       op,
		Bytes:    msgBody,
	})
}

func (c *client) SaveSnapshot(ctx context.Context, snapshotName string) (*rpcpb.SaveSnapshotResponse, error) {
	c.logger.Info("save snapshot", log.String("snapshot-name", snapshotName))
	return zaprpc.Call[rpcpb.SaveSnapshotRequest, rpcpb.SaveSnapshotResponse](ctx, c.zc, zaprpc.MsgSaveSnapshot, &rpcpb.SaveSnapshotRequest{SnapshotName: snapshotName})
}

func (c *client) SaveHotSnapshot(ctx context.Context, snapshotName string) (*rpcpb.SaveSnapshotResponse, error) {
	c.logger.Info("save hot snapshot", log.String("snapshot-name", snapshotName))
	return zaprpc.Call[rpcpb.SaveSnapshotRequest, rpcpb.SaveSnapshotResponse](ctx, c.zc, zaprpc.MsgSaveHotSnapshot, &rpcpb.SaveSnapshotRequest{SnapshotName: snapshotName})
}

func (c *client) LoadSnapshot(ctx context.Context, snapshotName string, opts ...OpOption) (*rpcpb.LoadSnapshotResponse, error) {
	c.logger.Info("load snapshot", log.String("snapshot-name", snapshotName))
	ret := &Op{}
	ret.applyOpts(opts)
	req := rpcpb.LoadSnapshotRequest{
		SnapshotName:     snapshotName,
		ChainConfigs:     ret.chainConfigs,
		UpgradeConfigs:   ret.upgradeConfigs,
		ChainConfigFiles: ret.chainConfigs,
	}
	if ret.execPath != "" {
		req.ExecPath = &ret.execPath
	}
	if ret.pluginDir != "" {
		req.PluginDir = ret.pluginDir
	}
	if ret.rootDataDir != "" {
		req.RootDataDir = &ret.rootDataDir
	}
	if ret.globalNodeConfig != "" {
		req.GlobalNodeConfig = &ret.globalNodeConfig
	}
	req.ReassignPortsIfUsed = &ret.reassignPortsIfUsed
	return zaprpc.Call[rpcpb.LoadSnapshotRequest, rpcpb.LoadSnapshotResponse](ctx, c.zc, zaprpc.MsgLoadSnapshot, &req)
}

func (c *client) RemoveSnapshot(ctx context.Context, snapshotName string) (*rpcpb.RemoveSnapshotResponse, error) {
	c.logger.Info("remove snapshot", log.String("snapshot-name", snapshotName))
	return zaprpc.Call[rpcpb.RemoveSnapshotRequest, rpcpb.RemoveSnapshotResponse](ctx, c.zc, zaprpc.MsgRemoveSnapshot, &rpcpb.RemoveSnapshotRequest{SnapshotName: snapshotName})
}

func (c *client) GetSnapshotNames(ctx context.Context) ([]string, error) {
	c.logger.Info("get snapshot names")
	resp, err := zaprpc.Call[rpcpb.GetSnapshotNamesRequest, rpcpb.GetSnapshotNamesResponse](ctx, c.zc, zaprpc.MsgGetSnapshotNames, &rpcpb.GetSnapshotNamesRequest{})
	if err != nil {
		return nil, err
	}
	return resp.SnapshotNames, nil
}

func (c *client) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
	})
	return c.zc.Close()
}

type Op struct {
	numNodes            uint32
	execPath            string
	trackChains         string
	globalNodeConfig    string
	rootDataDir         string
	pluginDir           string
	chainSpecs          []*rpcpb.BlockchainSpec
	customNodeConfigs   map[string]string
	numChains           uint32
	chainConfigs        map[string]string
	upgradeConfigs      map[string]string
	pChainConfigs       map[string]string
	reassignPortsIfUsed bool
	dynamicPorts        bool
}

type OpOption func(*Op)

func (op *Op) applyOpts(opts []OpOption) {
	for _, opt := range opts {
		opt(op)
	}
}

func WithGlobalNodeConfig(nodeConfig string) OpOption {
	return func(op *Op) {
		op.globalNodeConfig = nodeConfig
	}
}

func WithNumNodes(numNodes uint32) OpOption {
	return func(op *Op) {
		op.numNodes = numNodes
	}
}

func WithExecPath(execPath string) OpOption {
	return func(op *Op) {
		op.execPath = execPath
	}
}

func WithWhitelistedChains(trackChains string) OpOption {
	return func(op *Op) {
		op.trackChains = trackChains
	}
}

func WithTrackChains(trackChains string) OpOption {
	return func(op *Op) {
		op.trackChains = trackChains
	}
}

func WithRootDataDir(rootDataDir string) OpOption {
	return func(op *Op) {
		op.rootDataDir = rootDataDir
	}
}

func WithPluginDir(pluginDir string) OpOption {
	return func(op *Op) {
		op.pluginDir = pluginDir
	}
}

// WithChainSpecs sets the chain specifications for creating chains with VMs
func WithChainSpecs(chainSpecs []*rpcpb.BlockchainSpec) OpOption {
	return func(op *Op) {
		op.chainSpecs = chainSpecs
	}
}

// Map from chain name to its configuration json contents.
func WithChainConfigs(chainConfigs map[string]string) OpOption {
	return func(op *Op) {
		op.chainConfigs = chainConfigs
	}
}

// Map from chain name to its upgrade json contents.
func WithUpgradeConfigs(upgradeConfigs map[string]string) OpOption {
	return func(op *Op) {
		op.upgradeConfigs = upgradeConfigs
	}
}

// Map from chain id to its configuration json contents.
func WithChainConfigFiles(chainConfigs map[string]string) OpOption {
	return func(op *Op) {
		op.chainConfigs = chainConfigs
	}
}

// Map from node name to its custom node config
func WithCustomNodeConfigs(customNodeConfigs map[string]string) OpOption {
	return func(op *Op) {
		op.customNodeConfigs = customNodeConfigs
	}
}

func WithNumChains(numChains uint32) OpOption {
	return func(op *Op) {
		op.numChains = numChains
	}
}

func WithReassignPortsIfUsed(reassignPortsIfUsed bool) OpOption {
	return func(op *Op) {
		op.reassignPortsIfUsed = reassignPortsIfUsed
	}
}

func WithDynamicPorts(dynamicPorts bool) OpOption {
	return func(op *Op) {
		op.dynamicPorts = dynamicPorts
	}
}
