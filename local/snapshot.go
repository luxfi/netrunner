package local

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"maps"
	"slices"

	luxconfig "github.com/luxfi/config"
	"github.com/luxfi/constantsants"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/netrunner/api"
	"github.com/luxfi/netrunner/network"
	"github.com/luxfi/netrunner/network/node"
	"github.com/luxfi/netrunner/utils"

	"github.com/luxfi/config"
	"github.com/luxfi/sdk/api/admin"
	dircopy "github.com/otiai10/copy"
)

// NetworkState defines dynamic network information not available on chain db
type NetworkState struct {
	// Map from chain id to elastic chain tx id
	ChainID2ElasticChainID map[string]string `json:"chainID2ElasticChainID"`
}

const (
	hotSnapshotManifestName = "hot_snapshot.json"
	hotSnapshotVersion      = 1
)

type hotSnapshotManifest struct {
	Version int                        `json:"version"`
	Network string                     `json:"network"`
	Nodes   map[string]hotSnapshotNode `json:"nodes"`
}

type hotSnapshotNode struct {
	LastVersion uint64              `json:"last_version"`
	Backups     []hotSnapshotBackup `json:"backups"`
}

type hotSnapshotBackup struct {
	Since     uint64 `json:"since"`
	Version   uint64 `json:"version"`
	Path      string `json:"path"`
	CreatedAt string `json:"created_at"`
}

// NewNetwork returns a new network from the given snapshot
func NewNetworkFromSnapshot(
	log log.Logger,
	snapshotName string,
	rootDir string,
	snapshotsDir string,
	binaryPath string,
	pluginDir string,
	chainConfigs map[string]string,
	upgradeConfigs map[string]string,
	pChainConfigs map[string]string,
	flags map[string]interface{},
	reassignPortsIfUsed bool,
) (network.Network, error) {
	net, err := newNetwork(
		log,
		api.NewAPIClient,
		&nodeProcessCreator{
			colorPicker: utils.NewColorPicker(),
			logger:      log,
			stdout:      os.Stdout,
			stderr:      os.Stderr,
		},
		rootDir,
		snapshotsDir,
		reassignPortsIfUsed,
	)
	if err != nil {
		return net, err
	}
	err = net.loadSnapshot(
		context.Background(),
		snapshotName,
		binaryPath,
		pluginDir,
		chainConfigs,
		upgradeConfigs,
		pChainConfigs,
		flags,
	)
	return net, err
}

