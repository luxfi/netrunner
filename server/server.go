// Copyright (C) 2021-2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

// Package server implements server.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/multierr"

	"github.com/luxfi/netrunner/network"
	"github.com/luxfi/netrunner/network/node"
	"github.com/luxfi/netrunner/rpcpb"
	"github.com/luxfi/netrunner/utils"
	"github.com/luxfi/netrunner/utils/constants"
	"github.com/luxfi/node/config"
	"github.com/luxfi/node/message"
	"github.com/luxfi/consensus/core"
	"github.com/luxfi/node/network/peer"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"golang.org/x/exp/maps"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const (
	// RPCVersion should be bumped anytime changes are made which require
	// the RPC client to upgrade to latest RPC server to be compatible
	// Note: This should match the node's RPC protocol version for compatibility
	RPCVersion   uint32 = 42
	MinNodes     uint32 = 1
	DefaultNodes uint32 = 5

	stopTimeout           = 5 * time.Second
	defaultStartTimeout   = 5 * time.Minute
	// waitForHealthyTimeout increased from 3 to 10 minutes to allow proper
	// bootstrapping of staking validators in mainnet configuration
	waitForHealthyTimeout = 10 * time.Minute

	TimeParseLayout        = "2006-01-02 15:04:05"
	StakingMinimumLeadTime = 25 * time.Second
)

var (
	ErrInvalidVMName          = errors.New("invalid VM name")
	ErrInvalidPort            = errors.New("invalid port")
	ErrNotEnoughNodesForStart = errors.New("not enough nodes specified for start")
	ErrAlreadyBootstrapped    = errors.New("already bootstrapped")
	ErrNotBootstrapped        = errors.New("not bootstrapped")
	ErrNodeNotFound           = errors.New("node not found")
	ErrPeerNotFound           = errors.New("peer not found")
	ErrStatusCanceled         = errors.New("gRPC stream status canceled")
	ErrNoChainSpec       = errors.New("no blockchain spec was provided")
	ErrNoChainID             = errors.New("chainID is missing")
	ErrNoElasticChainSpec    = errors.New("no elastic chain spec was provided")
	ErrNoValidatorSpec        = errors.New("no validator spec was provided")
)

// ensureNetworkRunDir ensures a run directory exists for the specified network.
// Directory structure: <baseDir>/runs/<networkName>/run_<timestamp>/
// If an existing run with node data exists, it will be reused (for persistence).
// Otherwise, a new timestamped run directory is created.
// Returns the full path to the run directory (e.g., ~/.lux/runs/mainnet/run_20251222_102823/)
func ensureNetworkRunDir(baseDir, networkName string) (string, error) {
	// Create flat path: <baseDir>/runs/<networkName>/
	networkRunsDir := filepath.Join(baseDir, constants.RunsDir, networkName)

	// Ensure the network runs directory exists
	if err := os.MkdirAll(networkRunsDir, os.ModePerm); err != nil {
		return "", fmt.Errorf("failed to create network runs directory %s: %w", networkRunsDir, err)
	}

	// Look for existing run directories with node data
	entries, err := os.ReadDir(networkRunsDir)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}

	// Find the most recent run directory that has node data
	var latestRunDir string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, constants.RunDirPrefix+"_") {
			continue
		}
		// Check if this directory has node subdirectories
		runPath := filepath.Join(networkRunsDir, name)
		nodeEntries, _ := os.ReadDir(runPath)
		hasNodes := false
		for _, nodeEntry := range nodeEntries {
			if nodeEntry.IsDir() && strings.HasPrefix(nodeEntry.Name(), "node") {
				hasNodes = true
				break
			}
		}
		if hasNodes {
			// Timestamps sort lexicographically, so later entries are more recent
			if latestRunDir == "" || name > filepath.Base(latestRunDir) {
				latestRunDir = runPath
			}
		}
	}

	if latestRunDir != "" {
		// Reuse existing run directory for persistence
		return latestRunDir, nil
	}

	// No existing run with nodes, create new timestamped run directory
	return utils.MkDirWithTimestamp(filepath.Join(networkRunsDir, constants.RunDirPrefix))
}

// getNetworkNameFromRootDir extracts network name from root data dir path.
// It expects paths like ~/.lux/runs/mainnet/run_<timestamp> or ~/.lux/runs/mainnet.
// Falls back to constants.DefaultNetwork if no network name can be determined.
func getNetworkNameFromRootDir(rootDir string) string {
	// Check if path contains known network names
	base := filepath.Base(rootDir)
	switch base {
	case "mainnet", "testnet", "local", "devnet":
		return base
	}
	// Check parent for network type (for paths like ~/.lux/runs/mainnet/run_xxx)
	parent := filepath.Base(filepath.Dir(rootDir))
	switch parent {
	case "mainnet", "testnet", "local", "devnet":
		return parent
	}
	return constants.DefaultNetwork
}

type Config struct {
	Port   string
	GwPort string
	// true to disable grpc-gateway server
	GwDisabled          bool
	DialTimeout         time.Duration
	RedirectNodesOutput bool
	SnapshotsDir        string
	LogLevel            log.Level
}

type Server interface {
	Run(rootCtx context.Context) error
}

type server struct {
	mu *sync.RWMutex

	cfg Config
	logger log.Logger

	rootCtx    context.Context
	rootCancel context.CancelFunc
	closed     chan struct{}

	ln         net.Listener
	gRPCServer *grpc.Server

	gwMux    *runtime.ServeMux
	gwServer *http.Server

	clusterInfo *rpcpb.ClusterInfo
	// Controls running nodes.
	// Invariant: If [network] is non-nil, then [clusterInfo] is non-nil.

	network    *localNetwork
	asyncErrCh chan error

	rpcpb.UnimplementedPingServiceServer
	rpcpb.UnimplementedControlServiceServer
}

// grpc encapsulates the non protocol-related, ANR server domain errors,
// inside grpc.status.Status structs, with status.Code() code.Unknown,
// and original error msg inside status.Message() string
// this aux function is to be used by clients, to check for the appropriate
// ANR domain error kind
func IsServerError(err error, serverError error) bool {
	status := status.Convert(err)
	return status.Code() == codes.Unknown && status.Message() == serverError.Error()
}

func New(cfg Config, logger log.Logger) (Server, error) {
	if cfg.Port == "" || cfg.GwPort == "" {
		return nil, ErrInvalidPort
	}

	listener, err := net.Listen("tcp", cfg.Port)
	if err != nil {
		return nil, err
	}

	s := &server{
		cfg:        cfg,
		logger: logger,
		closed:     make(chan struct{}),
		ln:         listener,
		gRPCServer: grpc.NewServer(),
		mu:         new(sync.RWMutex),
		asyncErrCh: make(chan error, 1),
	}
	if !cfg.GwDisabled {
		s.gwMux = runtime.NewServeMux()
		s.gwServer = &http.Server{ //nolint // TODO add ReadHeaderTimeout
			Addr:    cfg.GwPort,
			Handler: s.gwMux,
		}
	}

	return s, nil
}

