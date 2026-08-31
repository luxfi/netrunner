// Copyright (C) 2021-2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"maps"
	"slices"

	"github.com/luxfi/config"
	"github.com/luxfi/ids"
	"github.com/luxfi/netrunner/local"
	"github.com/luxfi/netrunner/network"
	"github.com/luxfi/netrunner/network/node"
	"github.com/luxfi/netrunner/rpcpb"
	netconstants "github.com/luxfi/netrunner/utils/constants"
	"github.com/luxfi/netrunner/ux"

	"github.com/luxfi/constants"
	log "github.com/luxfi/log"
)

const (
	prometheusConfFname  = "prometheus.yaml"
	prometheusConfCommon = `global:
  scrape_interval: 15s
  evaluation_interval: 15s
scrape_configs:
  - job_name: prometheus
    static_configs:
      - targets: 
        - localhost:9090
  - job_name: node-machine
    static_configs:
     - targets: 
       - localhost:9100
       labels:
         alias: machine
  - job_name: node
    metrics_path: /v1/metrics
    static_configs:
      - targets:
`
)

type localNetwork struct {
	lock sync.Mutex
	log  log.Logger

	execPath  string
	pluginDir string

	cfg network.Config

	nw network.Network

	nodeInfos map[string]*rpcpb.NodeInfo

	options localNetworkOptions

	// map from blockchain ID to blockchain info
	customChainIDToInfo map[ids.ID]chainInfo

	// Closed when [stop] is called.
	stopCh   chan struct{}
	stopOnce sync.Once

	chains map[string]*rpcpb.ChainInfo

	prometheusConfPath string
}

type chainInfo struct {
	info         *rpcpb.CustomChainInfo
	chainID      ids.ID
	blockchainID ids.ID
}

type localNetworkOptions struct {
	execPath            string
	rootDataDir         string
	numNodes            uint32
	trackChains         string
	redirectNodesOutput bool
	globalNodeConfig    string

	// Node consensus engine type
	// Determines which binary to use and how to configure it
	nodeType string // "luxd", "avalanchego", "geth", "op-node", etc.

	pluginDir         string
	customNodeConfigs map[string]string

	// chain configs to be added to the network, besides the ones in default config, or saved snapshot
	chainConfigs map[string]string
	// upgrade configs to be added to the network, besides the ones in default config, or saved snapshot
	upgradeConfigs map[string]string
	// P-chain configs to be added to the network, besides the ones in default config, or saved snapshot
	pChainConfigs map[string]string

	snapshotsDir string

	logLevel log.Level

	reassignPortsIfUsed bool

	dynamicPorts bool
}

func newLocalNetwork(opts localNetworkOptions) (*localNetwork, error) {
	logFactory := log.NewFactoryWithConfig(log.Config{
		RotatingWriterConfig: log.RotatingWriterConfig{
			Directory: opts.rootDataDir,
		},
		LogLevel:     opts.logLevel,
		DisplayLevel: opts.logLevel,
	})
	logger, err := logFactory.Make(netconstants.LogNameMain)
	if err != nil {
		return nil, err
	}

	return &localNetwork{
		log:                 logger,
		execPath:            opts.execPath,
		pluginDir:           opts.pluginDir,
		options:             opts,
		customChainIDToInfo: make(map[ids.ID]chainInfo),
		stopCh:              make(chan struct{}),
		nodeInfos:           make(map[string]*rpcpb.NodeInfo),
		chains:              make(map[string]*rpcpb.ChainInfo),
	}, nil
}