// Save network snapshot
// Network is stopped in order to do a safe preservation
func (ln *localNetwork) SaveSnapshot(ctx context.Context, snapshotName string) (string, error) {
	ln.lock.Lock()
	defer ln.lock.Unlock()

	if ln.stopCalled() {
		return "", network.ErrStopped
	}
	if len(snapshotName) == 0 {
		return "", fmt.Errorf("invalid snapshotName %q", snapshotName)
	}
	// check if snapshot already exists
	snapshotDir := filepath.Join(ln.snapshotsDir, snapshotPrefix+snapshotName)
	if _, err := os.Stat(snapshotDir); err == nil {
		return "", fmt.Errorf("snapshot %q already exists", snapshotName)
	}
	// keep copy of node info that will be removed by stop
	nodesConfig := map[string]node.Config{}
	nodesDBDir := map[string]string{}
	for nodeName, node := range ln.nodes {
		nodeConfig := node.config
		// depending on how the user generated the config, different nodes config flags
		// may point to the same map, so we made a copy to avoid always modifying the same value
		nodeConfig.Flags = maps.Clone(nodeConfig.Flags)
		nodesConfig[nodeName] = nodeConfig
		nodesDBDir[nodeName] = node.GetDbDir()
	}
	// we change nodeConfig.Flags so as to preserve in snapshot the current node ports
	for nodeName, nodeConfig := range nodesConfig {
		nodeConfig.Flags[config.HTTPPortKey] = ln.nodes[nodeName].GetAPIPort()
		nodeConfig.Flags[config.StakingPortKey] = ln.nodes[nodeName].GetP2PPort()
	}
	// make copy of network flags
	networkConfigFlags := maps.Clone(ln.flags)
	// remove all data dir, log dir references
	delete(networkConfigFlags, config.DataDirKey)
	delete(networkConfigFlags, config.LogsDirKey)
	for nodeName, nodeConfig := range nodesConfig {
		if nodeConfig.ConfigFile != "" {
			var err error
			nodeConfig.ConfigFile, err = utils.SetJSONKey(nodeConfig.ConfigFile, config.LogsDirKey, "")
			if err != nil {
				return "", err
			}
		}
		delete(nodeConfig.Flags, config.DataDirKey)
		delete(nodeConfig.Flags, config.LogsDirKey)
		nodesConfig[nodeName] = nodeConfig
	}

	// stop network to safely save snapshot
	if err := ln.stop(ctx); err != nil {
		return "", err
	}
	syncFilesystem()
	// create main snapshot dirs
	snapshotDBDir := filepath.Join(snapshotDir, defaultDBSubdir)
	if err := os.MkdirAll(snapshotDBDir, os.ModePerm); err != nil {
		return "", err
	}
	// save db
	for _, nodeConfig := range nodesConfig {
		sourceDBDir, ok := nodesDBDir[nodeConfig.Name]
		if !ok {
			return "", fmt.Errorf("failure obtaining db path for node %q", nodeConfig.Name)
		}
		sourceDBDir = filepath.Join(sourceDBDir, constants.NetworkName(ln.networkID))
		targetDBDir := filepath.Join(filepath.Join(snapshotDBDir, nodeConfig.Name), constants.NetworkName(ln.networkID))
		if err := dircopy.Copy(sourceDBDir, targetDBDir); err != nil {
			return "", fmt.Errorf("failure saving node %q db dir: %w", nodeConfig.Name, err)
		}
	}
	// save network conf
	networkConfig := network.Config{
		Genesis:            string(ln.genesis),
		Flags:              networkConfigFlags,
		NodeConfigs:        []node.Config{},
		BinaryPath:         ln.binaryPath,
		ChainConfigFiles:   ln.chainConfigFiles,
		GenesisConfigFiles: ln.genesisConfigFiles,
		UpgradeConfigFiles: ln.upgradeConfigFiles,
		PChainConfigFiles:  ln.pChainConfigFiles,
	}

	// no need to save this, will be generated automatically on snapshot load
	networkConfig.NodeConfigs = append(networkConfig.NodeConfigs, slices.Collect(maps.Values(nodesConfig))...)
	networkConfigJSON, err := json.MarshalIndent(networkConfig, "", "    ")
	if err != nil {
		return "", err
	}
	if err := createFileAndWrite(filepath.Join(snapshotDir, "network.json"), networkConfigJSON); err != nil {
		return "", err
	}
	// save dynamic part of network not available on blockchain
	chainID2ElasticChainID := map[string]string{}
	for chainID, elasticChainID := range ln.chainID2ElasticChainID {
		chainID2ElasticChainID[chainID.String()] = elasticChainID.String()
	}
	networkState := NetworkState{
		ChainID2ElasticChainID: chainID2ElasticChainID,
	}
	networkStateJSON, err := json.MarshalIndent(networkState, "", "    ")
	if err != nil {
		return "", err
	}
	if err := createFileAndWrite(filepath.Join(snapshotDir, "state.json"), networkStateJSON); err != nil {
		return "", err
	}
	return snapshotDir, nil
}