// Blocking call until server listeners return.
func (s *server) Run(rootCtx context.Context) (err error) {
	s.rootCtx, s.rootCancel = context.WithCancel(rootCtx)

	rpcpb.RegisterPingServiceServer(s.gRPCServer, s)
	rpcpb.RegisterControlServiceServer(s.gRPCServer, s)

	gRPCErrChan := make(chan error)
	go func() {
		s.logger.Info("serving gRPC server", log.String("port", s.cfg.Port))
		gRPCErrChan <- s.gRPCServer.Serve(s.ln)
	}()

	gwErrChan := make(chan error)
	if s.cfg.GwDisabled {
		s.logger.Info("gRPC gateway server is disabled")
	} else {
		// Set up gRPC gateway to allow for HTTP requests to [s.gRPCServer].
		go func() {
			s.logger.Info("dialing gRPC server for gRPC gateway", log.String("port", s.cfg.Port))
			ctx, cancel := context.WithTimeout(rootCtx, s.cfg.DialTimeout)
			gwConn, err := grpc.DialContext(
				ctx,
				"0.0.0.0"+s.cfg.Port,
				grpc.WithBlock(),
				grpc.WithTransportCredentials(insecure.NewCredentials()),
			)
			cancel()
			if err != nil {
				gwErrChan <- err
				return
			}
			defer gwConn.Close()

			if err := rpcpb.RegisterPingServiceHandler(rootCtx, s.gwMux, gwConn); err != nil {
				gwErrChan <- err
				return
			}
			if err := rpcpb.RegisterControlServiceHandler(rootCtx, s.gwMux, gwConn); err != nil {
				gwErrChan <- err
				return
			}

			s.logger.Info("serving gRPC gateway", log.String("port", s.cfg.GwPort))
			gwErrChan <- s.gwServer.ListenAndServe()
		}()
	}

	select {
	case <-rootCtx.Done():
		s.logger.Warn("root context is done")

		if !s.cfg.GwDisabled {
			s.logger.Warn("closed gRPC gateway server", log.Err(s.gwServer.Close()))
			<-gwErrChan
		}

		s.gRPCServer.Stop()
		s.logger.Warn("closed gRPC server")
		<-gRPCErrChan // Wait for [s.gRPCServer.Serve] to return.
		s.logger.Warn("gRPC terminated")

	case err = <-gRPCErrChan:
		s.logger.Warn("gRPC server failed", log.Err(err))

		// [s.grpcServer] is already stopped.
		if !s.cfg.GwDisabled {
			s.logger.Warn("closed gRPC gateway server", log.Err(s.gwServer.Close()))
			<-gwErrChan
		}

	case err = <-gwErrChan: // if disabled, this will never be selected
		// [s.gwServer] is already closed.
		s.logger.Warn("gRPC gateway server failed", log.Err(err))
		s.gRPCServer.Stop()
		s.logger.Warn("closed gRPC server")
		<-gRPCErrChan // Wait for [s.gRPCServer.Serve] to return.
	}

	// Grab lock to ensure [s.network] isn't being used.
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.network != nil {
		// Close the network.
		s.stopAndRemoveNetwork(nil)
		s.logger.Warn("network stopped")
	}

	s.rootCancel()
	return err
}

func (s *server) Ping(context.Context, *rpcpb.PingRequest) (*rpcpb.PingResponse, error) {
	s.logger.Debug("received ping request")
	return &rpcpb.PingResponse{Pid: int32(os.Getpid())}, nil
}

func (s *server) RPCVersion(context.Context, *rpcpb.RPCVersionRequest) (*rpcpb.RPCVersionResponse, error) {
	s.logger.Debug("RPCVersion")

	return &rpcpb.RPCVersionResponse{Version: RPCVersion}, nil
}

func (s *server) Start(_ context.Context, req *rpcpb.StartRequest) (*rpcpb.StartResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// If [network] is already populated, the network has already been started.
	if s.network != nil {
		return nil, ErrAlreadyBootstrapped
	}

	// Set default values for [req.NumNodes] if not given.
	if req.NumNodes == nil {
		n := DefaultNodes
		req.NumNodes = &n
	}
	if *req.NumNodes < MinNodes {
		return nil, ErrNotEnoughNodesForStart
	}

	if err := utils.CheckExecPath(req.GetExecPath()); err != nil {
		return nil, err
	}

	pluginDir := req.GetPluginDir()
	chainSpecs := []network.ChainSpec{}
	if len(req.GetBlockchainSpecs()) > 0 {
		s.logger.Info("plugin-dir:", log.String("plugin-dir", pluginDir))
		for _, spec := range req.GetBlockchainSpecs() {
			chainSpec, err := getNetworkChainSpec(s.logger, spec, true, pluginDir)
			if err != nil {
				return nil, err
			}
			chainSpecs = append(chainSpecs, chainSpec)
		}
	}

	var (
		execPath          = req.GetExecPath()
		numNodes          = req.GetNumNodes()
		trackChains      = req.GetWhitelistedChains()
		rootDataDir       = req.GetRootDataDir()
		pid               = int32(os.Getpid())
		globalNodeConfig  = req.GetGlobalNodeConfig()
		customNodeConfigs = req.GetCustomNodeConfigs()
		err               error
	)

	// Determine base directory and network name for network-centric structure
	var networkName string
	if len(rootDataDir) == 0 {
		// Default to ~/.lux for the base
		homeDir, _ := os.UserHomeDir()
		baseDir := filepath.Join(homeDir, ".lux")
		networkName = constants.DefaultNetwork

		// Ensure base directory exists
		if err = os.MkdirAll(baseDir, os.ModePerm); err != nil {
			return nil, err
		}

		// Create/reuse run directory: <baseDir>/runs/<networkName>/run_<timestamp>/
		rootDataDir, err = ensureNetworkRunDir(baseDir, networkName)
		if err != nil {
			return nil, err
		}
	} else {
		// CLI provided a specific rootDataDir - use it directly
		// Trust the CLI to provide a properly structured path
		networkName = getNetworkNameFromRootDir(rootDataDir)

		// Ensure the provided directory exists
		if err = os.MkdirAll(rootDataDir, os.ModePerm); err != nil {
			return nil, err
		}
	}

	if len(customNodeConfigs) > 0 {
		s.logger.Warn("custom node configs have been provided; ignoring the 'number-of-nodes' parameter and setting it to:", log.Int("number-of-nodes", len(customNodeConfigs)))
		numNodes = uint32(len(customNodeConfigs))
	}

	s.clusterInfo = &rpcpb.ClusterInfo{
		Pid:         pid,
		RootDataDir: rootDataDir,
	}

	s.network, err = newLocalNetwork(localNetworkOptions{
		execPath:            execPath,
		rootDataDir:         rootDataDir,
		numNodes:            numNodes,
		trackChains:        trackChains,
		redirectNodesOutput: s.cfg.RedirectNodesOutput,
		pluginDir:           pluginDir,
		globalNodeConfig:    globalNodeConfig,
		customNodeConfigs:   customNodeConfigs,
		chainConfigs:        req.ChainConfigs,
		upgradeConfigs:      req.UpgradeConfigs,
		pChainConfigs:       req.ChainConfigFiles,
		logLevel:            s.cfg.LogLevel,
		reassignPortsIfUsed: req.GetReassignPortsIfUsed(),
		dynamicPorts:        req.GetDynamicPorts(),
		snapshotsDir:        s.cfg.SnapshotsDir,
	})
	if err != nil {
		return nil, err
	}

	s.logger.Info("starting",
		log.String("exec-path", execPath),
		log.Uint32("num-nodes", numNodes),
		log.String("track-chains", trackChains),
		log.Int32("pid", pid),
		log.String("root-data-dir", rootDataDir),
		log.String("plugin-dir", pluginDir),
		log.Any("chain-configs", req.ChainConfigs),
		log.String("global-node-config", globalNodeConfig),
	)

	ctx, cancel := context.WithTimeout(context.Background(), waitForHealthyTimeout)
	defer cancel()
	if err := s.network.Start(ctx); err != nil {
		s.logger.Warn("start failed to complete", log.Err(err))
		s.stopAndRemoveNetwork(nil)
		return nil, err
	}

	ctx, cancel = context.WithTimeout(context.Background(), waitForHealthyTimeout)
	defer cancel()
	chainIDs, err := s.network.CreateChains(ctx, chainSpecs)
	if err != nil {
		s.logger.Error("network never became healthy", log.Err(err))
		s.stopAndRemoveNetwork(err)
		return nil, err
	}
	s.updateClusterInfo()
	s.logger.Info("network healthy")

	strChainIDs := []string{}
	for _, chainID := range chainIDs {
		strChainIDs = append(strChainIDs, chainID.String())
	}

	clusterInfo, err := deepCopy(s.clusterInfo)
	if err != nil {
		return nil, err
	}
	return &rpcpb.StartResponse{ClusterInfo: clusterInfo, ChainIds: strChainIDs}, nil
}

