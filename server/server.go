// Copyright (C) 2021-2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

// Package server implements server.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/multierr"

	"maps"
	"slices"

	"github.com/luxfi/config"
	"github.com/luxfi/netrunner/network"
	"github.com/luxfi/netrunner/network/node"
	"github.com/luxfi/netrunner/rpcpb"
	"github.com/luxfi/netrunner/utils"
	"github.com/luxfi/netrunner/utils/constants"
	"github.com/luxfi/netrunner/zaprpc"
	"github.com/luxfi/p2p/message"
	"github.com/luxfi/p2p/peer"

	log "github.com/luxfi/log"
	"github.com/luxfi/math/set"
)

const (
	// RPCVersion should be bumped anytime changes are made which require
	// the RPC client to upgrade to latest RPC server to be compatible
	// Note: This should match the node's RPC protocol version for compatibility
	RPCVersion   uint32 = 42
	MinNodes     uint32 = 1
	DefaultNodes uint32 = 5

	stopTimeout         = 5 * time.Second
	defaultStartTimeout = 5 * time.Minute
	// waitForHealthyTimeout - 7 minutes for mainnet network bootstrap
	// Node's monitorBootstrap has a 5-minute timeout, so we need to exceed that
	// First startup takes longer as nodes need to establish consensus
	// Subsequent restarts are faster (<10s) but initial bootstrap needs time
	// P-Chain API may return 503 for up to 60s after health check passes
	waitForHealthyTimeout = 7 * time.Minute
	// chainDeployTimeout - time for chain deploy operations including node restarts
	// Chain creation, node restart, and P-chain sync can take 60-90s
	chainDeployTimeout = 120 * time.Second

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
	ErrStatusCanceled         = errors.New("status stream canceled")
	ErrNoChainSpec            = errors.New("no blockchain spec was provided")
	ErrNoChainID              = errors.New("chainID is missing")
	ErrNoElasticChainSpec     = errors.New("no elastic chain spec was provided")
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
	// Port is the TCP address the netrunner ZAP server listens on,
	// e.g. ":8546" or "127.0.0.1:8546".
	Port string
	// GwPort is reserved for the optional ZIP edge surface (Fiber v3 /
	// fasthttp). Empty means no HTTP edge — clients talk ZAP directly.
	// The previous grpc-gateway HTTP→gRPC bridge is gone with the gRPC
	// transport itself.
	GwPort              string
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

	cfg    Config
	logger log.Logger

	rootCtx    context.Context
	rootCancel context.CancelFunc
	closed     chan struct{}

	zapServer *zaprpc.Server

	// Multi-network support: map from network name to network instance
	networks     map[string]*localNetwork
	clusterInfos map[string]*rpcpb.ClusterInfo
	// Controls running nodes.
	// Invariant: If [networks] is non-nil, then [clusterInfos] is non-nil.

	asyncErrCh chan error
}

// IsServerError reports whether err's text matches serverError's text. Used
// by callers to recognise domain errors that round-tripped as plain strings
// across the ZAP envelope (we don't ship typed gRPC status codes anymore).
func IsServerError(err error, serverError error) bool {
	if err == nil || serverError == nil {
		return false
	}
	return strings.Contains(err.Error(), serverError.Error())
}

func New(cfg Config, logger log.Logger) (Server, error) {
	if cfg.Port == "" {
		return nil, ErrInvalidPort
	}

	s := &server{
		cfg:          cfg,
		logger:       logger,
		closed:       make(chan struct{}),
		mu:           new(sync.RWMutex),
		asyncErrCh:   make(chan error, 1),
		networks:     make(map[string]*localNetwork),
		clusterInfos: make(map[string]*rpcpb.ClusterInfo),
	}

	port, err := portFromAddr(cfg.Port)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPort, err)
	}
	s.zapServer = zaprpc.NewServer(zaprpc.ServerConfig{
		NodeID: "netrunner-server",
		Port:   port,
	}, bindZAP(s))

	return s, nil
}

// portFromAddr accepts ":8546" or "host:8546" and returns the int port —
// the ZAP server binds by port number, not by string address.
func portFromAddr(addr string) (int, error) {
	host := addr
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		host = addr[i+1:]
	}
	return strconv.Atoi(host)
}

