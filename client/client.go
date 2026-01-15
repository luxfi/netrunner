// Copyright (C) 2021-2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

// Package client implements client.
package client

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	log "github.com/luxfi/log"
	"github.com/luxfi/netrunner/local"
	"github.com/luxfi/netrunner/rpcpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type Config struct {
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

	conn *grpc.ClientConn

	pingc    rpcpb.PingServiceClient
	controlc rpcpb.ControlServiceClient

	closed    chan struct{}
	closeOnce sync.Once
}

func New(cfg Config, logger log.Logger) (Client, error) {
	log.Debug("dialing server at ", log.String("endpoint", cfg.Endpoint))

	ctx, cancel := context.WithTimeout(context.Background(), cfg.DialTimeout)
	conn, err := grpc.DialContext(
		ctx,
		cfg.Endpoint,
		grpc.WithBlock(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	cancel()
	if err != nil {
		return nil, err
	}

	return &client{
		cfg:      cfg,
		logger:   logger,
		conn:     conn,
		pingc:    rpcpb.NewPingServiceClient(conn),
		controlc: rpcpb.NewControlServiceClient(conn),
		closed:   make(chan struct{}),
	}, nil
}

func (c *client) Ping(ctx context.Context) (*rpcpb.PingResponse, error) {
	c.logger.Info("ping")

	// ref. https://grpc-ecosystem.github.io/grpc-gateway/docs/tutorials/adding_annotations/
	// curl -X POST -k http://localhost:8081/v1/ping -d ''
	return c.pingc.Ping(ctx, &rpcpb.PingRequest{})
}

func (c *client) RPCVersion(ctx context.Context) (*rpcpb.RPCVersionResponse, error) {
	c.logger.Info("rpc version")
	return c.controlc.RPCVersion(ctx, &rpcpb.RPCVersionRequest{})
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
	return c.controlc.Start(ctx, req)
}

func (c *client) CreateChains(ctx context.Context, chainSpecs []*rpcpb.BlockchainSpec) (*rpcpb.CreateBlockchainsResponse, error) {
	req := &rpcpb.CreateBlockchainsRequest{
		BlockchainSpecs: chainSpecs,
	}

	c.logger.Info("create chains")
	return c.controlc.CreateBlockchains(ctx, req)
}

func (c *client) CreateParticipantGroups(ctx context.Context, participantsSpecs []*rpcpb.ChainSpec) (*rpcpb.CreateChainsResponse, error) {
	req := &rpcpb.CreateChainsRequest{
		ChainSpecs: participantsSpecs,
	}

	c.logger.Info("create participant groups")
	return c.controlc.CreateChains(ctx, req)
}

func (c *client) TransformElasticChains(ctx context.Context, elasticChainSpecs []*rpcpb.ElasticChainSpec) (*rpcpb.TransformElasticChainsResponse, error) {
	req := &rpcpb.TransformElasticChainsRequest{
		ElasticChainSpec: elasticChainSpecs,
	}

	c.logger.Info("transform chains")
	return c.controlc.TransformElasticChains(ctx, req)
}

func (c *client) AddPermissionlessValidator(ctx context.Context, validatorSpec []*rpcpb.PermissionlessValidatorSpec) (*rpcpb.AddPermissionlessValidatorResponse, error) {
	req := &rpcpb.AddPermissionlessValidatorRequest{
		ValidatorSpec: validatorSpec,
	}

	c.logger.Info("add permissionless validators to elastic chains")
	return c.controlc.AddPermissionlessValidator(ctx, req)
}

func (c *client) RemoveChainValidator(ctx context.Context, validatorSpec []*rpcpb.RemoveChainValidatorSpec) (*rpcpb.RemoveChainValidatorResponse, error) {
	req := &rpcpb.RemoveChainValidatorRequest{
		ValidatorSpec: validatorSpec,
	}

	c.logger.Info("remove chain validator")
	return c.controlc.RemoveChainValidator(ctx, req)
}

func (c *client) Health(ctx context.Context) (*rpcpb.HealthResponse, error) {
	c.logger.Info("health")
	return c.controlc.Health(ctx, &rpcpb.HealthRequest{})
}

func (c *client) WaitForHealthy(ctx context.Context) (*rpcpb.WaitForHealthyResponse, error) {
	c.logger.Info("wait for healthy")
	return c.controlc.WaitForHealthy(ctx, &rpcpb.WaitForHealthyRequest{})
}

func (c *client) URIs(ctx context.Context) ([]string, error) {
	c.logger.Info("uris")
	resp, err := c.controlc.URIs(ctx, &rpcpb.URIsRequest{})
	if err != nil {
		return nil, err
	}
	return resp.Uris, nil
}

func (c *client) Status(ctx context.Context) (*rpcpb.StatusResponse, error) {
	c.logger.Info("status")
	return c.controlc.Status(ctx, &rpcpb.StatusRequest{})
}

func (c *client) StreamStatus(ctx context.Context, pushInterval time.Duration) (<-chan *rpcpb.ClusterInfo, error) {
	stream, err := c.controlc.StreamStatus(ctx, &rpcpb.StreamStatusRequest{
		PushInterval: int64(pushInterval),
	})
	if err != nil {
		return nil, err
	}

	ch := make(chan *rpcpb.ClusterInfo, 1)
	go func() {
		defer func() {
			c.logger.Debug("closing stream send", log.Err(stream.CloseSend()))
			close(ch)
		}()
		c.logger.Info("start receive routine")
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.closed:
				return
			default:
			}

			// receive data from stream
			msg := new(rpcpb.StatusResponse)
			err := stream.RecvMsg(msg)
			if err == nil {
				ch <- msg.GetClusterInfo()
				continue
			}

			if errors.Is(err, io.EOF) {
				c.logger.Debug("received EOF from client; returning to close the stream from server side")
				return
			}
			if isClientCanceled(stream.Context().Err(), err) {
				c.logger.Warn("failed to receive status request from gRPC stream due to client cancellation", log.Err(err))
			} else {
				c.logger.Warn("failed to receive status request from gRPC stream", log.Err(err))
			}
			return
		}
	}()
	return ch, nil
}