// Asssumes [s.mu] is held.
func (s *server) updateClusterInfo() {
	if s.network == nil {
		// stop may have been called
		return
	}
	s.clusterInfo.Healthy = true
	s.clusterInfo.NodeNames = maps.Keys(s.network.nodeInfos)
	sort.Strings(s.clusterInfo.NodeNames)
	s.clusterInfo.NodeInfos = s.network.nodeInfos
	s.clusterInfo.CustomChainsHealthy = true
	s.clusterInfo.CustomChains = make(map[string]*rpcpb.CustomChainInfo)
	for chainID, chainInfo := range s.network.customChainIDToInfo {
		s.clusterInfo.CustomChains[chainID.String()] = chainInfo.info
	}
	s.clusterInfo.Chains = s.network.chains
}

// wait until some of this conditions is met:
// - timeout expires
// - network operation terminates with error
// - network operation terminates successfully by setting CustomChainsHealthy
func (s *server) WaitForHealthy(ctx context.Context, _ *rpcpb.WaitForHealthyRequest) (*rpcpb.WaitForHealthyResponse, error) {
	s.logger.Debug("WaitForHealthy")

	ctx, cancel := context.WithTimeout(ctx, waitForHealthyTimeout)
	defer cancel()

	for {
		s.mu.RLock()
		if s.clusterInfo == nil {
			defer s.mu.RUnlock()
			return nil, ErrNotBootstrapped
		}
		if s.clusterInfo.CustomChainsHealthy {
			defer s.mu.RUnlock()
			clusterInfo, err := deepCopy(s.clusterInfo)
			if err != nil {
				return nil, err
			}
			return &rpcpb.WaitForHealthyResponse{ClusterInfo: clusterInfo}, nil
		}
		select {
		case err := <-s.asyncErrCh:
			defer s.mu.RUnlock()
			clusterInfo, deepCopyErr := deepCopy(s.clusterInfo)
			if deepCopyErr != nil {
				err = multierr.Append(err, deepCopyErr)
				return nil, err
			}
			return &rpcpb.WaitForHealthyResponse{ClusterInfo: clusterInfo}, err
		case <-ctx.Done():
			defer s.mu.RUnlock()
			clusterInfo, err := deepCopy(s.clusterInfo)
			if err != nil {
				return nil, err
			}
			return &rpcpb.WaitForHealthyResponse{ClusterInfo: clusterInfo}, ctx.Err()
		default:
		}
		if s.network == nil {
			defer s.mu.RUnlock()
			return nil, ErrNotBootstrapped
		}
		s.mu.RUnlock()
		time.Sleep(1 * time.Second)
	}
}

func (s *server) CreateBlockchains(
	_ context.Context,
	req *rpcpb.CreateBlockchainsRequest,
) (*rpcpb.CreateBlockchainsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.network == nil {
		s.logger.Error("CreateBlockchains: network not bootstrapped")
		return nil, ErrNotBootstrapped
	}

	// Log the incoming request for debugging
	s.logger.Info("CreateBlockchains: received request",
		log.Int("numBlockchainSpecs", len(req.GetBlockchainSpecs())),
	)

	if len(req.GetBlockchainSpecs()) == 0 {
		s.logger.Error("CreateBlockchains: no blockchain specs provided")
		return nil, ErrNoChainSpec
	}

	// Log details of each blockchain spec being processed
	chainSpecs := []network.ChainSpec{}
	for i, spec := range req.GetBlockchainSpecs() {
		s.logger.Info("CreateBlockchains: processing blockchain spec",
			log.Int("index", i),
			log.String("vmName", spec.GetVmName()),
			log.Bool("hasGenesis", spec.GetGenesis() != ""),
			log.Bool("hasChainId", spec.GetChainId() != ""),
		)
		chainSpec, err := getNetworkChainSpec(s.logger, spec, false, s.network.pluginDir)
		if err != nil {
			s.logger.Error("CreateBlockchains: failed to parse blockchain spec",
				log.Err(err),
				log.Int("specIndex", i),
				log.String("vmName", spec.GetVmName()),
			)
			return nil, fmt.Errorf("failed to parse blockchain spec %d (VM=%s): %w", i, spec.GetVmName(), err)
		}
		chainSpecs = append(chainSpecs, chainSpec)
	}

	// check that the given chains exist
	chainsSet := set.Set[string]{}
	chainIDsList := maps.Keys(s.clusterInfo.Chains)
	chainsSet.Add(chainIDsList...)

	for _, chainSpec := range chainSpecs {
		if chainSpec.ChainID != nil && !chainsSet.Contains(*chainSpec.ChainID) {
			s.logger.Error("CreateBlockchains: chain ID does not exist",
				log.String("chainID", *chainSpec.ChainID),
				log.String("vmName", chainSpec.VMName),
				log.Strings("existingChains", chainIDsList),
			)
			return nil, fmt.Errorf("chain id %q does not exist", *chainSpec.ChainID)
		}
	}

	s.clusterInfo.Healthy = false
	s.clusterInfo.CustomChainsHealthy = false

	s.logger.Info("CreateBlockchains: starting chain creation",
		log.Int("numChains", len(chainSpecs)),
		log.Duration("timeout", waitForHealthyTimeout),
	)

	ctx, cancel := context.WithTimeout(context.Background(), waitForHealthyTimeout)
	defer cancel()
	chainIDs, err := s.network.CreateChains(ctx, chainSpecs)
	if err != nil {
		// Build detailed error context for logging
		vmNames := make([]string, len(chainSpecs))
		for i, spec := range chainSpecs {
			vmNames[i] = spec.VMName
		}
		s.logger.Error("CreateBlockchains: failed to create blockchains",
			log.Err(err),
			log.String("errorDetail", fmt.Sprintf("%+v", err)),
			log.Strings("vmNames", vmNames),
			log.Int("numChainSpecs", len(chainSpecs)),
			log.String("pluginDir", s.network.pluginDir),
		)
		// Also print to stdout for immediate visibility
		fmt.Printf("ERROR: CreateBlockchains failed: %v\n", err)
		fmt.Printf("ERROR: VMs attempted: %v\n", vmNames)
		fmt.Printf("ERROR: Plugin directory: %s\n", s.network.pluginDir)
		// Reset health flags on failure so subsequent deployments can proceed.
		// The network itself is still healthy, just this chain creation failed.
		s.updateClusterInfo()
		// Don't stop the entire network on chain creation failure - keep it running
		// so user can retry or investigate. This makes the network more resilient.
		return nil, fmt.Errorf("CreateBlockchains failed for VMs %v: %w", vmNames, err)
	}
	s.updateClusterInfo()
	s.logger.Info("CreateBlockchains: custom chains created successfully",
		log.Int("numChains", len(chainIDs)),
	)

	strChainIDs := []string{}
	for _, chainID := range chainIDs {
		strChainIDs = append(strChainIDs, chainID.String())
	}

	clusterInfo, err := deepCopy(s.clusterInfo)
	if err != nil {
		return nil, err
	}
	return &rpcpb.CreateBlockchainsResponse{ClusterInfo: clusterInfo, ChainIds: strChainIDs}, nil
}

