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

	"github.com/luxfi/netrunner/local"
	"github.com/luxfi/netrunner/network"
	"github.com/luxfi/netrunner/network/node"
	"github.com/luxfi/netrunner/rpcpb"
	"github.com/luxfi/netrunner/utils/constants"
	"github.com/luxfi/netrunner/ux"
	"github.com/luxfi/node/config"
	"github.com/luxfi/ids"
	luxd_constants "github.com/luxfi/constants"
	"github.com/luxfi/log"
	"golang.org/x/exp/maps"
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
    metrics_path: /ext/metrics
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
	chainID     ids.ID
	blockchainID ids.ID
}

type localNetworkOptions struct {
	execPath            string
	rootDataDir         string
	numNodes            uint32
	trackChains        string
	redirectNodesOutput bool
	globalNodeConfig    string

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
	logger, err := logFactory.Make(constants.LogNameMain)
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
		chains:             make(map[string]*rpcpb.ChainInfo),
	}, nil
}

// TODO document.
// Assumes [lc.lock] is held.
func (lc *localNetwork) createConfig() error {
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
	networkID := luxd_constants.LocalID // default to local
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

	// Use the appropriate genesis configuration based on network ID
	// If LUX_MNEMONIC is set, derive validators from it (preferred for mainnet/testnet)
	mnemonic := os.Getenv("LUX_MNEMONIC")
	useMnemonic := mnemonic != ""
	fmt.Printf("🔍 DEBUG: LUX_MNEMONIC set: %v (len=%d)\n", useMnemonic, len(mnemonic))

	switch networkID {
	case 96369: // LUX Mainnet
		if useMnemonic {
			fmt.Printf("🔑 Using NewMainnetConfigFromMnemonic (mnemonic-derived keys)\n")
			cfg, err = local.NewMainnetConfigFromMnemonic(lc.options.execPath, lc.options.numNodes)
		} else {
			fmt.Printf("📦 Using NewMainnetConfig (embedded keys)\n")
			cfg, err = local.NewMainnetConfig(lc.options.execPath, lc.options.numNodes)
		}
	case 96368: // LUX Testnet
		if useMnemonic {
			cfg, err = local.NewTestnetConfigFromMnemonic(lc.options.execPath, lc.options.numNodes)
		} else {
			// Use pre-existing keys from ~/.lux/keys if available
			fmt.Printf("📦 Using NewTestnetConfigWithKeys (pre-existing keys from ~/.lux/keys)\n")
			cfg, err = local.NewTestnetConfigWithKeys(lc.options.execPath, "")
		}
	default:
		cfg, err = local.NewDefaultConfigNNodes(lc.options.execPath, lc.options.numNodes)
	}
	if err != nil {
		return err
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
	blockchains, err := (*pChainClient).GetBlockchains(ctx)
	if err != nil {
		return err
	}

	for _, blockchain := range blockchains {
		if blockchain.Name == "C-Chain" || blockchain.Name == "X-Chain" {
			continue
		}
		lc.customChainIDToInfo[blockchain.ID] = chainInfo{
			info: &rpcpb.CustomChainInfo{
				ChainName: blockchain.Name,
				VmId:      blockchain.VMID.String(),
				PchainId:  blockchain.NetID.String(),
				BlockchainId:   blockchain.ID.String(),
			},
			chainID:     blockchain.NetID,
			blockchainID: blockchain.ID,
		}
	}

	chains, err := (*pChainClient).GetNets(ctx, nil)
	if err != nil {
		return err
	}

	chainIDList := []string{}
	for _, chain := range chains {
		// Skip both PlatformChainID and PrimaryNetworkID (which is ids.Empty)
		if chain.ID != luxd_constants.PlatformChainID && chain.ID != luxd_constants.PrimaryNetworkID {
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
			isElastic = true
			elasticChainID, err = lc.nw.GetElasticChainID(ctx, chainID)
			if err != nil {
				return err
			}
		}

		lc.chains[chainIDStr] = &rpcpb.ChainInfo{
			IsElastic:          isElastic,
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
			lc.log.Info(fmt.Sprintf(log.LightBlue.Wrap("[blockchain RPC for %q] \"%s/ext/bc/%s\""), chainInfo.info.VmId, nodeInfo.GetUri(), chainID))
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
		return err
	}

	nodeNames := maps.Keys(lc.nodeInfos)
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
			Name:               node.GetName(),
			Uri:                node.GetURL(),
			Id:                 node.GetNodeID().String(),
			ExecPath:           node.GetBinaryPath(),
			LogDir:             node.GetLogsDir(),
			DbDir:              node.GetDbDir(),
			Config:             []byte(node.GetConfigFile()),
			PluginDir:          node.GetPluginDir(),
			WhitelistedChains: trackChains,
			Paused:             node.GetPaused(),
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