// TODO document.
// Assumes [lc.lock] is held.
func (lc *localNetwork) createConfig() error {
	lc.log.Info("createConfig() ENTERED", "globalNodeConfig", lc.options.globalNodeConfig)
	var globalConfig map[string]interface{}

	if lc.options.globalNodeConfig != "" {
		if err := json.Unmarshal([]byte(lc.options.globalNodeConfig), &globalConfig); err != nil {
			return err
		}
	}

	// Check if a specific network-id is requested in globalConfig
	// If so, use the appropriate genesis configuration
	var cfg network.Config
	var err error
	networkID := constants.LocalID // default to local (1337)
	if networkIDVal, ok := globalConfig["network-id"]; ok {
		switch v := networkIDVal.(type) {
		case float64:
			networkID = uint32(v)
		case int:
			networkID = uint32(v)
		case string:
			// Parse string representation
			var n int
			fmt.Sscanf(v, "%d", &n)
			networkID = uint32(n)
		}
	}

	lc.log.Info("createConfig networkID parsed", "networkID", networkID, "MainnetID", constants.MainnetID, "TestnetID", constants.TestnetID)
	fmt.Fprintf(os.Stderr, "DEBUG: rootDataDir=%s\n", lc.options.rootDataDir)

	// CRITICAL: Check if we're resuming from existing state.
	// If node1/genesis.json exists AND node1/db has data, we MUST use the existing genesis
	// AND the existing staking keys to avoid validator mismatch errors.
	existingGenesis, isResume := lc.checkForExistingGenesis()
	fmt.Fprintf(os.Stderr, "DEBUG: checkForExistingGenesis returned isResume=%v, genesisLen=%d\n", isResume, len(existingGenesis))
	if isResume {
		ux.Print(lc.log, log.Green.Wrap("📂 RESUME DETECTED: Loading existing genesis AND staking keys from %s"), lc.options.rootDataDir)
		fmt.Fprintf(os.Stderr, "📂 RESUME DETECTED: Loading existing genesis AND staking keys from %s\n", lc.options.rootDataDir)

		// CRITICAL: When resuming from a snapshot, we MUST use the existing staking keys
		// from the snapshot, not generate new ones from ~/.lux/keys/. The snapshot's genesis
		// contains NodeIDs that match the snapshot's staking keys. Using different keys
		// causes validator mismatch and nodes fail to start.
		cfg, err = local.NewConfigFromExistingSnapshot(
			lc.options.execPath,
			lc.options.rootDataDir,
			existingGenesis,
			lc.options.numNodes,
			9630, // default port base
		)
		if err != nil {
			return fmt.Errorf("failed to load config from existing snapshot: %w", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "DEBUG: Fresh start - generating new config (rootDataDir=%s)\n", lc.options.rootDataDir)

		// Use the appropriate genesis configuration based on network ID
		// CRITICAL: For mainnet/testnet, ALWAYS use canonical genesis to ensure byte-for-byte
		// deterministic output. This prevents "db contains invalid genesis hash" errors on restart.
		// The canonical genesis files are pre-serialized and never re-generated.

		// Check if MNEMONIC is set - if so, use mnemonic-based config to generate
		// genesis with funds allocated to the mnemonic-derived address
		mnemonic := os.Getenv("MNEMONIC")
		useMnemonic := mnemonic != ""

		switch networkID {
		case constants.MainnetID: // LUX Mainnet (1)
			if useMnemonic {
				ux.Print(lc.log, "%s", log.Green.Wrap("Loading mainnet genesis FROM MNEMONIC (funds allocated to derived address)"))
				cfg, err = local.NewMainnetConfigFromMnemonic(lc.options.execPath, lc.options.numNodes)
			} else {
				// ALWAYS use canonical genesis for mainnet - never regenerate
				ux.Print(lc.log, "%s", log.Green.Wrap("Loading CANONICAL mainnet genesis (deterministic bytes)"))
				cfg, err = local.NewCanonicalMainnetConfig(lc.options.execPath, lc.options.numNodes)
			}
		case constants.TestnetID: // LUX Testnet (2)
			if useMnemonic {
				ux.Print(lc.log, "%s", log.Green.Wrap("Loading testnet genesis FROM MNEMONIC (funds allocated to derived address)"))
				cfg, err = local.NewTestnetConfigFromMnemonic(lc.options.execPath, lc.options.numNodes)
			} else {
				// ALWAYS use canonical genesis for testnet - never regenerate
				ux.Print(lc.log, "%s", log.Green.Wrap("Loading CANONICAL testnet genesis (deterministic bytes)"))
				cfg, err = local.NewCanonicalTestnetConfig(lc.options.execPath, lc.options.numNodes)
			}
		case constants.DevnetID: // LUX Devnet (3)
			if useMnemonic {
				ux.Print(lc.log, "%s", log.Green.Wrap("Loading devnet genesis FROM MNEMONIC (funds allocated to derived address)"))
				cfg, err = local.NewDevnetConfigFromMnemonic(lc.options.execPath, lc.options.numNodes)
			} else {
				ux.Print(lc.log, "%s", log.Green.Wrap("Loading CANONICAL devnet genesis (deterministic bytes)"))
				cfg, err = local.NewCanonicalDevnetConfig(lc.options.execPath, lc.options.numNodes)
			}
		case constants.LocalID: // Custom/Local (1337)
			if useMnemonic {
				ux.Print(lc.log, "%s", log.Green.Wrap("Loading custom genesis FROM MNEMONIC (funds allocated to derived address)"))
				cfg, err = local.NewLocalConfigFromMnemonic(lc.options.execPath, lc.options.numNodes)
			} else {
				// Use canonical genesis for custom network
				ux.Print(lc.log, "%s", log.Green.Wrap("Loading CANONICAL custom genesis (deterministic bytes)"))
				cfg, err = local.NewCanonicalCustomConfig(lc.options.execPath, lc.options.numNodes)
			}
		default:
			// Fallback for unknown network IDs
			ux.Print(lc.log, "%s", log.Orange.Wrap(fmt.Sprintf("Unknown network ID %d, using default config", networkID)))
			cfg, err = local.NewDefaultConfigNNodes(lc.options.execPath, lc.options.numNodes)
		}
		if err != nil {
			return err
		}
	}

	// Ensure required staking flags are set for non-local networks
	// stake-minting-period must be >= max-stake-duration (1 year = 8760h)
	if _, ok := cfg.Flags["stake-minting-period"]; !ok {
		cfg.Flags["stake-minting-period"] = "8760h" // 1 year
	}
	if _, ok := cfg.Flags["max-stake-duration"]; !ok {
		cfg.Flags["max-stake-duration"] = "8760h" // 1 year
	}

	// set flags applied to all nodes
	for k, v := range globalConfig {
		cfg.Flags[k] = v
	}

	// Extract track-chains from global config if present
	// This allows CLI to pass track-chains in globalNodeConfig JSON
	if trackChainsVal, ok := globalConfig["track-chains"]; ok {
		if trackChainsStr, ok := trackChainsVal.(string); ok {
			lc.options.trackChains = trackChainsStr
		}
	}

	if lc.pluginDir != "" {
		cfg.Flags[config.PluginDirKey] = lc.pluginDir
	}

	for i := range cfg.NodeConfigs {
		// NOTE: Naming convention for node names is currently `node` + number, i.e. `node1,node2,node3,...node101`
		nodeName := fmt.Sprintf("node%d", i+1)

		cfg.NodeConfigs[i].Name = nodeName

		// Initialize maps if nil to avoid panic when assigning
		if cfg.NodeConfigs[i].Flags == nil {
			cfg.NodeConfigs[i].Flags = map[string]interface{}{}
		}
		if cfg.NodeConfigs[i].ChainConfigFiles == nil {
			cfg.NodeConfigs[i].ChainConfigFiles = map[string]string{}
		}
		if cfg.NodeConfigs[i].UpgradeConfigFiles == nil {
			cfg.NodeConfigs[i].UpgradeConfigFiles = map[string]string{}
		}
		if cfg.NodeConfigs[i].ChainConfigFiles == nil {
			cfg.NodeConfigs[i].ChainConfigFiles = map[string]string{}
		}

		// Propagate global config flags to each node config
		// This ensures track-chains and other global settings apply to all nodes
		for k, v := range globalConfig {
			cfg.NodeConfigs[i].Flags[k] = v
		}

		for k, v := range lc.options.chainConfigs {
			cfg.NodeConfigs[i].ChainConfigFiles[k] = v
		}
		for k, v := range lc.options.upgradeConfigs {
			cfg.NodeConfigs[i].UpgradeConfigFiles[k] = v
		}
		for k, v := range lc.options.chainConfigs {
			cfg.NodeConfigs[i].ChainConfigFiles[k] = v
		}

		if lc.options.dynamicPorts {
			// remove http port defined in local network config, to get dynamic port generation
			delete(cfg.NodeConfigs[i].Flags, config.HTTPPortKey)
			delete(cfg.NodeConfigs[i].Flags, config.StakingPortKey)
		}

		if lc.options.trackChains != "" {
			cfg.NodeConfigs[i].Flags[config.TrackChainsKey] = lc.options.trackChains
		}

		cfg.NodeConfigs[i].BinaryPath = lc.execPath
		cfg.NodeConfigs[i].RedirectStdout = lc.options.redirectNodesOutput
		cfg.NodeConfigs[i].RedirectStderr = lc.options.redirectNodesOutput

		// set flags applied to the specific node
		var customNodeConfig map[string]interface{}
		if lc.options.customNodeConfigs != nil && lc.options.customNodeConfigs[nodeName] != "" {
			if err := json.Unmarshal([]byte(lc.options.customNodeConfigs[nodeName]), &customNodeConfig); err != nil {
				return err
			}
		}
		for k, v := range customNodeConfig {
			cfg.NodeConfigs[i].Flags[k] = v
		}
	}

	lc.cfg = cfg
	return nil
}

// Creates a network and sets [lc.nw] to it.
// Assumes [lc.lock] isn't held.
func (lc *localNetwork) Start(ctx context.Context) error {
	lc.lock.Lock()
	defer lc.lock.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func(ctx context.Context) {
		select {
		case <-lc.stopCh:
			// The network is stopped; return from method calls below.
			cancel()
		case <-ctx.Done():
			// This method is done. Don't leak [ctx].
		}
	}(ctx)

	if err := lc.createConfig(); err != nil {
		return err
	}

	ux.Print(lc.log, "%s", log.Blue.Wrap(log.Bold.Wrap("create and run local network")))
	nw, err := local.NewNetwork(lc.log, lc.cfg, lc.options.rootDataDir, lc.options.snapshotsDir, lc.options.reassignPortsIfUsed)
	if err != nil {
		return err
	}
	lc.nw = nw

	// node info is already available
	if err := lc.updateNodeInfo(); err != nil {
		return err
	}

	if err := lc.awaitHealthyAndUpdateNetworkInfo(ctx); err != nil {
		return err
	}

	return nil
}

// checkForExistingGenesis checks if we're resuming from existing state.
// Returns (genesisJSON, isResume) where isResume is true if:
// 1. node1/genesis.json exists, AND
// 2. node1/db directory has data (meaning blocks have been committed)
//
// This is critical because JSON serialization is non-deterministic in Go
// (map iteration order varies), so regenerating genesis from the same source
// data produces different bytes, causing "db contains invalid genesis hash" errors.
func (lc *localNetwork) checkForExistingGenesis() (string, bool) {
	if lc.options.rootDataDir == "" {
		return "", false
	}

	// Check node1 for existing genesis and db data
	node1Dir := filepath.Join(lc.options.rootDataDir, "node1")
	genesisPath := filepath.Join(node1Dir, "genesis.json")
	dbDir := filepath.Join(node1Dir, "db")

	// Check if genesis.json exists
	genesisBytes, err := os.ReadFile(genesisPath)
	if err != nil {
		// No existing genesis file - this is a fresh start
		return "", false
	}

	// Check if db directory has data (indicating we're resuming)
	entries, err := os.ReadDir(dbDir)
	if err != nil || len(entries) == 0 {
		// No db data - this is a fresh start even though genesis file exists
		// (perhaps from a failed previous start)
		return "", false
	}

	// Both genesis.json exists AND db has data - we're resuming
	// Note: We can't log here since lc.log may not be set yet during config creation
	return string(genesisBytes), true
}

// Creates the blockchains specified in [chainSpecs].
// Assumes [lc.lock] isn't held.
func (lc *localNetwork) CreateChains(
	ctx context.Context,
	chainSpecs []network.ChainSpec, // VM name + genesis bytes
) ([]ids.ID, error) {
	lc.lock.Lock()
	defer lc.lock.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func(ctx context.Context) {
		select {
		case <-lc.stopCh:
			// The network is stopped; return from method calls below.
			cancel()
		case <-ctx.Done():
			// This method is done. Don't leak [ctx].
		}
	}(ctx)

	if len(chainSpecs) == 0 {
		return nil, nil
	}

	if err := lc.awaitHealthyAndUpdateNetworkInfo(ctx); err != nil {
		return nil, err
	}

	chainIDs, err := lc.nw.CreateChains(ctx, chainSpecs)
	if err != nil {
		return nil, err
	}

	if err := lc.awaitHealthyAndUpdateNetworkInfo(ctx); err != nil {
		return nil, err
	}

	return chainIDs, nil
}

func (lc *localNetwork) AddPermissionlessValidators(ctx context.Context, validatorSpecs []network.PermissionlessValidatorSpec) error {
	lc.lock.Lock()
	defer lc.lock.Unlock()

	if len(validatorSpecs) == 0 {
		ux.Print(lc.log, "%s", log.Orange.Wrap(log.Bold.Wrap("no validator specs provided...")))
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func(ctx context.Context) {
		select {
		case <-lc.stopCh:
			// The network is stopped; return from method calls below.
			cancel()
		case <-ctx.Done():
			// This method is done. Don't leak [ctx].
		}
	}(ctx)

	if err := lc.awaitHealthyAndUpdateNetworkInfo(ctx); err != nil {
		return err
	}

	err := lc.nw.AddPermissionlessValidators(ctx, validatorSpecs)
	if err != nil {
		return err
	}

	if err := lc.awaitHealthyAndUpdateNetworkInfo(ctx); err != nil {
		return err
	}

	ux.Print(lc.log, "%s", log.Green.Wrap(log.Bold.Wrap("finished adding permissionless validators")))
	return nil
}

func (lc *localNetwork) RemoveChainValidator(ctx context.Context, validatorSpecs []network.RemoveChainValidatorSpec) error {
	lc.lock.Lock()
	defer lc.lock.Unlock()

	if len(validatorSpecs) == 0 {
		ux.Print(lc.log, "%s", log.Orange.Wrap(log.Bold.Wrap("no validator specs provided...")))
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func(ctx context.Context) {
		select {
		case <-lc.stopCh:
			// The network is stopped; return from method calls below.
			cancel()
		case <-ctx.Done():
			// This method is done. Don't leak [ctx].
		}
	}(ctx)

	if err := lc.awaitHealthyAndUpdateNetworkInfo(ctx); err != nil {
		return err
	}

	err := lc.nw.RemoveChainValidators(ctx, validatorSpecs)
	if err != nil {
		return err
	}

	if err := lc.awaitHealthyAndUpdateNetworkInfo(ctx); err != nil {
		return err
	}

	ux.Print(lc.log, "%s", log.Green.Wrap(log.Bold.Wrap("finished removing chain validators")))
	return nil
}

func (lc *localNetwork) TransformChains(ctx context.Context, elasticChainSpecs []network.ElasticChainSpec) ([]ids.ID, []ids.ID, error) {
	lc.lock.Lock()
	defer lc.lock.Unlock()

	if len(elasticChainSpecs) == 0 {
		ux.Print(lc.log, "%s", log.Orange.Wrap(log.Bold.Wrap("no chains specified...")))
		return nil, nil, nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func(ctx context.Context) {
		select {
		case <-lc.stopCh:
			// The network is stopped; return from method calls below.
			cancel()
		case <-ctx.Done():
			// This method is done. Don't leak [ctx].
		}
	}(ctx)

	if err := lc.awaitHealthyAndUpdateNetworkInfo(ctx); err != nil {
		return nil, nil, err
	}

	chainIDs, assetIDs, err := lc.nw.TransformChain(ctx, elasticChainSpecs)
	if err != nil {
		return nil, nil, err
	}

	if err := lc.awaitHealthyAndUpdateNetworkInfo(ctx); err != nil {
		return nil, nil, err
	}

	ux.Print(lc.log, "%s", log.Green.Wrap(log.Bold.Wrap("finished transforming chains")))
	return chainIDs, assetIDs, nil
}

// CreateParticipantGroups creates the given number of participant groups (validators).
// Assumes [lc.lock] isn't held.
func (lc *localNetwork) CreateParticipantGroups(ctx context.Context, participantsSpecs []network.ParticipantsSpec) ([]ids.ID, error) {
	lc.lock.Lock()
	defer lc.lock.Unlock()

	if len(participantsSpecs) == 0 {
		ux.Print(lc.log, "%s", log.Orange.Wrap(log.Bold.Wrap("no chains specified...")))
		return nil, nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func(ctx context.Context) {
		select {
		case <-lc.stopCh:
			// The network is stopped; return from method calls below.
			cancel()
		case <-ctx.Done():
			// This method is done. Don't leak [ctx].
		}
	}(ctx)

	if err := lc.awaitHealthyAndUpdateNetworkInfo(ctx); err != nil {
		return nil, err
	}

	chainIDs, err := lc.nw.CreateParticipantGroups(ctx, participantsSpecs)
	if err != nil {
		return nil, err
	}

	if err := lc.awaitHealthyAndUpdateNetworkInfo(ctx); err != nil {
		return nil, err
	}

	ux.Print(lc.log, "%s", log.Green.Wrap(log.Bold.Wrap("finished adding chains")))
	return chainIDs, nil
}

// Loads a snapshot and sets [l.nw] to the network created from the snapshot.
// Assumes [lc.lock] isn't held.
func (lc *localNetwork) LoadSnapshot(snapshotName string) error {
	lc.lock.Lock()
	defer lc.lock.Unlock()

	ux.Print(lc.log, "%s", log.Blue.Wrap(log.Bold.Wrap("create and run local network from snapshot")))

	var globalNodeConfig map[string]interface{}
	if lc.options.globalNodeConfig != "" {
		if err := json.Unmarshal([]byte(lc.options.globalNodeConfig), &globalNodeConfig); err != nil {
			return err
		}
	}

	nw, err := local.NewNetworkFromSnapshot(
		lc.log,
		snapshotName,
		lc.options.rootDataDir,
		lc.options.snapshotsDir,
		lc.execPath,
		lc.pluginDir,
		lc.options.chainConfigs,
		lc.options.upgradeConfigs,
		lc.options.chainConfigs,
		globalNodeConfig,
		lc.options.reassignPortsIfUsed,
	)
	if err != nil {
		return err
	}
	lc.nw = nw

	if err := lc.updateNodeInfo(); err != nil {
		return err
	}

	return nil
}

// Populates [lc.customChainIDToInfo] for all chains other than those on
// the Primary Network (P-Chain, X-Chain, C-Chain.)
// Populates [lc.chains] with all chains that exist.
// Doesn't contain the Primary network.
// Assumes [lc.lock] is held.
func (lc *localNetwork) updateChainInfo(ctx context.Context) error {
	nodes, err := lc.nw.GetAllNodes()
	if err != nil {
		return err
	}
	minAPIPortNumber := uint16(local.MaxPort)
	var node node.Node
	for _, n := range nodes {
		if n.GetPaused() {
			continue
		}
		if n.GetAPIPort() < minAPIPortNumber {
			minAPIPortNumber = n.GetAPIPort()
			node = n
		}
	}

	if node == nil {
		return fmt.Errorf("no active nodes found in network")
	}

	pChainClient := node.GetAPIClient().PChainAPI()
	if pChainClient == nil {
		return fmt.Errorf("P-Chain client is nil")
	}

	// Retry GetBlockchains with exponential backoff
	// The P-Chain may still be initializing even after health check passes
	maxRetries := 120
	retryInterval := 500 * time.Millisecond
	blockchains, err := (*pChainClient).GetBlockchains(ctx)
	for i := 1; err != nil && i < maxRetries; i++ {
		lc.log.Debug("P-Chain GetBlockchains failed, retrying...",
			log.Int("attempt", i+1),
			log.Int("maxRetries", maxRetries),
			log.Err(err))
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for P-Chain: %w", ctx.Err())
		case <-time.After(retryInterval):
			// Continue to next retry
		}
		blockchains, err = (*pChainClient).GetBlockchains(ctx)
	}
	if err != nil {
		return fmt.Errorf("P-Chain GetBlockchains failed after %d retries: %w", maxRetries, err)
	}

	for _, blockchain := range blockchains {
		if blockchain.Name == "C-Chain" || blockchain.Name == "X-Chain" {
			continue
		}
		lc.customChainIDToInfo[blockchain.ID] = chainInfo{
			info: &rpcpb.CustomChainInfo{
				ChainName:    blockchain.Name,
				VmId:         blockchain.VMID.String(),
				PchainId:     blockchain.NetID.String(),
				BlockchainId: blockchain.ID.String(),
			},
			chainID:      blockchain.NetID,
			blockchainID: blockchain.ID,
		}
	}

	// Retry GetNets with same logic
	chains, err := (*pChainClient).GetNets(ctx, nil)
	for i := 1; err != nil && i < maxRetries; i++ {
		lc.log.Debug("P-Chain GetNets failed, retrying...",
			log.Int("attempt", i+1),
			log.Int("maxRetries", maxRetries),
			log.Err(err))
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for P-Chain GetNets: %w", ctx.Err())
		case <-time.After(retryInterval):
			// Continue to next retry
		}
		chains, err = (*pChainClient).GetNets(ctx, nil)
	}
	if err != nil {
		return fmt.Errorf("P-Chain GetNets failed after %d retries: %w", maxRetries, err)
	}

	chainIDList := []string{}
	for _, chain := range chains {
		// Skip both PlatformChainID and PrimaryNetworkID (which is ids.Empty)
		if chain.ID != constants.PlatformChainID && chain.ID != constants.PrimaryNetworkID {
			chainIDList = append(chainIDList, chain.ID.String())
		}
	}

	for _, chainIDStr := range chainIDList {
		chainID, err := ids.FromString(chainIDStr)
		if err != nil {
			return err
		}
		vdrs, err := (*pChainClient).GetCurrentValidators(ctx, chainID, nil)
		if err != nil {
			return err
		}
		var nodeNameList []string

		for _, node := range vdrs {
			for nodeName, nodeInfo := range lc.nodeInfos {
				if nodeInfo.Id == node.NodeID.String() {
					nodeNameList = append(nodeNameList, nodeName)
				}
			}
		}

		isElastic := false
		elasticChainID := ids.Empty
		if _, _, err := (*pChainClient).GetCurrentSupply(ctx, chainID); err == nil {
			// Only mark as elastic if we can get the elastic chain ID
			// (chain must have been transformed via IssueTransformChainTx)
			if eid, err := lc.nw.GetElasticChainID(ctx, chainID); err == nil {
				isElastic = true
				elasticChainID = eid
			}
		}

		lc.chains[chainIDStr] = &rpcpb.ChainInfo{
			IsElastic:         isElastic,
			ElasticChainId:    elasticChainID.String(),
			ChainParticipants: &rpcpb.ChainParticipants{NodeNames: nodeNameList},
		}
	}

	for chainID, chainInfo := range lc.customChainIDToInfo {
		vs, err := (*pChainClient).GetCurrentValidators(ctx, chainInfo.chainID, nil)
		if err != nil {
			return err
		}
		nodeNames := []string{}
		for _, v := range vs {
			for nodeName, nodeInfo := range lc.nodeInfos {
				if v.NodeID.String() == nodeInfo.Id {
					nodeNames = append(nodeNames, nodeName)
				}
			}
		}
		if len(nodeNames) != len(vs) {
			return fmt.Errorf("not all chain validators are in network for chain %s", chainInfo.chainID.String())
		}

		sort.Strings(nodeNames)
		for _, nodeName := range nodeNames {
			nodeInfo := lc.nodeInfos[nodeName]
			if nodeInfo.Paused {
				continue
			}
			lc.log.Info(fmt.Sprintf(log.LightBlue.Wrap("[blockchain RPC for %q] \"%s/v1/chain/%s\""), chainInfo.info.VmId, nodeInfo.GetUri(), chainID))
		}
	}

	return nil
}

// Assumes [lc.lock] isn't held.
func (lc *localNetwork) AwaitHealthyAndUpdateNetworkInfo(ctx context.Context) error {
	lc.lock.Lock()
	defer lc.lock.Unlock()

	return lc.awaitHealthyAndUpdateNetworkInfo(ctx)
}

// Returns nil when [lc.nw] reports healthy.
// Updates node and chain info.
// Assumes [lc.lock] is held.
func (lc *localNetwork) awaitHealthyAndUpdateNetworkInfo(ctx context.Context) error {
	ux.Print(lc.log, "%s", log.Blue.Wrap(log.Bold.Wrap("waiting for all nodes to report healthy...")))

	if err := lc.nw.Healthy(ctx); err != nil {
		return err
	}

	if err := lc.updateNodeInfo(); err != nil {
		return err
	}

	if err := lc.updateChainInfo(ctx); err != nil {
		// For fresh mainnet/testnet starts, P-Chain API may not be ready immediately
		// This is non-fatal since updateChainInfo only populates custom chain info
		lc.log.Warn("updateChainInfo failed (non-fatal for initial start)", log.Err(err))
	}

	nodeNames := slices.Collect(maps.Keys(lc.nodeInfos))
	sort.Strings(nodeNames)
	for _, nodeName := range nodeNames {
		nodeInfo := lc.nodeInfos[nodeName]
		if nodeInfo.Paused {
			continue
		}
		lc.log.Debug(fmt.Sprintf(log.Cyan.Wrap("node-info: node-name %s, node-ID: %s, URI: %s"), nodeName, nodeInfo.Id, nodeInfo.Uri))
	}

	return nil
}

// Assumes [lc.lock] isn't held.
func (lc *localNetwork) UpdateNodeInfo() error {
	lc.lock.Lock()
	defer lc.lock.Unlock()

	return lc.updateNodeInfo()
}

// Populates [lc.nodeNames] and [lc.nodeInfos] for
// all nodes in this network.
// Assumes [lc.lock] is held.
func (lc *localNetwork) updateNodeInfo() error {
	nodes, err := lc.nw.GetAllNodes()
	if err != nil {
		return err
	}

	lc.nodeInfos = make(map[string]*rpcpb.NodeInfo)
	for name, node := range nodes {
		trackChains, err := node.GetFlag(config.TrackChainsKey)
		if err != nil {
			return err
		}

		lc.nodeInfos[name] = &rpcpb.NodeInfo{
			Name:              node.GetName(),
			Uri:               node.GetURL(),
			Id:                node.GetNodeID().String(),
			ExecPath:          node.GetBinaryPath(),
			LogDir:            node.GetLogsDir(),
			DbDir:             node.GetDbDir(),
			Config:            []byte(node.GetConfigFile()),
			PluginDir:         node.GetPluginDir(),
			WhitelistedChains: trackChains,
			Paused:            node.GetPaused(),
		}

		// update default exec and pluginDir if empty (snapshots started without these params)
		if lc.execPath == "" {
			lc.execPath = node.GetBinaryPath()
		}
		if lc.pluginDir == "" {
			lc.pluginDir = node.GetPluginDir()
		}
	}
	return lc.generatePrometheusConf()
}

func (lc *localNetwork) generatePrometheusConf() error {
	if lc.prometheusConfPath == "" {
		lc.prometheusConfPath = filepath.Join(lc.options.rootDataDir, prometheusConfFname)
		lc.log.Info(fmt.Sprintf(log.Cyan.Wrap("prometheus conf file %s"), lc.prometheusConfPath))
	}
	prometheusConf := prometheusConfCommon
	for _, nodeInfo := range lc.nodeInfos {
		if !nodeInfo.Paused {
			prometheusConf += "        - " + strings.TrimPrefix(nodeInfo.Uri, "http://") + "\n"
		}
	}
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(lc.prometheusConfPath), 0755); err != nil {
		return err
	}
	file, err := os.Create(lc.prometheusConfPath)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write([]byte(prometheusConf))
	return err
}

// Assumes [lc.lock] isn't held.
func (lc *localNetwork) Stop(ctx context.Context) {
	lc.stopOnce.Do(func() {
		close(lc.stopCh) // Stop in-flight method executions

		lc.lock.Lock()
		defer lc.lock.Unlock()

		if lc.nw != nil {
			err := lc.nw.Stop(ctx)
			msg := "terminated network"
			if err != nil {
				msg += fmt.Sprintf(" (error %v)", err)
			}
			ux.Print(lc.log, "%s", log.Red.Wrap(msg))
		}
	})
}