func (s *server) AddPermissionlessValidator(
	_ context.Context,
	req *rpcpb.AddPermissionlessValidatorRequest,
) (*rpcpb.AddPermissionlessValidatorResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.network == nil {
		return nil, ErrNotBootstrapped
	}

	s.logger.Debug("AddPermissionlessValidator")

	if len(req.GetValidatorSpec()) == 0 {
		return nil, ErrNoValidatorSpec
	}

	validatorSpecList := []network.PermissionlessValidatorSpec{}
	for _, spec := range req.GetValidatorSpec() {
		validatorSpec, err := getPermissionlessValidatorSpec(spec)
		if err != nil {
			return nil, err
		}
		validatorSpecList = append(validatorSpecList, validatorSpec)
	}

	// check that the given chains exist
	chainsSet := set.Set[string]{}
	chainsSet.Add(maps.Keys(s.clusterInfo.Chains)...)

	for _, validatorSpec := range validatorSpecList {
		if validatorSpec.ChainID == "" {
			return nil, ErrNoChainID
		} else if !chainsSet.Contains(validatorSpec.ChainID) {
			return nil, fmt.Errorf("chain id %q does not exist", validatorSpec.ChainID)
		}
	}

	s.clusterInfo.Healthy = false
	s.clusterInfo.CustomChainsHealthy = false

	ctx, cancel := context.WithTimeout(context.Background(), waitForHealthyTimeout)
	defer cancel()
	err := s.network.AddPermissionlessValidators(ctx, validatorSpecList)

	s.updateClusterInfo()

	if err != nil {
		s.logger.Error("failed to add permissionless validator", log.Err(err))
		return nil, err
	}

	s.logger.Info("successfully added permissionless validator")

	clusterInfo, err := deepCopy(s.clusterInfo)
	if err != nil {
		return nil, err
	}
	return &rpcpb.AddPermissionlessValidatorResponse{ClusterInfo: clusterInfo}, nil
}

func (s *server) RemoveChainValidator(
	_ context.Context,
	req *rpcpb.RemoveChainValidatorRequest,
) (*rpcpb.RemoveChainValidatorResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.network == nil {
		return nil, ErrNotBootstrapped
	}

	s.logger.Debug("RemoveChainValidator")

	if len(req.GetValidatorSpec()) == 0 {
		return nil, ErrNoValidatorSpec
	}

	validatorSpecList := []network.RemoveChainValidatorSpec{}
	for _, spec := range req.GetValidatorSpec() {
		validatorSpec := getRemoveChainValidatorSpec(spec)
		validatorSpecList = append(validatorSpecList, validatorSpec)
	}

	// check that the given chains exist
	chainsSet := set.Set[string]{}
	chainsSet.Add(maps.Keys(s.clusterInfo.Chains)...)

	for _, validatorSpec := range validatorSpecList {
		if validatorSpec.ChainID == "" {
			return nil, ErrNoChainID
		} else if !chainsSet.Contains(validatorSpec.ChainID) {
			return nil, fmt.Errorf("chain id %q does not exist", validatorSpec.ChainID)
		}
	}

	s.clusterInfo.Healthy = false
	s.clusterInfo.CustomChainsHealthy = false

	ctx, cancel := context.WithTimeout(context.Background(), waitForHealthyTimeout)
	defer cancel()
	err := s.network.RemoveChainValidator(ctx, validatorSpecList)

	s.updateClusterInfo()

	if err != nil {
		s.logger.Error("failed to remove chain validator", log.Err(err))
		return nil, err
	}

	s.logger.Info("successfully removed chain validator")

	clusterInfo, err := deepCopy(s.clusterInfo)
	if err != nil {
		return nil, err
	}
	return &rpcpb.RemoveChainValidatorResponse{ClusterInfo: clusterInfo}, nil
}

func (s *server) TransformElasticChains(
	_ context.Context,
	req *rpcpb.TransformElasticChainsRequest,
) (*rpcpb.TransformElasticChainsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.network == nil {
		return nil, ErrNotBootstrapped
	}

	s.logger.Debug("TransformElasticChain")

	if len(req.GetElasticChainSpec()) == 0 {
		return nil, ErrNoElasticChainSpec
	}

	elasticParticipantsSpecList := []network.ElasticChainSpec{}
	for _, spec := range req.GetElasticChainSpec() {
		elasticParticipantsSpec := getNetworkElasticChainSpec(spec)
		elasticParticipantsSpecList = append(elasticParticipantsSpecList, elasticParticipantsSpec)
	}

	// check that the given chains exist
	chainsSet := set.Set[string]{}
	chainsSet.Add(maps.Keys(s.clusterInfo.Chains)...)

	for _, elasticParticipantsSpec := range elasticParticipantsSpecList {
		if elasticParticipantsSpec.ChainID == nil {
			return nil, ErrNoChainID
		} else if !chainsSet.Contains(*elasticParticipantsSpec.ChainID) {
			return nil, fmt.Errorf("chain id %q does not exist", *elasticParticipantsSpec.ChainID)
		}
	}

	s.clusterInfo.Healthy = false
	s.clusterInfo.CustomChainsHealthy = false

	ctx, cancel := context.WithTimeout(context.Background(), waitForHealthyTimeout)
	defer cancel()
	txIDs, assetIDs, err := s.network.TransformChains(ctx, elasticParticipantsSpecList)

	s.updateClusterInfo()

	if err != nil {
		s.logger.Error("failed to transform chain into elastic chain", log.Err(err))
		return nil, err
	}

	s.logger.Info("chain transformed into elastic chain")

	strTXIDs := []string{}
	for _, txID := range txIDs {
		strTXIDs = append(strTXIDs, txID.String())
	}

	strAssetIDs := []string{}
	for _, assetID := range assetIDs {
		strAssetIDs = append(strAssetIDs, assetID.String())
	}

	clusterInfo, err := deepCopy(s.clusterInfo)
	if err != nil {
		return nil, err
	}
	return &rpcpb.TransformElasticChainsResponse{ClusterInfo: clusterInfo, TxIds: strTXIDs, AssetIds: strAssetIDs}, nil
}