// start network from snapshot
func (ln *localNetwork) loadSnapshot(
	ctx context.Context,
	snapshotName string,
	binaryPath string,
	pluginDir string,
	chainConfigs map[string]string,
	upgradeConfigs map[string]string,
	pChainConfigs map[string]string,
	flags map[string]interface{},
) error {
	ln.lock.Lock()
	defer ln.lock.Unlock()

	snapshotDir := filepath.Join(ln.snapshotsDir, snapshotPrefix+snapshotName)
	snapshotDBDir := filepath.Join(snapshotDir, defaultDBSubdir)
	_, err := os.Stat(snapshotDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrSnapshotNotFound
		} else {
			return fmt.Errorf("failure accessing snapshot %q: %w", snapshotName, err)
		}
	}
	// load network config
	networkConfigJSON, err := os.ReadFile(filepath.Join(snapshotDir, "network.json"))
	if err != nil {
		return fmt.Errorf("failure reading network config file from snapshot: %w", err)
	}
	networkConfig := network.Config{}
	if err := json.Unmarshal(networkConfigJSON, &networkConfig); err != nil {
		return fmt.Errorf("failure unmarshaling network config from snapshot: %w", err)
	}
	// add flags
	for i := range networkConfig.NodeConfigs {
		for k, v := range flags {
			networkConfig.NodeConfigs[i].Flags[k] = v
		}
	}
	hotManifestPath := filepath.Join(snapshotDir, hotSnapshotManifestName)
	hotManifest, err := loadHotSnapshotManifest(hotManifestPath)
	if err != nil {
		return err
	}
	if hotManifest != nil {
		if err := ln.prepareHotSnapshotDirs(networkConfig.NodeConfigs); err != nil {
			return err
		}
		for i := range networkConfig.NodeConfigs {
			targetDBDir := filepath.Join(filepath.Join(ln.rootDir, networkConfig.NodeConfigs[i].Name), defaultDBSubdir)
			networkConfig.NodeConfigs[i].Flags[config.DBPathKey] = targetDBDir
		}
	} else {
		// load db from full directory copies
		for _, nodeConfig := range networkConfig.NodeConfigs {
			sourceDBDir := filepath.Join(snapshotDBDir, nodeConfig.Name)
			targetDBDir := filepath.Join(filepath.Join(ln.rootDir, nodeConfig.Name), defaultDBSubdir)
			if err := dircopy.Copy(sourceDBDir, targetDBDir); err != nil {
				return fmt.Errorf("failure loading node %q db dir: %w", nodeConfig.Name, err)
			}
			nodeConfig.Flags[config.DBPathKey] = targetDBDir
		}
	}
	// replace binary path
	if binaryPath != "" {
		for i := range networkConfig.NodeConfigs {
			networkConfig.NodeConfigs[i].BinaryPath = binaryPath
		}
	}
	// set plugin dir
	resolvedPluginDir := pluginDir
	if resolvedPluginDir == "" {
		resolvedPluginDir = luxconfig.ResolvePluginDir()
	}
	for i := range networkConfig.NodeConfigs {
		networkConfig.NodeConfigs[i].Flags[config.PluginDirKey] = resolvedPluginDir
	}
	// add chain configs and upgrade configs
	for i := range networkConfig.NodeConfigs {
		if networkConfig.NodeConfigs[i].ChainConfigFiles == nil {
			networkConfig.NodeConfigs[i].ChainConfigFiles = map[string]string{}
		}
		if networkConfig.NodeConfigs[i].UpgradeConfigFiles == nil {
			networkConfig.NodeConfigs[i].UpgradeConfigFiles = map[string]string{}
		}
		if networkConfig.NodeConfigs[i].ChainConfigFiles == nil {
			networkConfig.NodeConfigs[i].ChainConfigFiles = map[string]string{}
		}
		for k, v := range chainConfigs {
			networkConfig.NodeConfigs[i].ChainConfigFiles[k] = v
		}
		for k, v := range upgradeConfigs {
			networkConfig.NodeConfigs[i].UpgradeConfigFiles[k] = v
		}
		for k, v := range chainConfigs {
			networkConfig.NodeConfigs[i].ChainConfigFiles[k] = v
		}
	}
	// load network state not available at blockchain db
	networkStateJSON, err := os.ReadFile(filepath.Join(snapshotDir, "state.json"))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("failure reading network state file from snapshot: %w", err)
		}
		ln.logger.Warn("network state file not found on snapshot")
	} else {
		networkState := NetworkState{}
		if err := json.Unmarshal(networkStateJSON, &networkState); err != nil {
			return fmt.Errorf("failure unmarshaling network state from snapshot: %w", err)
		}
		ln.chainID2ElasticChainID = map[ids.ID]ids.ID{}
		for chainIDStr, elasticChainIDStr := range networkState.ChainID2ElasticChainID {
			chainID, err := ids.FromString(chainIDStr)
			if err != nil {
				return err
			}
			elasticChainID, err := ids.FromString(elasticChainIDStr)
			if err != nil {
				return err
			}
			ln.chainID2ElasticChainID[chainID] = elasticChainID
		}
	}
	if err := ln.loadConfig(ctx, networkConfig); err != nil {
		return err
	}

	if hotManifest != nil {
		if err := ln.applyHotSnapshot(ctx, snapshotDir, networkConfig.NodeConfigs, hotManifest); err != nil {
			return err
		}
	}

	return nil
}