func (c *client) Stop(ctx context.Context) (*rpcpb.StopResponse, error) {
	c.logger.Info("stop")
	return c.controlc.Stop(ctx, &rpcpb.StopRequest{})
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
	return c.controlc.AddNode(ctx, req)
}

func (c *client) RemoveNode(ctx context.Context, name string) (*rpcpb.RemoveNodeResponse, error) {
	c.logger.Info("remove node", log.String("name", name))
	return c.controlc.RemoveNode(ctx, &rpcpb.RemoveNodeRequest{Name: name})
}

func (c *client) PauseNode(ctx context.Context, name string) (*rpcpb.PauseNodeResponse, error) {
	c.logger.Info("pause node", log.String("name", name))
	return c.controlc.PauseNode(ctx, &rpcpb.PauseNodeRequest{Name: name})
}

func (c *client) ResumeNode(ctx context.Context, name string) (*rpcpb.ResumeNodeResponse, error) {
	c.logger.Info("resume node", log.String("name", name))
	return c.controlc.ResumeNode(ctx, &rpcpb.ResumeNodeRequest{Name: name})
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
	return c.controlc.RestartNode(ctx, req)
}

func (c *client) AttachPeer(ctx context.Context, nodeName string) (*rpcpb.AttachPeerResponse, error) {
	c.logger.Info("attaching peer", log.String("name", nodeName))
	return c.controlc.AttachPeer(ctx, &rpcpb.AttachPeerRequest{NodeName: nodeName})
}

func (c *client) SendOutboundMessage(ctx context.Context, nodeName string, peerID string, op uint32, msgBody []byte) (*rpcpb.SendOutboundMessageResponse, error) {
	c.logger.Info("sending outbound message", log.String("name", nodeName), log.String("peer-ID", peerID))
	return c.controlc.SendOutboundMessage(ctx, &rpcpb.SendOutboundMessageRequest{
		NodeName: nodeName,
		PeerId:   peerID,
		Op:       op,
		Bytes:    msgBody,
	})
}

func (c *client) SaveSnapshot(ctx context.Context, snapshotName string) (*rpcpb.SaveSnapshotResponse, error) {
	c.logger.Info("save snapshot", log.String("snapshot-name", snapshotName))
	return c.controlc.SaveSnapshot(ctx, &rpcpb.SaveSnapshotRequest{SnapshotName: snapshotName})
}

func (c *client) SaveHotSnapshot(ctx context.Context, snapshotName string) (*rpcpb.SaveSnapshotResponse, error) {
	c.logger.Info("save hot snapshot", log.String("snapshot-name", snapshotName))
	return c.controlc.SaveHotSnapshot(ctx, &rpcpb.SaveSnapshotRequest{SnapshotName: snapshotName})
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
	return c.controlc.LoadSnapshot(ctx, &req)
}

func (c *client) RemoveSnapshot(ctx context.Context, snapshotName string) (*rpcpb.RemoveSnapshotResponse, error) {
	c.logger.Info("remove snapshot", log.String("snapshot-name", snapshotName))
	return c.controlc.RemoveSnapshot(ctx, &rpcpb.RemoveSnapshotRequest{SnapshotName: snapshotName})
}

func (c *client) GetSnapshotNames(ctx context.Context) ([]string, error) {
	c.logger.Info("get snapshot names")
	resp, err := c.controlc.GetSnapshotNames(ctx, &rpcpb.GetSnapshotNamesRequest{})
	if err != nil {
		return nil, err
	}
	return resp.SnapshotNames, nil
}

func (c *client) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
	})
	return c.conn.Close()
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

func isClientCanceled(ctxErr error, err error) bool {
	if ctxErr != nil {
		return true
	}

	ev, ok := status.FromError(err)
	if !ok {
		return false
	}

	switch ev.Code() {
	case codes.Canceled, codes.DeadlineExceeded:
		// client-side context cancel or deadline exceeded
		// "rpc error: code = Canceled desc = context canceled"
		// "rpc error: code = DeadlineExceeded desc = context deadline exceeded"
		return true
	case codes.Unavailable:
		msg := ev.Message()
		// client-side context cancel or deadline exceeded with TLS ("http2.errClientDisconnected")
		// "rpc error: code = Unavailable desc = client disconnected"
		if msg == "client disconnected" {
			return true
		}
		// "grpc/transport.ClientTransport.CloseStream" on canceled streams
		// "rpc error: code = Unavailable desc = stream error: stream ID 21; CANCEL")
		if strings.HasPrefix(msg, "stream error: ") && strings.HasSuffix(msg, "; CANCEL") {
			return true
		}
	}
	return false
}