func (s *server) CreateChains(_ context.Context, req *rpcpb.CreateChainsRequest) (*rpcpb.CreateChainsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.network == nil {
		return nil, ErrNotBootstrapped
	}

	s.logger.Debug("CreateParticipantGroups", log.Uint32("num-groups", uint32(len(req.GetChainSpecs()))))

	participantsSpecs := []network.ParticipantsSpec{}
	for _, spec := range req.GetChainSpecs() {
		participantsSpec := getNetworkParticipantsSpec(spec)
		participantsSpecs = append(participantsSpecs, participantsSpec)
	}

	s.logger.Info("waiting for local cluster readiness")

	s.clusterInfo.Healthy = false
	s.clusterInfo.CustomChainsHealthy = false

	ctx, cancel := context.WithTimeout(context.Background(), waitForHealthyTimeout)
	defer cancel()
	chainIDs, err := s.network.CreateParticipantGroups(ctx, participantsSpecs)
	if err != nil {
		s.logger.Error("failed to create chains", log.Err(err))
		// Don't stop the entire network on chain creation failure - keep it running
		// so user can retry or investigate. This makes the network more resilient.
		// s.stopAndRemoveNetwork(err)  // Commented out for resilience
		return nil, err
	} else {
		s.updateClusterInfo()
	}
	s.logger.Info("chains created")

	strChainIDs := []string{}
	for _, chainID := range chainIDs {
		strChainIDs = append(strChainIDs, chainID.String())
	}

	clusterInfo, err := deepCopy(s.clusterInfo)
	if err != nil {
		return nil, err
	}
	return &rpcpb.CreateChainsResponse{ClusterInfo: clusterInfo, ChainIds: strChainIDs}, nil
}

func (s *server) Health(ctx context.Context, _ *rpcpb.HealthRequest) (*rpcpb.HealthResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Debug("Health")

	if s.network == nil {
		return nil, ErrNotBootstrapped
	}

	s.logger.Info("waiting for local cluster readiness")
	if err := s.network.AwaitHealthyAndUpdateNetworkInfo(ctx); err != nil {
		return nil, err
	}

	s.clusterInfo.NodeNames = maps.Keys(s.network.nodeInfos)
	sort.Strings(s.clusterInfo.NodeNames)
	s.clusterInfo.NodeInfos = s.network.nodeInfos
	s.clusterInfo.Healthy = true

	clusterInfo, err := deepCopy(s.clusterInfo)
	if err != nil {
		return nil, err
	}
	return &rpcpb.HealthResponse{ClusterInfo: clusterInfo}, nil
}

func (s *server) URIs(context.Context, *rpcpb.URIsRequest) (*rpcpb.URIsResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	s.logger.Debug("URIs")

	if s.network == nil {
		return nil, ErrNotBootstrapped
	}

	uris := make([]string, 0, len(s.clusterInfo.NodeInfos))
	for _, nodeInfo := range s.clusterInfo.NodeInfos {
		uris = append(uris, nodeInfo.Uri)
	}
	sort.Strings(uris)

	return &rpcpb.URIsResponse{Uris: uris}, nil
}

func (s *server) Status(context.Context, *rpcpb.StatusRequest) (*rpcpb.StatusResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	s.logger.Debug("Status")

	if s.network == nil {
		return &rpcpb.StatusResponse{}, ErrNotBootstrapped
	}

	return &rpcpb.StatusResponse{ClusterInfo: s.clusterInfo}, nil
}

// Assumes [s.mu] is held.
func (s *server) stopAndRemoveNetwork(err error) {
	s.logger.Info("removing network")
	select {
	// cleanup of possible previous unchecked async err
	case err := <-s.asyncErrCh:
		s.logger.Debug(fmt.Sprintf("async err %s not returned to user", err))
	default:
	}
	if err != nil {
		s.asyncErrCh <- err
	}
	if s.network != nil {
		ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
		defer cancel()
		s.network.Stop(ctx)
	}
	if s.clusterInfo != nil {
		s.clusterInfo.Healthy = false
		s.clusterInfo.CustomChainsHealthy = false
	}
	s.network = nil
}

// TODO document this
func (s *server) StreamStatus(req *rpcpb.StreamStatusRequest, stream rpcpb.ControlService_StreamStatusServer) (err error) {
	s.logger.Debug("StreamStatus")

	interval := time.Duration(req.PushInterval)

	// returns this method, then server closes the stream
	s.logger.Info("pushing status updates to the stream", log.String("interval", interval.String()))
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		s.sendLoop(stream, interval)
		wg.Done()
	}()

	errCh := make(chan error, 1)
	go func() {
		err := s.recvLoop(stream)
		if err != nil {
			if isClientCanceled(stream.Context().Err(), err) {
				s.logger.Warn("failed to receive status request from gRPC stream due to client cancellation", log.Err(err))
			} else {
				s.logger.Warn("failed to receive status request from gRPC stream", log.Err(err))
			}
		}
		errCh <- err
	}()

	select {
	case err = <-errCh:
		if errors.Is(err, context.Canceled) {
			err = ErrStatusCanceled
		}
	case <-stream.Context().Done():
		err = stream.Context().Err()
		if errors.Is(err, context.Canceled) {
			err = ErrStatusCanceled
		}
	}

	wg.Wait()
	return err
}

// TODO document this
func (s *server) sendLoop(stream rpcpb.ControlService_StreamStatusServer, interval time.Duration) {
	s.logger.Info("start status send loop")

	tc := time.NewTicker(1)
	defer tc.Stop()

	for {
		select {
		case <-s.rootCtx.Done():
			return
		case <-tc.C:
			tc.Reset(interval)
		}

		s.logger.Debug("sending cluster info")

		s.mu.RLock()
		err := stream.Send(&rpcpb.StreamStatusResponse{ClusterInfo: s.clusterInfo})
		s.mu.RUnlock()
		if err != nil {
			if isClientCanceled(stream.Context().Err(), err) {
				s.logger.Debug("client stream canceled", log.Err(err))
				return
			}
			s.logger.Warn("failed to send an event", log.Err(err))
			return
		}
	}
}