// SaveHotSnapshot saves a snapshot without stopping the network.
// Uses the admin snapshot API to stream a consistent backup while nodes continue serving.
func (ln *localNetwork) SaveHotSnapshot(ctx context.Context, snapshotName string) (string, error) {
	ln.lock.RLock() // Read lock - doesn't block network operations
	defer ln.lock.RUnlock()

	if ln.stopCalled() {
		return "", network.ErrStopped
	}
	if len(snapshotName) == 0 {
		return "", fmt.Errorf("invalid snapshotName %q", snapshotName)
	}
	snapshotDir := filepath.Join(ln.snapshotsDir, snapshotPrefix+snapshotName)
	networkName := constants.NetworkName(ln.networkID)
	snapshotExists := true
	if _, err := os.Stat(snapshotDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			snapshotExists = false
		} else {
			return "", fmt.Errorf("failed to access snapshot dir: %w", err)
		}
	}

	// Collect node info (read-only, safe during operation)
	nodesConfig := map[string]node.Config{}
	for nodeName, node := range ln.nodes {
		nodeConfig := node.config
		nodeConfig.Flags = maps.Clone(nodeConfig.Flags)
		nodesConfig[nodeName] = nodeConfig
	}

	// Preserve current node ports
	for nodeName, nodeConfig := range nodesConfig {
		nodeConfig.Flags[config.HTTPPortKey] = ln.nodes[nodeName].GetAPIPort()
		nodeConfig.Flags[config.StakingPortKey] = ln.nodes[nodeName].GetP2PPort()
	}

	// Make copy of network flags
	networkConfigFlags := maps.Clone(ln.flags)
	delete(networkConfigFlags, config.DataDirKey)
	delete(networkConfigFlags, config.LogsDirKey)
	for nodeName, nodeConfig := range nodesConfig {
		if nodeConfig.ConfigFile != "" {
			var err error
			nodeConfig.ConfigFile, err = utils.SetJSONKey(nodeConfig.ConfigFile, config.LogsDirKey, "")
			if err != nil {
				return "", err
			}
		}
		delete(nodeConfig.Flags, config.DataDirKey)
		delete(nodeConfig.Flags, config.LogsDirKey)
		nodesConfig[nodeName] = nodeConfig
	}

	// Create main snapshot dirs
	snapshotDBDir := filepath.Join(snapshotDir, defaultDBSubdir)
	if err := os.MkdirAll(snapshotDBDir, os.ModePerm); err != nil {
		return "", err
	}

	manifestPath := filepath.Join(snapshotDir, hotSnapshotManifestName)
	manifest, err := loadHotSnapshotManifest(manifestPath)
	if err != nil {
		return "", err
	}
	if manifest == nil {
		if snapshotExists {
			return "", fmt.Errorf("snapshot %q exists without hot manifest; choose a new name or remove it", snapshotName)
		}
		manifest = &hotSnapshotManifest{
			Version: hotSnapshotVersion,
			Network: networkName,
			Nodes:   map[string]hotSnapshotNode{},
		}
	} else if manifest.Network != networkName {
		return "", fmt.Errorf("hot snapshot network mismatch: got %q want %q", manifest.Network, networkName)
	}

	// Save network config
	networkConfig := network.Config{
		Genesis:            string(ln.genesis),
		Flags:              networkConfigFlags,
		NodeConfigs:        []node.Config{},
		BinaryPath:         ln.binaryPath,
		ChainConfigFiles:   ln.chainConfigFiles,
		GenesisConfigFiles: ln.genesisConfigFiles,
		UpgradeConfigFiles: ln.upgradeConfigFiles,
		PChainConfigFiles:  ln.pChainConfigFiles,
	}
	networkConfig.NodeConfigs = append(networkConfig.NodeConfigs, slices.Collect(maps.Values(nodesConfig))...)
	networkConfigJSON, err := json.MarshalIndent(networkConfig, "", "    ")
	if err != nil {
		return "", err
	}
	if err := createFileAndWrite(filepath.Join(snapshotDir, "network.json"), networkConfigJSON); err != nil {
		return "", err
	}

	// Save dynamic network state
	chainID2ElasticChainID := map[string]string{}
	for chainID, elasticChainID := range ln.chainID2ElasticChainID {
		chainID2ElasticChainID[chainID.String()] = elasticChainID.String()
	}
	networkState := NetworkState{
		ChainID2ElasticChainID: chainID2ElasticChainID,
	}
	networkStateJSON, err := json.MarshalIndent(networkState, "", "    ")
	if err != nil {
		return "", err
	}
	if err := createFileAndWrite(filepath.Join(snapshotDir, "state.json"), networkStateJSON); err != nil {
		return "", err
	}

	for _, nodeConfig := range nodesConfig {
		localNode, ok := ln.nodes[nodeConfig.Name]
		if !ok {
			return "", fmt.Errorf("node %q not found for hot snapshot", nodeConfig.Name)
		}
		adminClient := localNode.GetAPIClient().AdminAPI()
		if adminClient == nil {
			return "", fmt.Errorf("admin API client is nil for node %q", nodeConfig.Name)
		}

		nodeManifest := manifest.Nodes[nodeConfig.Name]
		since := nodeManifest.LastVersion

		nodeSnapshotDir := filepath.Join(snapshotDBDir, nodeConfig.Name, networkName, "backups")
		if err := os.MkdirAll(nodeSnapshotDir, os.ModePerm); err != nil {
			return "", fmt.Errorf("failed to create backup dir for node %q: %w", nodeConfig.Name, err)
		}

		timestamp := time.Now().UTC().Format("20060102T150405Z")
		backupFile := fmt.Sprintf("backup_since_%d_%s.bak", since, timestamp)
		backupPath := filepath.Join(nodeSnapshotDir, backupFile)

		version, err := requestAdminSnapshot(ctx, adminClient, backupPath, since)
		if err != nil {
			_ = os.Remove(backupPath)
			return "", fmt.Errorf("failed to snapshot node %q: %w", nodeConfig.Name, err)
		}

		relativePath := filepath.ToSlash(filepath.Join(defaultDBSubdir, nodeConfig.Name, networkName, "backups", backupFile))
		nodeManifest.Backups = append(nodeManifest.Backups, hotSnapshotBackup{
			Since:     since,
			Version:   version,
			Path:      relativePath,
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		})
		nodeManifest.LastVersion = version
		manifest.Nodes[nodeConfig.Name] = nodeManifest
	}

	if err := saveHotSnapshotManifest(manifestPath, manifest); err != nil {
		return "", err
	}

	ln.logger.Info("Hot snapshot saved", log.String("snapshot", snapshotName), log.String("path", snapshotDir))
	return snapshotDir, nil
}