// Run starts the ZAP server and blocks until rootCtx is canceled.
func (s *server) Run(rootCtx context.Context) (err error) {
	s.rootCtx, s.rootCancel = context.WithCancel(rootCtx)

	s.logger.Info("starting netrunner ZAP server", log.String("port", s.cfg.Port))
	if err := s.zapServer.Start(); err != nil {
		return fmt.Errorf("start ZAP server: %w", err)
	}

	<-s.rootCtx.Done()
	s.logger.Warn("root context is done")
	s.zapServer.Stop()
	s.logger.Warn("closed ZAP server")

	// Grab lock to ensure [s.networks] isn't being used.
	s.mu.Lock()
	defer s.mu.Unlock()
	for name := range s.networks {
		s.stopAndRemoveNetwork(name, nil)
		s.logger.Warn("network stopped", log.String("network", name))
	}

	s.rootCancel()
	return nil
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

	// Extract network name from request or use default
	networkName := "mainnet" // Default for backward compatibility
	// TODO: Use proper GetNetworkName() once proto is regenerated
	// For now, we'll determine network type from other parameters
	// In the future: networkName := req.GetNetworkName()

	// Extract node type from request
	nodeType := "luxd" // Default to luxd for backward compatibility
	// TODO: Use proper GetNodeType() once proto is regenerated
	// In the future: nodeType := req.GetNodeType()

	// If this specific network is already running, return error
	if s.networks[networkName] != nil {
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
		trackChains       = req.GetWhitelistedChains()
		rootDataDir       = req.GetRootDataDir()
		pid               = int32(os.Getpid())
		globalNodeConfig  = req.GetGlobalNodeConfig()
		customNodeConfigs = req.GetCustomNodeConfigs()
		err               error
	)

	// Determine base directory and network name for network-centric structure
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
		_ = getNetworkNameFromRootDir(rootDataDir)

		// Ensure the provided directory exists
		if err = os.MkdirAll(rootDataDir, os.ModePerm); err != nil {
			return nil, err
		}
	}

	if len(customNodeConfigs) > 0 {
		s.logger.Warn("custom node configs have been provided; ignoring the 'number-of-nodes' parameter and setting it to:", log.Int("number-of-nodes", len(customNodeConfigs)))
		numNodes = uint32(len(customNodeConfigs))
	}

	if s.clusterInfos == nil {
		s.clusterInfos = make(map[string]*rpcpb.ClusterInfo)
	}
	s.clusterInfos[networkName] = &rpcpb.ClusterInfo{
		Pid:         pid,
		RootDataDir: rootDataDir,
	}

	if s.networks == nil {
		s.networks = make(map[string]*localNetwork)
	}
	var nw *localNetwork
	nw, err = newLocalNetwork(localNetworkOptions{
		execPath:            execPath,
		rootDataDir:         rootDataDir,
		numNodes:            numNodes,
		trackChains:         trackChains,
		redirectNodesOutput: s.cfg.RedirectNodesOutput,
		pluginDir:           pluginDir,
		globalNodeConfig:    globalNodeConfig,
		nodeType:            nodeType,
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
	s.networks[networkName] = nw

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
	if err := s.networks[networkName].Start(ctx); err != nil {
		s.logger.Warn("start failed to complete", log.Err(err), log.String("network", networkName))
		s.stopAndRemoveNetwork(networkName, nil)
		return nil, err
	}

	ctx, cancel = context.WithTimeout(context.Background(), waitForHealthyTimeout)
	defer cancel()
	chainIDs, err := s.networks[networkName].CreateChains(ctx, chainSpecs)
	if err != nil {
		s.logger.Error("network never became healthy", log.Err(err), log.String("network", networkName))
		s.stopAndRemoveNetwork(networkName, err)
		return nil, err
	}
	s.updateClusterInfo(networkName)
	s.logger.Info("network healthy", log.String("network", networkName))

	strChainIDs := []string{}
	for _, chainID := range chainIDs {
		strChainIDs = append(strChainIDs, chainID.String())
	}

	clusterInfo, err := deepCopy(s.clusterInfos[networkName])
	if err != nil {
		return nil, err
	}
	return &rpcpb.StartResponse{ClusterInfo: clusterInfo, ChainIds: strChainIDs}, nil
}

// Asssumes [s.mu] is held.
func (s *server) updateClusterInfo(networkName string) {
	if s.networks[networkName] == nil {
		// stop may have been called
		return
	}
	clusterInfo := s.clusterInfos[networkName]
	clusterInfo.Healthy = true
	clusterInfo.NodeNames = slices.Collect(maps.Keys(s.networks[networkName].nodeInfos))
	sort.Strings(clusterInfo.NodeNames)
	clusterInfo.NodeInfos = s.networks[networkName].nodeInfos
	clusterInfo.CustomChainsHealthy = true
	clusterInfo.CustomChains = make(map[string]*rpcpb.CustomChainInfo)
	for chainID, chainInfo := range s.networks[networkName].customChainIDToInfo {
		clusterInfo.CustomChains[chainID.String()] = chainInfo.info
	}
	clusterInfo.Chains = s.networks[networkName].chains
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
		if len(s.clusterInfos) == 0 {
			defer s.mu.RUnlock()
			return nil, ErrNotBootstrapped
		}

		// Return status for the first running network found (since we expect 1 per process)
		for _, clusterInfo := range s.clusterInfos {
			if clusterInfo.CustomChainsHealthy {
				defer s.mu.RUnlock()
				copiedInfo, err := deepCopy(clusterInfo)
				if err != nil {
					return nil, err
				}
				return &rpcpb.WaitForHealthyResponse{ClusterInfo: copiedInfo}, nil
			}
		}

		select {
		case err := <-s.asyncErrCh:
			defer s.mu.RUnlock()
			// Try to get info from any existing cluster info
			var clusterInfo *rpcpb.ClusterInfo
			for _, info := range s.clusterInfos {
				clusterInfo = info
				break
			}
			if clusterInfo != nil {
				copiedInfo, deepCopyErr := deepCopy(clusterInfo)
				if deepCopyErr != nil {
					err = multierr.Append(err, deepCopyErr)
					return nil, err
				}
				return &rpcpb.WaitForHealthyResponse{ClusterInfo: copiedInfo}, err
			}
			return nil, err
		case <-ctx.Done():
			defer s.mu.RUnlock()
			var clusterInfo *rpcpb.ClusterInfo
			for _, info := range s.clusterInfos {
				clusterInfo = info
				break
			}
			if clusterInfo != nil {
				copiedInfo, err := deepCopy(clusterInfo)
				if err != nil {
					return nil, err
				}
				return &rpcpb.WaitForHealthyResponse{ClusterInfo: copiedInfo}, ctx.Err()
			}
			return nil, ctx.Err()
		default:
		}

		if len(s.networks) == 0 {
			defer s.mu.RUnlock()
			return nil, ErrNotBootstrapped
		}
		s.mu.RUnlock()
		time.Sleep(100 * time.Millisecond) // Reduced from 1s for faster health polling
	}
}