// TODO document this
func (s *server) recvLoop(stream rpcpb.ControlService_StreamStatusServer) error {
	s.logger.Info("start status receive loop")

	for {
		select {
		case <-s.rootCtx.Done():
			return s.rootCtx.Err()
		default:
		}

		// receive data from stream
		req := new(rpcpb.StatusRequest)
		err := stream.RecvMsg(req)
		if errors.Is(err, io.EOF) {
			s.logger.Debug("received EOF from client; returning to close the stream from server side")
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (s *server) AddNode(_ context.Context, req *rpcpb.AddNodeRequest) (*rpcpb.AddNodeResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Debug("AddNode", log.String("name", req.Name))

	if s.network == nil {
		return nil, ErrNotBootstrapped
	}

	nodeFlags := map[string]interface{}{}
	if req.GetNodeConfig() != "" {
		if err := json.Unmarshal([]byte(req.GetNodeConfig()), &nodeFlags); err != nil {
			return nil, err
		}
	}

	if req.GetPluginDir() != "" {
		nodeFlags[config.PluginDirKey] = req.GetPluginDir()
	}

	nodeConfig := node.Config{
		Name:               req.Name,
		Flags:              nodeFlags,
		BinaryPath:         req.GetExecPath(),
		RedirectStdout:     s.cfg.RedirectNodesOutput,
		RedirectStderr:     s.cfg.RedirectNodesOutput,
		ChainConfigFiles:   req.ChainConfigs,
		UpgradeConfigFiles: req.UpgradeConfigs,
		PChainConfigFiles:  req.ChainConfigFiles,
	}

	if _, err := s.network.nw.AddNode(nodeConfig); err != nil {
		return nil, err
	}

	if err := s.network.UpdateNodeInfo(); err != nil {
		return nil, err
	}

	s.clusterInfo.NodeNames = maps.Keys(s.network.nodeInfos)
	sort.Strings(s.clusterInfo.NodeNames)
	s.clusterInfo.NodeInfos = s.network.nodeInfos

	clusterInfo, err := deepCopy(s.clusterInfo)
	if err != nil {
		return nil, err
	}
	return &rpcpb.AddNodeResponse{ClusterInfo: clusterInfo}, nil
}

func (s *server) RemoveNode(ctx context.Context, req *rpcpb.RemoveNodeRequest) (*rpcpb.RemoveNodeResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Debug("RemoveNode", log.String("name", req.Name))

	if s.network == nil {
		return nil, ErrNotBootstrapped
	}

	if err := s.network.nw.RemoveNode(ctx, req.Name); err != nil {
		return nil, err
	}

	if err := s.network.UpdateNodeInfo(); err != nil {
		return nil, err
	}

	s.clusterInfo.NodeNames = maps.Keys(s.network.nodeInfos)
	sort.Strings(s.clusterInfo.NodeNames)
	s.clusterInfo.NodeInfos = s.network.nodeInfos

	clusterInfo, err := deepCopy(s.clusterInfo)
	if err != nil {
		return nil, err
	}
	return &rpcpb.RemoveNodeResponse{ClusterInfo: clusterInfo}, nil
}

func (s *server) RestartNode(ctx context.Context, req *rpcpb.RestartNodeRequest) (*rpcpb.RestartNodeResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Debug("RestartNode", log.String("name", req.Name))

	if s.network == nil {
		return nil, ErrNotBootstrapped
	}

	if err := s.network.nw.RestartNode(
		ctx,
		req.Name,
		req.GetExecPath(),
		req.GetPluginDir(),
		req.GetWhitelistedChains(),
		req.GetChainConfigs(),
		req.GetUpgradeConfigs(),
		req.GetChainConfigFiles(),
	); err != nil {
		return nil, err
	}

	if err := s.network.UpdateNodeInfo(); err != nil {
		return nil, err
	}

	s.clusterInfo.NodeNames = maps.Keys(s.network.nodeInfos)
	sort.Strings(s.clusterInfo.NodeNames)
	s.clusterInfo.NodeInfos = s.network.nodeInfos

	clusterInfo, err := deepCopy(s.clusterInfo)
	if err != nil {
		return nil, err
	}
	return &rpcpb.RestartNodeResponse{ClusterInfo: clusterInfo}, nil
}

func (s *server) PauseNode(ctx context.Context, req *rpcpb.PauseNodeRequest) (*rpcpb.PauseNodeResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Debug("PauseNode", log.String("name", req.Name))

	if s.network == nil {
		return nil, ErrNotBootstrapped
	}

	if err := s.network.nw.PauseNode(
		ctx,
		req.Name,
	); err != nil {
		return nil, err
	}

	if err := s.network.UpdateNodeInfo(); err != nil {
		return nil, err
	}

	s.clusterInfo.NodeNames = maps.Keys(s.network.nodeInfos)
	sort.Strings(s.clusterInfo.NodeNames)
	s.clusterInfo.NodeInfos = s.network.nodeInfos

	return &rpcpb.PauseNodeResponse{ClusterInfo: s.clusterInfo}, nil
}

func (s *server) ResumeNode(ctx context.Context, req *rpcpb.ResumeNodeRequest) (*rpcpb.ResumeNodeResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Debug("ResumeNode", log.String("name", req.Name))

	if s.network == nil {
		return nil, ErrNotBootstrapped
	}

	if err := s.network.nw.ResumeNode(
		ctx,
		req.Name,
	); err != nil {
		return nil, err
	}

	if err := s.network.UpdateNodeInfo(); err != nil {
		return nil, err
	}

	s.clusterInfo.NodeNames = maps.Keys(s.network.nodeInfos)
	sort.Strings(s.clusterInfo.NodeNames)
	s.clusterInfo.NodeInfos = s.network.nodeInfos

	return &rpcpb.ResumeNodeResponse{ClusterInfo: s.clusterInfo}, nil
}

func (s *server) Stop(context.Context, *rpcpb.StopRequest) (*rpcpb.StopResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Debug("Stop")

	s.stopAndRemoveNetwork(nil)

	return &rpcpb.StopResponse{ClusterInfo: s.clusterInfo}, nil
}

var _ peer.InboundHandler = &loggingInboundHandler{}

type loggingInboundHandler struct {
	nodeName string
	logger log.Logger
}

func (lh *loggingInboundHandler) HandleInbound(_ context.Context, msg message.InboundMessage) {
	lh.logger.Debug(
		"inbound handler received a message",
		log.String("message", msg.Op().String()),
		log.String("node-name", lh.nodeName),
	)
}

func (lh *loggingInboundHandler) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, appRequestBytes []byte) error {
	lh.logger.Debug(
		"AppRequest received",
		log.String("node-name", lh.nodeName),
		log.Stringer("nodeID", nodeID),
		log.Uint32("requestID", requestID),
	)
	return nil
}

func (lh *loggingInboundHandler) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32, appErr *core.AppError) error {
	lh.logger.Debug(
		"AppRequestFailed received",
		log.String("node-name", lh.nodeName),
		log.Stringer("nodeID", nodeID),
		log.Uint32("requestID", requestID),
	)
	return nil
}

func (lh *loggingInboundHandler) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) error {
	lh.logger.Debug(
		"AppResponse received",
		log.String("node-name", lh.nodeName),
		log.Stringer("nodeID", nodeID),
		log.Uint32("requestID", requestID),
	)
	return nil
}

func (lh *loggingInboundHandler) AppGossip(ctx context.Context, nodeID ids.NodeID, appGossipBytes []byte) error {
	lh.logger.Debug(
		"AppGossip received",
		log.String("node-name", lh.nodeName),
		log.Stringer("nodeID", nodeID),
		log.Int("gossipSize", len(appGossipBytes)),
	)
	return nil
}