// Remove network snapshot
func (ln *localNetwork) RemoveSnapshot(snapshotName string) error {
	snapshotDir := filepath.Join(ln.snapshotsDir, snapshotPrefix+snapshotName)
	_, err := os.Stat(snapshotDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrSnapshotNotFound
		} else {
			return fmt.Errorf("failure accessing snapshot %q: %w", snapshotName, err)
		}
	}
	if err := os.RemoveAll(snapshotDir); err != nil {
		return fmt.Errorf("failure removing snapshot path %q: %w", snapshotDir, err)
	}
	return nil
}

// Get network snapshots
func (ln *localNetwork) GetSnapshotNames() ([]string, error) {
	_, err := os.Stat(ln.snapshotsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("snapshots dir %q does not exists", ln.snapshotsDir)
		} else {
			return nil, fmt.Errorf("failure accessing snapshots dir %q: %w", ln.snapshotsDir, err)
		}
	}
	matches, err := filepath.Glob(filepath.Join(ln.snapshotsDir, snapshotPrefix+"*"))
	if err != nil {
		return nil, err
	}
	snapshots := []string{}
	for _, match := range matches {
		snapshots = append(snapshots, strings.TrimPrefix(filepath.Base(match), snapshotPrefix))
	}
	return snapshots, nil
}