func (s *server) CreateBlockchains(
	_ context.Context,
	req *rpcpb.CreateBlockchainsRequest,
) (*rpcpb.CreateBlockchainsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.networks) == 0 {
		s.logger.Error("CreateBlockchains: network not bootstrapped")
		return nil, ErrNotBootstrapped
	}

	// Use the first available network
	var networkName string
	var nw *localNetwork
	for name, net := range s.networks {
		networkName = name
		nw = net
		break
	}

	// Log the incoming request for debugging
	s.logger.Info("CreateBlockchains: received request",
		log.Int("numBlockchainSpecs", len(req.GetBlockchainSpecs())),
		log.String("network", networkName),
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
		chainSpec, err := getNetworkChainSpec(s.logger, spec, false, nw.pluginDir)
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
	chainIDsList := slices.Collect(maps.Keys(s.clusterInfos[networkName].Chains))
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

	s.clusterInfos[networkName].Healthy = false
	s.clusterInfos[networkName].CustomChainsHealthy = false

	s.logger.Info("CreateBlockchains: starting chain creation",
		log.Int("numChains", len(chainSpecs)),
		log.Duration("timeout", chainDeployTimeout),
	)

	// FAIL FAST: Use shorter timeout for chain deploy operations
	// 30s is plenty for local P-chain operations; if it takes longer, something is wrong
	ctx, cancel := context.WithTimeout(context.Background(), chainDeployTimeout)
	defer cancel()
	chainIDs, err := nw.CreateChains(ctx, chainSpecs)
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
			log.String("pluginDir", nw.pluginDir),
		)
		// Also print to stdout for immediate visibility
		fmt.Printf("ERROR: CreateBlockchains failed: %v\n", err)
		fmt.Printf("ERROR: VMs attempted: %v\n", vmNames)
		fmt.Printf("ERROR: Plugin directory: %s\n", nw.pluginDir)
		// Reset health flags on failure so subsequent deployments can proceed.
		// The network itself is still healthy, just this chain creation failed.
		s.updateClusterInfo(networkName)
		// Don't stop the entire network on chain creation failure - keep it running
		// so user can retry or investigate. This makes the network more resilient.
		return nil, fmt.Errorf("CreateBlockchains failed for VMs %v: %w", vmNames, err)
	}
	s.updateClusterInfo(networkName)
	s.logger.Info("CreateBlockchains: custom chains created successfully",
		log.Int("numChains", len(chainIDs)),
	)

	strChainIDs := []string{}
	for _, chainID := range chainIDs {
		strChainIDs = append(strChainIDs, chainID.String())
	}

	clusterInfo, err := deepCopy(s.clusterInfos[networkName])
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

	if s.networks["mainnet"] == nil {
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
	chainsSet.Add(slices.Collect(maps.Keys(s.clusterInfos["mainnet"].Chains))...)

	for _, validatorSpec := range validatorSpecList {
		if validatorSpec.ChainID == "" {
			return nil, ErrNoChainID
		} else if !chainsSet.Contains(validatorSpec.ChainID) {
			return nil, fmt.Errorf("chain id %q does not exist", validatorSpec.ChainID)
		}
	}

	s.clusterInfos["mainnet"].Healthy = false
	s.clusterInfos["mainnet"].CustomChainsHealthy = false

	ctx, cancel := context.WithTimeout(context.Background(), waitForHealthyTimeout)
	defer cancel()
	err := s.networks["mainnet"].AddPermissionlessValidators(ctx, validatorSpecList)

	s.updateClusterInfo("mainnet")

	if err != nil {
		s.logger.Error("failed to add permissionless validator", log.Err(err))
		return nil, err
	}

	s.logger.Info("successfully added permissionless validator")

	clusterInfo, err := deepCopy(s.clusterInfos["mainnet"])
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

	if s.networks["mainnet"] == nil {
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
	chainsSet.Add(slices.Collect(maps.Keys(s.clusterInfos["mainnet"].Chains))...)

	for _, validatorSpec := range validatorSpecList {
		if validatorSpec.ChainID == "" {
			return nil, ErrNoChainID
		} else if !chainsSet.Contains(validatorSpec.ChainID) {
			return nil, fmt.Errorf("chain id %q does not exist", validatorSpec.ChainID)
		}
	}

	s.clusterInfos["mainnet"].Healthy = false
	s.clusterInfos["mainnet"].CustomChainsHealthy = false

	ctx, cancel := context.WithTimeout(context.Background(), waitForHealthyTimeout)
	defer cancel()
	err := s.networks["mainnet"].RemoveChainValidator(ctx, validatorSpecList)

	s.updateClusterInfo("mainnet")

	if err != nil {
		s.logger.Error("failed to remove chain validator", log.Err(err))
		return nil, err
	}

	s.logger.Info("successfully removed chain validator")

	clusterInfo, err := deepCopy(s.clusterInfos["mainnet"])
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

	if s.networks["mainnet"] == nil {
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
	chainsSet.Add(slices.Collect(maps.Keys(s.clusterInfos["mainnet"].Chains))...)

	for _, elasticParticipantsSpec := range elasticParticipantsSpecList {
		if elasticParticipantsSpec.ChainID == nil {
			return nil, ErrNoChainID
		} else if !chainsSet.Contains(*elasticParticipantsSpec.ChainID) {
			return nil, fmt.Errorf("chain id %q does not exist", *elasticParticipantsSpec.ChainID)
		}
	}

	s.clusterInfos["mainnet"].Healthy = false
	s.clusterInfos["mainnet"].CustomChainsHealthy = false

	ctx, cancel := context.WithTimeout(context.Background(), waitForHealthyTimeout)
	defer cancel()
	txIDs, assetIDs, err := s.networks["mainnet"].TransformChains(ctx, elasticParticipantsSpecList)

	s.updateClusterInfo("mainnet")

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

	clusterInfo, err := deepCopy(s.clusterInfos["mainnet"])
	if err != nil {
		return nil, err
	}
	return &rpcpb.TransformElasticChainsResponse{ClusterInfo: clusterInfo, TxIds: strTXIDs, AssetIds: strAssetIDs}, nil
}

func (s *server) CreateChains(_ context.Context, req *rpcpb.CreateChainsRequest) (*rpcpb.CreateChainsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.networks["mainnet"] == nil {
		return nil, ErrNotBootstrapped
	}

	s.logger.Debug("CreateParticipantGroups", log.Uint32("num-groups", uint32(len(req.GetChainSpecs()))))

	participantsSpecs := []network.ParticipantsSpec{}
	for _, spec := range req.GetChainSpecs() {
		participantsSpec := getNetworkParticipantsSpec(spec)
		participantsSpecs = append(participantsSpecs, participantsSpec)
	}

	s.logger.Info("waiting for local cluster readiness")

	s.clusterInfos["mainnet"].Healthy = false
	s.clusterInfos["mainnet"].CustomChainsHealthy = false

	ctx, cancel := context.WithTimeout(context.Background(), waitForHealthyTimeout)
	defer cancel()
	chainIDs, err := s.networks["mainnet"].CreateParticipantGroups(ctx, participantsSpecs)
	if err != nil {
		s.logger.Error("failed to create chains", log.Err(err))
		// Don't stop the entire network on chain creation failure - keep it running
		// so user can retry or investigate. This makes the network more resilient.
		// s.stopAndRemoveNetwork(err)  // Commented out for resilience
		return nil, err
	} else {
		s.updateClusterInfo("mainnet")
	}
	s.logger.Info("chains created")

	strChainIDs := []string{}
	for _, chainID := range chainIDs {
		strChainIDs = append(strChainIDs, chainID.String())
	}

	clusterInfo, err := deepCopy(s.clusterInfos["mainnet"])
	if err != nil {
		return nil, err
	}
	return &rpcpb.CreateChainsResponse{ClusterInfo: clusterInfo, ChainIds: strChainIDs}, nil
}

func (s *server) Health(ctx context.Context, _ *rpcpb.HealthRequest) (*rpcpb.HealthResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Debug("Health")

	if len(s.networks) == 0 {
		return nil, ErrNotBootstrapped
	}

	// Just check the first available network for backward compatibility
	var networkName string
	var network *localNetwork
	for name, net := range s.networks {
		networkName = name
		network = net
		break
	}

	s.logger.Info("waiting for local cluster readiness", log.String("network", networkName))
	if err := network.AwaitHealthyAndUpdateNetworkInfo(ctx); err != nil {
		return nil, err
	}

	// Update cluster info for this network
	s.updateClusterInfo(networkName)

	clusterInfo, err := deepCopy(s.clusterInfos[networkName])
	if err != nil {
		return nil, err
	}
	return &rpcpb.HealthResponse{ClusterInfo: clusterInfo}, nil
}
func (s *server) URIs(context.Context, *rpcpb.URIsRequest) (*rpcpb.URIsResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	s.logger.Debug("URIs")

	if len(s.clusterInfos) == 0 {
		return nil, ErrNotBootstrapped
	}

	var clusterInfo *rpcpb.ClusterInfo
	for _, info := range s.clusterInfos {
		clusterInfo = info
		break
	}

	if clusterInfo == nil {
		return nil, ErrNotBootstrapped
	}

	uris := make([]string, 0, len(clusterInfo.NodeInfos))
	for _, nodeInfo := range clusterInfo.NodeInfos {
		uris = append(uris, nodeInfo.Uri)
	}
	sort.Strings(uris)

	return &rpcpb.URIsResponse{Uris: uris}, nil
}

func (s *server) Status(ctx context.Context, req *rpcpb.StatusRequest) (*rpcpb.StatusResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	s.logger.Debug("Status")

	if len(s.networks) == 0 {
		return &rpcpb.StatusResponse{}, ErrNotBootstrapped
	}

	// Just return the first available network's info
	var clusterInfo *rpcpb.ClusterInfo
	for _, info := range s.clusterInfos {
		clusterInfo = info
		break
	}

	if clusterInfo == nil {
		return &rpcpb.StatusResponse{}, ErrNotBootstrapped
	}

	return &rpcpb.StatusResponse{ClusterInfo: clusterInfo}, nil
}

// Assumes [s.mu] is held.
func (s *server) stopAndRemoveNetwork(networkName string, err error) {
	s.logger.Info("removing network", log.String("network", networkName))
	select {
	// cleanup of possible previous unchecked async err
	case err := <-s.asyncErrCh:
		s.logger.Debug(fmt.Sprintf("async err %s not returned to user", err))
	default:
	}
	if err != nil {
		s.asyncErrCh <- err
	}
	if s.networks[networkName] != nil {
		ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
		defer cancel()
		s.networks[networkName].Stop(ctx)
		delete(s.networks, networkName)
	}
	if s.clusterInfos[networkName] != nil {
		s.clusterInfos[networkName].Healthy = false
		s.clusterInfos[networkName].CustomChainsHealthy = false
		delete(s.clusterInfos, networkName)
	}
}

// StreamStatus over ZAP works as client-driven polling: client.StreamStatus
// in the netrunner client package opens a goroutine that calls Status() on
// the requested interval and emits ClusterInfo values into the returned
// channel. Server-side, the only thing we need to ensure is that the
// Status RPC returns fresh data — see (*server).Status.
//
// (The original gRPC StreamStatus used a server-streaming RPC. ZAP doesn't
// model streams natively; polling matches the same observable contract with
// strictly less plumbing and works over a single Call() round-trip per tick.)

func (s *server) AddNode(_ context.Context, req *rpcpb.AddNodeRequest) (*rpcpb.AddNodeResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Debug("AddNode", log.String("name", req.Name))

	if s.networks["mainnet"] == nil {
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

	if _, err := s.networks["mainnet"].nw.AddNode(nodeConfig); err != nil {
		return nil, err
	}

	if err := s.networks["mainnet"].UpdateNodeInfo(); err != nil {
		return nil, err
	}

	s.clusterInfos["mainnet"].NodeNames = slices.Collect(maps.Keys(s.networks["mainnet"].nodeInfos))
	sort.Strings(s.clusterInfos["mainnet"].NodeNames)
	s.clusterInfos["mainnet"].NodeInfos = s.networks["mainnet"].nodeInfos

	clusterInfo, err := deepCopy(s.clusterInfos["mainnet"])
	if err != nil {
		return nil, err
	}
	return &rpcpb.AddNodeResponse{ClusterInfo: clusterInfo}, nil
}

func (s *server) RemoveNode(ctx context.Context, req *rpcpb.RemoveNodeRequest) (*rpcpb.RemoveNodeResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Debug("RemoveNode", log.String("name", req.Name))

	if s.networks["mainnet"] == nil {
		return nil, ErrNotBootstrapped
	}

	if err := s.networks["mainnet"].nw.RemoveNode(ctx, req.Name); err != nil {
		return nil, err
	}

	if err := s.networks["mainnet"].UpdateNodeInfo(); err != nil {
		return nil, err
	}

	s.clusterInfos["mainnet"].NodeNames = slices.Collect(maps.Keys(s.networks["mainnet"].nodeInfos))
	sort.Strings(s.clusterInfos["mainnet"].NodeNames)
	s.clusterInfos["mainnet"].NodeInfos = s.networks["mainnet"].nodeInfos

	clusterInfo, err := deepCopy(s.clusterInfos["mainnet"])
	if err != nil {
		return nil, err
	}
	return &rpcpb.RemoveNodeResponse{ClusterInfo: clusterInfo}, nil
}

func (s *server) RestartNode(ctx context.Context, req *rpcpb.RestartNodeRequest) (*rpcpb.RestartNodeResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Debug("RestartNode", log.String("name", req.Name))

	if s.networks["mainnet"] == nil {
		return nil, ErrNotBootstrapped
	}

	if err := s.networks["mainnet"].nw.RestartNode(
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

	if err := s.networks["mainnet"].UpdateNodeInfo(); err != nil {
		return nil, err
	}

	s.clusterInfos["mainnet"].NodeNames = slices.Collect(maps.Keys(s.networks["mainnet"].nodeInfos))
	sort.Strings(s.clusterInfos["mainnet"].NodeNames)
	s.clusterInfos["mainnet"].NodeInfos = s.networks["mainnet"].nodeInfos

	clusterInfo, err := deepCopy(s.clusterInfos["mainnet"])
	if err != nil {
		return nil, err
	}
	return &rpcpb.RestartNodeResponse{ClusterInfo: clusterInfo}, nil
}

func (s *server) PauseNode(ctx context.Context, req *rpcpb.PauseNodeRequest) (*rpcpb.PauseNodeResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Debug("PauseNode", log.String("name", req.Name))

	if s.networks["mainnet"] == nil {
		return nil, ErrNotBootstrapped
	}

	if err := s.networks["mainnet"].nw.PauseNode(
		ctx,
		req.Name,
	); err != nil {
		return nil, err
	}

	if err := s.networks["mainnet"].UpdateNodeInfo(); err != nil {
		return nil, err
	}

	s.clusterInfos["mainnet"].NodeNames = slices.Collect(maps.Keys(s.networks["mainnet"].nodeInfos))
	sort.Strings(s.clusterInfos["mainnet"].NodeNames)
	s.clusterInfos["mainnet"].NodeInfos = s.networks["mainnet"].nodeInfos

	return &rpcpb.PauseNodeResponse{ClusterInfo: s.clusterInfos["mainnet"]}, nil
}

func (s *server) ResumeNode(ctx context.Context, req *rpcpb.ResumeNodeRequest) (*rpcpb.ResumeNodeResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Debug("ResumeNode", log.String("name", req.Name))

	if s.networks["mainnet"] == nil {
		return nil, ErrNotBootstrapped
	}

	if err := s.networks["mainnet"].nw.ResumeNode(
		ctx,
		req.Name,
	); err != nil {
		return nil, err
	}

	if err := s.networks["mainnet"].UpdateNodeInfo(); err != nil {
		return nil, err
	}

	s.clusterInfos["mainnet"].NodeNames = slices.Collect(maps.Keys(s.networks["mainnet"].nodeInfos))
	sort.Strings(s.clusterInfos["mainnet"].NodeNames)
	s.clusterInfos["mainnet"].NodeInfos = s.networks["mainnet"].nodeInfos

	return &rpcpb.ResumeNodeResponse{ClusterInfo: s.clusterInfos["mainnet"]}, nil
}

func (s *server) Stop(context.Context, *rpcpb.StopRequest) (*rpcpb.StopResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Debug("Stop")

	s.stopAndRemoveNetwork("mainnet", nil)

	return &rpcpb.StopResponse{ClusterInfo: s.clusterInfos["mainnet"]}, nil
}

var _ peer.InboundHandler = &loggingInboundHandler{}

type loggingInboundHandler struct {
	nodeName string
	logger   log.Logger
}

func (lh *loggingInboundHandler) HandleInbound(_ context.Context, msg message.InboundMessage) {
	lh.logger.Debug(
		"inbound handler received a message",
		log.String("message", msg.Op().String()),
		log.String("node-name", lh.nodeName),
	)
}

func (s *server) AttachPeer(ctx context.Context, req *rpcpb.AttachPeerRequest) (*rpcpb.AttachPeerResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Debug("AttachPeer")

	if s.networks["mainnet"] == nil {
		return nil, ErrNotBootstrapped
	}

	node, err := s.networks["mainnet"].nw.GetNode(req.NodeName)
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

	if s.clusterInfos["mainnet"].AttachedPeerInfos == nil {
		s.clusterInfos["mainnet"].AttachedPeerInfos = make(map[string]*rpcpb.ListOfAttachedPeerInfo)
	}
	peerInfo := &rpcpb.AttachedPeerInfo{Id: newPeerID}
	if v, ok := s.clusterInfos["mainnet"].AttachedPeerInfos[req.NodeName]; ok {
		v.Peers = append(v.Peers, peerInfo)
	} else {
		s.clusterInfos["mainnet"].AttachedPeerInfos[req.NodeName] = &rpcpb.ListOfAttachedPeerInfo{
			Peers: []*rpcpb.AttachedPeerInfo{peerInfo},
		}
	}

	clusterInfo, err := deepCopy(s.clusterInfos["mainnet"])
	if err != nil {
		return nil, err
	}
	return &rpcpb.AttachPeerResponse{ClusterInfo: clusterInfo, AttachedPeerInfo: peerInfo}, nil
}

func (s *server) SendOutboundMessage(ctx context.Context, req *rpcpb.SendOutboundMessageRequest) (*rpcpb.SendOutboundMessageResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Debug("SendOutboundMessage")

	if s.networks["mainnet"] == nil {
		return nil, ErrNotBootstrapped
	}

	node, err := s.networks["mainnet"].nw.GetNode(req.NodeName)
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

	// Get network name from request, with backward compatibility
	networkName := req.GetNetworkName()
	if networkName == "" {
		// For backward compatibility, try to determine from root dir if not specified
		rootDataDir := req.GetRootDataDir()
		if len(rootDataDir) == 0 {
			networkName = constants.DefaultNetwork
		} else {
			networkName = getNetworkNameFromRootDir(rootDataDir)
		}
	}

	if len(s.networks) > 0 {
		return nil, ErrAlreadyBootstrapped
	}

	var err error
	rootDataDir := req.GetRootDataDir()

	// If rootDataDir is still empty, set it up based on network name
	if len(rootDataDir) == 0 {
		// Default to ~/.lux for the base
		homeDir, _ := os.UserHomeDir()
		baseDir := filepath.Join(homeDir, ".lux")

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
		// Ensure the provided directory exists
		if err = os.MkdirAll(rootDataDir, os.ModePerm); err != nil {
			return nil, err
		}
	}

	pid := int32(os.Getpid())
	s.logger.Info("starting", log.Int32("pid", pid), log.String("network", networkName), log.String("root-data-dir", rootDataDir))

	if s.networks == nil {
		s.networks = make(map[string]*localNetwork)
	}
	var nw *localNetwork
	nw, err = newLocalNetwork(localNetworkOptions{
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
	s.networks[networkName] = nw

	if s.clusterInfos == nil {
		s.clusterInfos = make(map[string]*rpcpb.ClusterInfo)
	}
	s.clusterInfos[networkName] = &rpcpb.ClusterInfo{
		Pid:         pid,
		RootDataDir: rootDataDir,
	}

	// blocking load snapshot to soon get not found snapshot errors
	if err := s.networks[networkName].LoadSnapshot(req.SnapshotName); err != nil {
		s.logger.Warn("snapshot load failed to complete", log.Err(err))
		s.stopAndRemoveNetwork(networkName, nil)
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), waitForHealthyTimeout)
	defer cancel()
	err = s.networks[networkName].AwaitHealthyAndUpdateNetworkInfo(ctx)
	if err != nil {
		s.logger.Warn("snapshot load failed to complete. stopping network and cleaning up network", log.Err(err))
		s.stopAndRemoveNetwork(networkName, err)
		return nil, err
	}
	s.updateClusterInfo(networkName)
	s.logger.Info("network healthy")

	clusterInfo, err := deepCopy(s.clusterInfos[networkName])
	if err != nil {
		return nil, err
	}
	return &rpcpb.LoadSnapshotResponse{ClusterInfo: clusterInfo}, nil
}

func (s *server) SaveSnapshot(ctx context.Context, req *rpcpb.SaveSnapshotRequest) (*rpcpb.SaveSnapshotResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	networkName := req.GetNetworkName()
	if networkName == "" {
		networkName = "mainnet" // Default for backward compatibility
	}

	s.logger.Info("SaveSnapshot", log.String("network-name", networkName), log.String("snapshot-name", req.SnapshotName))

	if s.networks[networkName] == nil {
		return nil, ErrNotBootstrapped
	}

	snapshotPath, err := s.networks[networkName].nw.SaveSnapshot(ctx, req.SnapshotName)
	if err != nil {
		s.logger.Warn("snapshot save failed to complete", log.Err(err))
		return nil, err
	}

	s.stopAndRemoveNetwork(networkName, nil)

	return &rpcpb.SaveSnapshotResponse{SnapshotPath: snapshotPath}, nil
}

// SaveHotSnapshot saves a snapshot without stopping the network
// Uses Copy-on-Write on APFS (macOS) for instant snapshots
func (s *server) SaveHotSnapshot(ctx context.Context, req *rpcpb.SaveSnapshotRequest) (*rpcpb.SaveSnapshotResponse, error) {
	s.mu.RLock() // Read lock - doesn't block network operations
	defer s.mu.RUnlock()

	networkName := req.GetNetworkName()
	if networkName == "" {
		networkName = "mainnet" // Default for backward compatibility
	}

	s.logger.Info("SaveHotSnapshot", log.String("network-name", networkName), log.String("snapshot-name", req.SnapshotName))

	if s.networks[networkName] == nil {
		return nil, ErrNotBootstrapped
	}

	snapshotPath, err := s.networks[networkName].nw.SaveHotSnapshot(ctx, req.SnapshotName)
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

	networkName := req.GetNetworkName()
	if networkName == "" {
		networkName = "mainnet" // Default for backward compatibility
	}

	s.logger.Info("RemoveSnapshot", log.String("network-name", networkName), log.String("snapshot-name", req.SnapshotName))

	if s.networks[networkName] == nil {
		return nil, ErrNotBootstrapped
	}

	if err := s.networks[networkName].nw.RemoveSnapshot(req.SnapshotName); err != nil {
		s.logger.Warn("snapshot remove failed to complete", log.Err(err))
		return nil, err
	}
	return &rpcpb.RemoveSnapshotResponse{}, nil
}

func (s *server) GetSnapshotNames(ctx context.Context, req *rpcpb.GetSnapshotNamesRequest) (*rpcpb.GetSnapshotNamesResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	networkName := req.GetNetworkName()
	if networkName == "" {
		networkName = "mainnet" // Default for backward compatibility
	}

	s.logger.Info("GetSnapshotNames", log.String("network-name", networkName))

	if s.networks[networkName] == nil {
		return nil, ErrNotBootstrapped
	}

	snapshotNames, err := s.networks[networkName].nw.GetSnapshotNames()
	if err != nil {
		return nil, err
	}
	return &rpcpb.GetSnapshotNamesResponse{SnapshotNames: snapshotNames}, nil
}

func getNetworkElasticChainSpec(
	spec *rpcpb.ElasticChainSpec,
) network.ElasticChainSpec {
	minStakeDuration := time.Duration(spec.MinStakeDuration) * time.Hour
	maxStakeDuration := time.Duration(spec.MaxStakeDuration) * time.Hour

	elasticParticipantsSpec := network.ElasticChainSpec{
		ChainID:                  &spec.ChainId,
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
		ChainID:       spec.ChainId,
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
		ChainID:   spec.ChainId,
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
		BlockchainName: spec.BlockchainAlias,
	}

	if spec.ChainSpec != nil {
		participantsSpec := network.ParticipantsSpec{
			Participants: spec.ChainSpec.Participants,
			ChainConfig:  chainConfigBytes,
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