func (s *server) AttachPeer(ctx context.Context, req *rpcpb.AttachPeerRequest) (*rpcpb.AttachPeerResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Debug("AttachPeer")

	if s.network == nil {
		return nil, ErrNotBootstrapped
	}

	node, err := s.network.nw.GetNode(req.NodeName)
	if err != nil {
		return nil, err
	}

	loggingHandler := &loggingInboundHandler{nodeName: req.NodeName, logger: s.logger}
	newPeer, err := node.AttachPeer(ctx, loggingHandler)
	if err != nil {
		return nil, err
	}

	newPeerID := newPeer.ID().String()
	s.logger.Debug("new peer is attached to", log.String("peer-ID", newPeerID), log.String("node-name", node.GetName()))

	if s.clusterInfo.AttachedPeerInfos == nil {
		s.clusterInfo.AttachedPeerInfos = make(map[string]*rpcpb.ListOfAttachedPeerInfo)
	}
	peerInfo := &rpcpb.AttachedPeerInfo{Id: newPeerID}
	if v, ok := s.clusterInfo.AttachedPeerInfos[req.NodeName]; ok {
		v.Peers = append(v.Peers, peerInfo)
	} else {
		s.clusterInfo.AttachedPeerInfos[req.NodeName] = &rpcpb.ListOfAttachedPeerInfo{
			Peers: []*rpcpb.AttachedPeerInfo{peerInfo},
		}
	}

	clusterInfo, err := deepCopy(s.clusterInfo)
	if err != nil {
		return nil, err
	}
	return &rpcpb.AttachPeerResponse{ClusterInfo: clusterInfo, AttachedPeerInfo: peerInfo}, nil
}

func (s *server) SendOutboundMessage(ctx context.Context, req *rpcpb.SendOutboundMessageRequest) (*rpcpb.SendOutboundMessageResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Debug("SendOutboundMessage")

	if s.network == nil {
		return nil, ErrNotBootstrapped
	}

	node, err := s.network.nw.GetNode(req.NodeName)
	if err != nil {
		return nil, err
	}

	sent, err := node.SendOutboundMessage(ctx, req.PeerId, req.Bytes, req.Op)
	return &rpcpb.SendOutboundMessageResponse{Sent: sent}, err
}

func (s *server) LoadSnapshot(_ context.Context, req *rpcpb.LoadSnapshotRequest) (*rpcpb.LoadSnapshotResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Debug("LoadSnapshot")

	if s.network != nil {
		return nil, ErrAlreadyBootstrapped
	}

	var err error
	rootDataDir := req.GetRootDataDir()

	// Determine base directory and network name for network-centric structure
	var networkName string
	if len(rootDataDir) == 0 {
		// Default to ~/.lux for the base
		homeDir, _ := os.UserHomeDir()
		baseDir := filepath.Join(homeDir, ".lux")
		networkName = constants.DefaultNetwork

		// Ensure base directory exists
		if err = os.MkdirAll(baseDir, os.ModePerm); err != nil {
			return nil, err
		}

		// Create/reuse run directory: <baseDir>/runs/<networkName>/run_<timestamp>/
		rootDataDir, err = ensureNetworkRunDir(baseDir, networkName)
		if err != nil {
			return nil, err
		}
	} else {
		// CLI provided a specific rootDataDir - use it directly
		// Trust the CLI to provide a properly structured path
		networkName = getNetworkNameFromRootDir(rootDataDir)

		// Ensure the provided directory exists
		if err = os.MkdirAll(rootDataDir, os.ModePerm); err != nil {
			return nil, err
		}
	}

	pid := int32(os.Getpid())
	s.logger.Info("starting", log.Int32("pid", pid), log.String("network", networkName), log.String("root-data-dir", rootDataDir))

	s.network, err = newLocalNetwork(localNetworkOptions{
		execPath:            req.GetExecPath(),
		pluginDir:           req.GetPluginDir(),
		rootDataDir:         rootDataDir,
		chainConfigs:        req.ChainConfigs,
		upgradeConfigs:      req.UpgradeConfigs,
		pChainConfigs:       req.ChainConfigFiles,
		globalNodeConfig:    req.GetGlobalNodeConfig(),
		logLevel:            s.cfg.LogLevel,
		reassignPortsIfUsed: req.GetReassignPortsIfUsed(),
		snapshotsDir:        s.cfg.SnapshotsDir,
	})
	if err != nil {
		return nil, err
	}
	s.clusterInfo = &rpcpb.ClusterInfo{
		Pid:         pid,
		RootDataDir: rootDataDir,
	}

	// blocking load snapshot to soon get not found snapshot errors
	if err := s.network.LoadSnapshot(req.SnapshotName); err != nil {
		s.logger.Warn("snapshot load failed to complete", log.Err(err))
		s.stopAndRemoveNetwork(nil)
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), waitForHealthyTimeout)
	defer cancel()
	err = s.network.AwaitHealthyAndUpdateNetworkInfo(ctx)
	if err != nil {
		s.logger.Warn("snapshot load failed to complete. stopping network and cleaning up network", log.Err(err))
		s.stopAndRemoveNetwork(err)
		return nil, err
	}
	s.updateClusterInfo()
	s.logger.Info("network healthy")

	clusterInfo, err := deepCopy(s.clusterInfo)
	if err != nil {
		return nil, err
	}
	return &rpcpb.LoadSnapshotResponse{ClusterInfo: clusterInfo}, nil
}

func (s *server) SaveSnapshot(ctx context.Context, req *rpcpb.SaveSnapshotRequest) (*rpcpb.SaveSnapshotResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info("SaveSnapshot", log.String("snapshot-name", req.SnapshotName))

	if s.network == nil {
		return nil, ErrNotBootstrapped
	}

	snapshotPath, err := s.network.nw.SaveSnapshot(ctx, req.SnapshotName)
	if err != nil {
		s.logger.Warn("snapshot save failed to complete", log.Err(err))
		return nil, err
	}

	s.stopAndRemoveNetwork(nil)

	return &rpcpb.SaveSnapshotResponse{SnapshotPath: snapshotPath}, nil
}

// SaveHotSnapshot saves a snapshot without stopping the network
// Uses Copy-on-Write on APFS (macOS) for instant snapshots
func (s *server) SaveHotSnapshot(ctx context.Context, req *rpcpb.SaveSnapshotRequest) (*rpcpb.SaveSnapshotResponse, error) {
	s.mu.RLock() // Read lock - doesn't block network operations
	defer s.mu.RUnlock()

	s.logger.Info("SaveHotSnapshot", log.String("snapshot-name", req.SnapshotName))

	if s.network == nil {
		return nil, ErrNotBootstrapped
	}

	snapshotPath, err := s.network.nw.SaveHotSnapshot(ctx, req.SnapshotName)
	if err != nil {
		s.logger.Warn("hot snapshot save failed to complete", log.Err(err))
		return nil, err
	}

	// Note: We do NOT stop the network for hot snapshots
	s.logger.Info("Hot snapshot saved successfully",
		log.String("snapshot-name", req.SnapshotName),
		log.String("path", snapshotPath))

	return &rpcpb.SaveSnapshotResponse{SnapshotPath: snapshotPath}, nil
}

func (s *server) RemoveSnapshot(_ context.Context, req *rpcpb.RemoveSnapshotRequest) (*rpcpb.RemoveSnapshotResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info("RemoveSnapshot", log.String("snapshot-name", req.SnapshotName))

	if s.network == nil {
		return nil, ErrNotBootstrapped
	}

	if err := s.network.nw.RemoveSnapshot(req.SnapshotName); err != nil {
		s.logger.Warn("snapshot remove failed to complete", log.Err(err))
		return nil, err
	}
	return &rpcpb.RemoveSnapshotResponse{}, nil
}