func loadHotSnapshotManifest(path string) (*hotSnapshotManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read hot snapshot manifest: %w", err)
	}
	manifest := &hotSnapshotManifest{}
	if err := json.Unmarshal(data, manifest); err != nil {
		return nil, fmt.Errorf("failed to parse hot snapshot manifest: %w", err)
	}
	if manifest.Version != hotSnapshotVersion {
		return nil, fmt.Errorf("unsupported hot snapshot version %d", manifest.Version)
	}
	if manifest.Nodes == nil {
		manifest.Nodes = map[string]hotSnapshotNode{}
	}
	return manifest, nil
}

func saveHotSnapshotManifest(path string, manifest *hotSnapshotManifest) error {
	data, err := json.MarshalIndent(manifest, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal hot snapshot manifest: %w", err)
	}
	return createFileAndWrite(path, data)
}

func (ln *localNetwork) prepareHotSnapshotDirs(nodeConfigs []node.Config) error {
	for _, nodeConfig := range nodeConfigs {
		targetDBDir := filepath.Join(ln.rootDir, nodeConfig.Name, defaultDBSubdir)
		if err := os.RemoveAll(targetDBDir); err != nil {
			return fmt.Errorf("failed to clear db dir for node %q: %w", nodeConfig.Name, err)
		}
		if err := os.MkdirAll(targetDBDir, 0o750); err != nil {
			return fmt.Errorf("failed to create db dir for node %q: %w", nodeConfig.Name, err)
		}
	}
	return nil
}

func (ln *localNetwork) applyHotSnapshot(
	ctx context.Context,
	snapshotDir string,
	nodeConfigs []node.Config,
	manifest *hotSnapshotManifest,
) error {
	for _, nodeConfig := range nodeConfigs {
		nodeName := nodeConfig.Name
		nodeManifest, ok := manifest.Nodes[nodeName]
		if !ok || len(nodeManifest.Backups) == 0 {
			return fmt.Errorf("missing hot snapshot backups for node %q", nodeName)
		}

		localNode, ok := ln.nodes[nodeName]
		if !ok {
			return fmt.Errorf("node %q not found while applying hot snapshot", nodeName)
		}

		adminClient := localNode.GetAPIClient().AdminAPI()
		if adminClient == nil {
			return fmt.Errorf("admin API client is nil for node %q", nodeName)
		}

		backups := append([]hotSnapshotBackup(nil), nodeManifest.Backups...)
		sort.Slice(backups, func(i, j int) bool {
			return backups[i].Since < backups[j].Since
		})

		for _, backup := range backups {
			backupPath := filepath.Join(snapshotDir, filepath.FromSlash(backup.Path))
			if err := requestAdminLoad(ctx, adminClient, backupPath); err != nil {
				return fmt.Errorf("failed to load backup for node %q: %w", nodeName, err)
			}
		}
	}

	return nil
}

type adminSnapshotArgs struct {
	Path  string `json:"path"`
	Since uint64 `json:"since"`
}

type adminSnapshotReply struct {
	Success bool   `json:"success"`
	Version uint64 `json:"version"`
}

type adminLoadArgs struct {
	Path string `json:"path"`
}

func requestAdminSnapshot(ctx context.Context, adminClient *admin.Client, path string, since uint64) (uint64, error) {
	reply := &adminSnapshotReply{}
	if err := adminClient.Requester.SendRequest(ctx, "admin.snapshot", &adminSnapshotArgs{
		Path:  path,
		Since: since,
	}, reply); err != nil {
		return 0, err
	}
	return reply.Version, nil
}

func requestAdminLoad(ctx context.Context, adminClient *admin.Client, path string) error {
	return adminClient.Requester.SendRequest(ctx, "admin.load", &adminLoadArgs{
		Path: path,
	}, &struct{}{})
}