func (s *server) GetSnapshotNames(context.Context, *rpcpb.GetSnapshotNamesRequest) (*rpcpb.GetSnapshotNamesResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	s.logger.Info("GetSnapshotNames")

	if s.network == nil {
		return nil, ErrNotBootstrapped
	}

	snapshotNames, err := s.network.nw.GetSnapshotNames()
	if err != nil {
		return nil, err
	}
	return &rpcpb.GetSnapshotNamesResponse{SnapshotNames: snapshotNames}, nil
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

func getNetworkElasticChainSpec(
	spec *rpcpb.ElasticChainSpec,
) network.ElasticChainSpec {
	minStakeDuration := time.Duration(spec.MinStakeDuration) * time.Hour
	maxStakeDuration := time.Duration(spec.MaxStakeDuration) * time.Hour

	elasticParticipantsSpec := network.ElasticChainSpec{
		ChainID:                 &spec.ChainId,
		AssetName:                spec.AssetName,
		AssetSymbol:              spec.AssetSymbol,
		InitialSupply:            spec.InitialSupply,
		MaxSupply:                spec.MaxSupply,
		MinConsumptionRate:       spec.MinConsumptionRate,
		MaxConsumptionRate:       spec.MaxConsumptionRate,
		MinValidatorStake:        spec.MinValidatorStake,
		MaxValidatorStake:        spec.MaxValidatorStake,
		MinStakeDuration:         minStakeDuration,
		MaxStakeDuration:         maxStakeDuration,
		MinDelegationFee:         spec.MinDelegationFee,
		MinDelegatorStake:        spec.MinDelegatorStake,
		MaxValidatorWeightFactor: byte(spec.MaxValidatorWeightFactor),
		UptimeRequirement:        spec.UptimeRequirement,
	}
	return elasticParticipantsSpec
}

func getPermissionlessValidatorSpec(
	spec *rpcpb.PermissionlessValidatorSpec,
) (network.PermissionlessValidatorSpec, error) {
	var startTime time.Time
	var err error
	if spec.StartTime != "" {
		startTime, err = time.Parse(TimeParseLayout, spec.StartTime)
		if err != nil {
			return network.PermissionlessValidatorSpec{}, err
		}
		if startTime.Before(time.Now().Add(StakingMinimumLeadTime)) {
			return network.PermissionlessValidatorSpec{}, fmt.Errorf("time should be at least %s in the future for validator spec of %s", StakingMinimumLeadTime, spec.NodeName)
		}
	}

	stakeDuration := time.Duration(spec.StakeDuration) * time.Hour

	validatorSpec := network.PermissionlessValidatorSpec{
		ChainID:      spec.ChainId,
		AssetID:       spec.AssetId,
		NodeName:      spec.NodeName,
		StakedAmount:  spec.StakedTokenAmount,
		StartTime:     startTime,
		StakeDuration: stakeDuration,
	}
	return validatorSpec, nil
}

func getRemoveChainValidatorSpec(
	spec *rpcpb.RemoveChainValidatorSpec,
) network.RemoveChainValidatorSpec {
	validatorSpec := network.RemoveChainValidatorSpec{
		ChainID:  spec.ChainId,
		NodeNames: spec.GetNodeNames(),
	}
	return validatorSpec
}

func getNetworkChainSpec(
	logger log.Logger,
	spec *rpcpb.BlockchainSpec,
	isNewEmptyNetwork bool,
	pluginDir string,
) (network.ChainSpec, error) {
	if isNewEmptyNetwork && spec.ChainId != nil {
		return network.ChainSpec{}, errors.New("blockchain chain id must be nil if starting a new empty network")
	}

	vmName := spec.VmName
	logger.Info("checking custom chain's VM ID before installation", log.String("id", vmName))
	vmID, err := utils.VMID(vmName)
	if err != nil {
		logger.Warn("failed to convert VM name to VM ID", log.String("vm-name", vmName), log.Err(err))
		return network.ChainSpec{}, ErrInvalidVMName
	}

	// there is no default plugindir from the ANR point of view, will not check if not given
	if pluginDir != "" {
		if err := utils.CheckPluginPath(
			filepath.Join(pluginDir, vmID.String()),
		); err != nil {
			return network.ChainSpec{}, err
		}
	}

	genesisBytes := readFileOrString(spec.Genesis)

	var chainConfigBytes []byte
	if spec.ChainConfig != "" {
		chainConfigBytes = readFileOrString(spec.ChainConfig)
	}

	var networkUpgradeBytes []byte
	if spec.NetworkUpgrade != "" {
		networkUpgradeBytes = readFileOrString(spec.NetworkUpgrade)
	}

	// Override chain config with ChainSpec.ChainConfigFile if provided
	if spec.ChainSpec != nil && spec.ChainSpec.ChainConfigFile != "" {
		chainConfigBytes = readFileOrString(spec.ChainSpec.ChainConfigFile)
	}

	perNodeChainConfig := map[string][]byte{}
	if spec.PerNodeChainConfig != "" {
		perNodeChainConfigBytes := readFileOrString(spec.PerNodeChainConfig)

		perNodeChainConfigMap := map[string]interface{}{}
		if err := json.Unmarshal(perNodeChainConfigBytes, &perNodeChainConfigMap); err != nil {
			return network.ChainSpec{}, err
		}

		for nodeName, cfg := range perNodeChainConfigMap {
			cfgBytes, err := json.Marshal(cfg)
			if err != nil {
				return network.ChainSpec{}, err
			}
			perNodeChainConfig[nodeName] = cfgBytes
		}
	}

	chainSpec := network.ChainSpec{
		VMName:             vmName,
		Genesis:            genesisBytes,
		ChainConfig:        chainConfigBytes,
		NetworkUpgrade:     networkUpgradeBytes,
		ChainID:            spec.ChainId,
		Alias:              spec.BlockchainAlias,
		PerNodeChainConfig: perNodeChainConfig,
		// Use BlockchainAlias as the blockchain name for the P-Chain transaction
		// This allows multiple chains to use the same VM (e.g., multiple EVM chains)
		BlockchainName:     spec.BlockchainAlias,
	}

	if spec.ChainSpec != nil {
		participantsSpec := network.ParticipantsSpec{
			Participants: spec.ChainSpec.Participants,
			ChainConfig: chainConfigBytes,
		}
		chainSpec.ParticipantsSpec = &participantsSpec
	}

	return chainSpec, nil
}

func getNetworkParticipantsSpec(
	spec *rpcpb.ChainSpec,
) network.ParticipantsSpec {
	var chainConfigBytes []byte
	if spec.ChainConfigFile != "" {
		chainConfigBytes = readFileOrString(spec.ChainConfigFile)
	}
	return network.ParticipantsSpec{
		Participants: spec.Participants,
		ChainConfig:  chainConfigBytes,
	}
}

// if [conf] is a readable file path, returns the file contents
// if not, returns [conf] as a byte slice
func readFileOrString(conf string) []byte {
	confBytes, err := os.ReadFile(conf)
	if err != nil {
		return []byte(conf)
	}
	return confBytes
}
