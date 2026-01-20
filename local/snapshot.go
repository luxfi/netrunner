package local

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"maps"
	"slices"

	"github.com/klauspost/compress/zstd"
	luxconfig "github.com/luxfi/config"
	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	log "github.com/luxfi/log"
	"github.com/luxfi/netrunner/api"
	"github.com/luxfi/netrunner/network"
	"github.com/luxfi/netrunner/network/node"
	"github.com/luxfi/netrunner/utils"

	"github.com/luxfi/config"
	"github.com/luxfi/sdk/admin"
)

// NetworkState defines dynamic network information not available on chain db
type NetworkState struct {
	// Map from chain id to elastic chain tx id
	ChainID2ElasticChainID map[string]string `json:"chainID2ElasticChainID"`
}

const (
	// Manifest file name for native backup snapshots
	manifestFileName = "manifest.json"
	// Current manifest version
	manifestVersion = 2
)

// snapshotManifest stores metadata for native database backup snapshots.
// Supports incremental backups with version tracking.
type snapshotManifest struct {
	// Manifest format version
	Version int `json:"version"`
	// Network name (e.g., "mainnet", "testnet", "local")
	Network string `json:"network"`
	// Unix timestamp when snapshot was created
	Timestamp int64 `json:"timestamp"`
	// Human-readable timestamp
	CreatedAt string `json:"created_at"`
	// Per-node backup metadata
	Nodes map[string]nodeBackupInfo `json:"nodes"`
}

// nodeBackupInfo stores backup metadata for a single node.
type nodeBackupInfo struct {
	// Database version after backup
	DBVersion uint64 `json:"db_version"`
	// Version this backup is incremental from (0 = full backup)
	IncrementalFrom uint64 `json:"incremental_from"`
	// Path to compressed backup file (relative to snapshot dir)
	BackupFile string `json:"backup_file"`
	// Size of compressed backup in bytes
	CompressedSize int64 `json:"compressed_size"`
	// Temp path during backup (not serialized)
	tmpPath string `json:"-"`
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

// SaveSnapshot saves a network snapshot using native database backup.
// Network is stopped to ensure consistent backup.
// Supports incremental backups when a previous snapshot exists.
func (ln *localNetwork) SaveSnapshot(ctx context.Context, snapshotName string) (string, error) {
	ln.lock.Lock()
	defer ln.lock.Unlock()

	if ln.stopCalled() {
		return "", network.ErrStopped
	}
	if len(snapshotName) == 0 {
		return "", fmt.Errorf("invalid snapshotName %q", snapshotName)
	}

	snapshotDir := filepath.Join(ln.snapshotsDir, snapshotPrefix+snapshotName)
	networkName := constants.NetworkName(ln.networkID)

	// Check for existing snapshot to enable incremental backup
	var previousManifest *snapshotManifest
	if _, err := os.Stat(snapshotDir); err == nil {
		manifestPath := filepath.Join(snapshotDir, manifestFileName)
		previousManifest, err = loadManifest(manifestPath)
		if err != nil {
			return "", fmt.Errorf("snapshot %q exists but manifest is invalid: %w", snapshotName, err)
		}
		if previousManifest.Network != networkName {
			return "", fmt.Errorf("snapshot network mismatch: got %q want %q", previousManifest.Network, networkName)
		}
	}

	// Collect node info before stopping
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

	// Make copy of network flags, remove data/log dir references
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

	// Request backup from each node while still running
	nodeBackups := make(map[string]nodeBackupInfo)
	for _, nodeConfig := range nodesConfig {
		localNode, ok := ln.nodes[nodeConfig.Name]
		if !ok {
			return "", fmt.Errorf("node %q not found for snapshot", nodeConfig.Name)
		}

		adminClient := localNode.GetAPIClient().AdminAPI()
		if adminClient == nil {
			return "", fmt.Errorf("admin API client is nil for node %q", nodeConfig.Name)
		}

		// Determine incremental base version
		var since uint64
		if previousManifest != nil {
			if prevNode, ok := previousManifest.Nodes[nodeConfig.Name]; ok {
				since = prevNode.DBVersion
			}
		}

		// Create temp file for backup
		tmpBackupPath := filepath.Join(os.TempDir(), fmt.Sprintf("lux-backup-%s-%d.tmp", nodeConfig.Name, time.Now().UnixNano()))
		defer os.Remove(tmpBackupPath)

		// Request native backup via admin API
		version, err := requestAdminSnapshot(ctx, adminClient, tmpBackupPath, since)
		if err != nil {
			return "", fmt.Errorf("failed to backup node %q: %w", nodeConfig.Name, err)
		}

		nodeBackups[nodeConfig.Name] = nodeBackupInfo{
			DBVersion:       version,
			IncrementalFrom: since,
			BackupFile:      fmt.Sprintf("%s.backup.zst", nodeConfig.Name),
			tmpPath:         tmpBackupPath,
		}
	}

	// Stop network for consistent state
	if err := ln.stop(ctx); err != nil {
		return "", err
	}
	syncFilesystem()

	// Create snapshot directory
	if err := os.MkdirAll(snapshotDir, os.ModePerm); err != nil {
		return "", err
	}

	// Compress and write backup files
	for nodeName, backupInfo := range nodeBackups {
		destPath := filepath.Join(snapshotDir, backupInfo.BackupFile)
		size, err := compressFile(backupInfo.tmpPath, destPath)
		if err != nil {
			return "", fmt.Errorf("failed to compress backup for node %q: %w", nodeName, err)
		}
		backupInfo.CompressedSize = size
		backupInfo.tmpPath = "" // Clear temp path
		nodeBackups[nodeName] = backupInfo
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

	// Save network state
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

	// Save manifest
	manifest := &snapshotManifest{
		Version:   manifestVersion,
		Network:   networkName,
		Timestamp: time.Now().Unix(),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Nodes:     nodeBackups,
	}
	if err := saveManifest(filepath.Join(snapshotDir, manifestFileName), manifest); err != nil {
		return "", err
	}

	ln.logger.Info("Snapshot saved",
		log.String("snapshot", snapshotName),
		log.String("path", snapshotDir),
		log.Int("nodes", len(nodeBackups)),
	)

	return snapshotDir, nil
}

// loadSnapshot restores network from a snapshot using native database restore.
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
	_, err := os.Stat(snapshotDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrSnapshotNotFound
		}
		return fmt.Errorf("failure accessing snapshot %q: %w", snapshotName, err)
	}

	// Load manifest to determine snapshot format
	manifestPath := filepath.Join(snapshotDir, manifestFileName)
	manifest, err := loadManifest(manifestPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to load manifest: %w", err)
	}

	// Load network config
	networkConfigJSON, err := os.ReadFile(filepath.Join(snapshotDir, "network.json"))
	if err != nil {
		return fmt.Errorf("failure reading network config file from snapshot: %w", err)
	}
	networkConfig := network.Config{}
	if err := json.Unmarshal(networkConfigJSON, &networkConfig); err != nil {
		return fmt.Errorf("failure unmarshaling network config from snapshot: %w", err)
	}

	// Add flags
	for i := range networkConfig.NodeConfigs {
		for k, v := range flags {
			networkConfig.NodeConfigs[i].Flags[k] = v
		}
	}

	// Prepare DB directories and restore backups
	if manifest != nil {
		// Native backup format - decompress and prepare for admin.load
		if err := ln.prepareNativeBackupDirs(networkConfig.NodeConfigs); err != nil {
			return err
		}
		for i := range networkConfig.NodeConfigs {
			targetDBDir := filepath.Join(filepath.Join(ln.rootDir, networkConfig.NodeConfigs[i].Name), defaultDBSubdir)
			networkConfig.NodeConfigs[i].Flags[config.DBPathKey] = targetDBDir
		}
	} else {
		// Legacy format - check for old hot snapshot manifest
		hotManifestPath := filepath.Join(snapshotDir, "hot_snapshot.json")
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
			// Legacy directory copy format
			snapshotDBDir := filepath.Join(snapshotDir, defaultDBSubdir)
			for _, nodeConfig := range networkConfig.NodeConfigs {
				sourceDBDir := filepath.Join(snapshotDBDir, nodeConfig.Name)
				targetDBDir := filepath.Join(filepath.Join(ln.rootDir, nodeConfig.Name), defaultDBSubdir)
				if err := copyDir(sourceDBDir, targetDBDir); err != nil {
					return fmt.Errorf("failure loading node %q db dir: %w", nodeConfig.Name, err)
				}
				nodeConfig.Flags[config.DBPathKey] = targetDBDir
			}
		}
	}

	// Replace binary path
	if binaryPath != "" {
		for i := range networkConfig.NodeConfigs {
			networkConfig.NodeConfigs[i].BinaryPath = binaryPath
		}
	}

	// Set plugin dir
	resolvedPluginDir := pluginDir
	if resolvedPluginDir == "" {
		resolvedPluginDir = luxconfig.ResolvePluginDir()
	}
	for i := range networkConfig.NodeConfigs {
		networkConfig.NodeConfigs[i].Flags[config.PluginDirKey] = resolvedPluginDir
	}

	// Add chain configs and upgrade configs
	for i := range networkConfig.NodeConfigs {
		if networkConfig.NodeConfigs[i].ChainConfigFiles == nil {
			networkConfig.NodeConfigs[i].ChainConfigFiles = map[string]string{}
		}
		if networkConfig.NodeConfigs[i].UpgradeConfigFiles == nil {
			networkConfig.NodeConfigs[i].UpgradeConfigFiles = map[string]string{}
		}
		for k, v := range chainConfigs {
			networkConfig.NodeConfigs[i].ChainConfigFiles[k] = v
		}
		for k, v := range upgradeConfigs {
			networkConfig.NodeConfigs[i].UpgradeConfigFiles[k] = v
		}
	}

	// Load network state
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

	// Load network config (starts nodes)
	if err := ln.loadConfig(ctx, networkConfig); err != nil {
		return err
	}

	// Apply native backups after nodes start
	if manifest != nil {
		if err := ln.applyNativeBackups(ctx, snapshotDir, networkConfig.NodeConfigs, manifest); err != nil {
			return err
		}
	} else {
		// Check for legacy hot snapshot
		hotManifestPath := filepath.Join(snapshotDir, "hot_snapshot.json")
		hotManifest, _ := loadHotSnapshotManifest(hotManifestPath)
		if hotManifest != nil {
			if err := ln.applyHotSnapshot(ctx, snapshotDir, networkConfig.NodeConfigs, hotManifest); err != nil {
				return err
			}
		}
	}

	return nil
}

// SaveHotSnapshot saves a snapshot without stopping the network.
// Uses native database backup API for consistent snapshots.
func (ln *localNetwork) SaveHotSnapshot(ctx context.Context, snapshotName string) (string, error) {
	ln.lock.RLock()
	defer ln.lock.RUnlock()

	if ln.stopCalled() {
		return "", network.ErrStopped
	}
	if len(snapshotName) == 0 {
		return "", fmt.Errorf("invalid snapshotName %q", snapshotName)
	}

	snapshotDir := filepath.Join(ln.snapshotsDir, snapshotPrefix+snapshotName)
	networkName := constants.NetworkName(ln.networkID)

	// Check for existing snapshot
	var previousManifest *snapshotManifest
	if _, err := os.Stat(snapshotDir); err == nil {
		manifestPath := filepath.Join(snapshotDir, manifestFileName)
		previousManifest, err = loadManifest(manifestPath)
		if err != nil {
			return "", fmt.Errorf("snapshot %q exists but manifest is invalid: %w", snapshotName, err)
		}
		if previousManifest.Network != networkName {
			return "", fmt.Errorf("snapshot network mismatch: got %q want %q", previousManifest.Network, networkName)
		}
	}

	// Collect node info
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

	// Create snapshot directory
	if err := os.MkdirAll(snapshotDir, os.ModePerm); err != nil {
		return "", err
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

	// Save network state
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

	// Request backup from each node and compress
	nodeBackups := make(map[string]nodeBackupInfo)
	for _, nodeConfig := range nodesConfig {
		localNode, ok := ln.nodes[nodeConfig.Name]
		if !ok {
			return "", fmt.Errorf("node %q not found for hot snapshot", nodeConfig.Name)
		}

		adminClient := localNode.GetAPIClient().AdminAPI()
		if adminClient == nil {
			return "", fmt.Errorf("admin API client is nil for node %q", nodeConfig.Name)
		}

		// Determine incremental base version
		var since uint64
		if previousManifest != nil {
			if prevNode, ok := previousManifest.Nodes[nodeConfig.Name]; ok {
				since = prevNode.DBVersion
			}
		}

		// Create temp file for backup
		tmpBackupPath := filepath.Join(os.TempDir(), fmt.Sprintf("lux-backup-%s-%d.tmp", nodeConfig.Name, time.Now().UnixNano()))

		// Request native backup via admin API
		version, err := requestAdminSnapshot(ctx, adminClient, tmpBackupPath, since)
		if err != nil {
			os.Remove(tmpBackupPath)
			return "", fmt.Errorf("failed to backup node %q: %w", nodeConfig.Name, err)
		}

		// Compress backup
		backupFileName := fmt.Sprintf("%s.backup.zst", nodeConfig.Name)
		destPath := filepath.Join(snapshotDir, backupFileName)
		size, err := compressFile(tmpBackupPath, destPath)
		os.Remove(tmpBackupPath)
		if err != nil {
			return "", fmt.Errorf("failed to compress backup for node %q: %w", nodeConfig.Name, err)
		}

		nodeBackups[nodeConfig.Name] = nodeBackupInfo{
			DBVersion:       version,
			IncrementalFrom: since,
			BackupFile:      backupFileName,
			CompressedSize:  size,
		}
	}

	// Save manifest
	manifest := &snapshotManifest{
		Version:   manifestVersion,
		Network:   networkName,
		Timestamp: time.Now().Unix(),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Nodes:     nodeBackups,
	}
	if err := saveManifest(filepath.Join(snapshotDir, manifestFileName), manifest); err != nil {
		return "", err
	}

	ln.logger.Info("Hot snapshot saved",
		log.String("snapshot", snapshotName),
		log.String("path", snapshotDir),
		log.Int("nodes", len(nodeBackups)),
	)

	return snapshotDir, nil
}

// RemoveSnapshot removes a network snapshot
func (ln *localNetwork) RemoveSnapshot(snapshotName string) error {
	snapshotDir := filepath.Join(ln.snapshotsDir, snapshotPrefix+snapshotName)
	_, err := os.Stat(snapshotDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrSnapshotNotFound
		}
		return fmt.Errorf("failure accessing snapshot %q: %w", snapshotName, err)
	}
	if err := os.RemoveAll(snapshotDir); err != nil {
		return fmt.Errorf("failure removing snapshot path %q: %w", snapshotDir, err)
	}
	return nil
}

// GetSnapshotNames returns all snapshot names
func (ln *localNetwork) GetSnapshotNames() ([]string, error) {
	_, err := os.Stat(ln.snapshotsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("snapshots dir %q does not exists", ln.snapshotsDir)
		}
		return nil, fmt.Errorf("failure accessing snapshots dir %q: %w", ln.snapshotsDir, err)
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

// prepareNativeBackupDirs creates empty DB directories for native backup restore
func (ln *localNetwork) prepareNativeBackupDirs(nodeConfigs []node.Config) error {
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

// applyNativeBackups decompresses and loads native database backups
func (ln *localNetwork) applyNativeBackups(
	ctx context.Context,
	snapshotDir string,
	nodeConfigs []node.Config,
	manifest *snapshotManifest,
) error {
	for _, nodeConfig := range nodeConfigs {
		nodeName := nodeConfig.Name
		nodeManifest, ok := manifest.Nodes[nodeName]
		if !ok {
			return fmt.Errorf("missing backup metadata for node %q", nodeName)
		}

		localNode, ok := ln.nodes[nodeName]
		if !ok {
			return fmt.Errorf("node %q not found while applying backup", nodeName)
		}

		adminClient := localNode.GetAPIClient().AdminAPI()
		if adminClient == nil {
			return fmt.Errorf("admin API client is nil for node %q", nodeName)
		}

		// Decompress backup to temp file
		compressedPath := filepath.Join(snapshotDir, nodeManifest.BackupFile)
		tmpPath := filepath.Join(os.TempDir(), fmt.Sprintf("lux-restore-%s-%d.tmp", nodeName, time.Now().UnixNano()))
		if err := decompressFile(compressedPath, tmpPath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("failed to decompress backup for node %q: %w", nodeName, err)
		}

		// Load backup via admin API
		if err := requestAdminLoad(ctx, adminClient, tmpPath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("failed to load backup for node %q: %w", nodeName, err)
		}
		os.Remove(tmpPath)
	}
	return nil
}

// loadManifest loads a snapshot manifest from disk
func loadManifest(path string) (*snapshotManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}
	manifest := &snapshotManifest{}
	if err := json.Unmarshal(data, manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}
	if manifest.Version > manifestVersion {
		return nil, fmt.Errorf("unsupported manifest version %d (max supported: %d)", manifest.Version, manifestVersion)
	}
	if manifest.Nodes == nil {
		manifest.Nodes = map[string]nodeBackupInfo{}
	}
	return manifest, nil
}

// saveManifest writes a snapshot manifest to disk
func saveManifest(path string, manifest *snapshotManifest) error {
	data, err := json.MarshalIndent(manifest, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}
	return createFileAndWrite(path, data)
}

// compressFile compresses src to dst using zstd, returns compressed size
func compressFile(src, dst string) (int64, error) {
	srcFile, err := os.Open(src)
	if err != nil {
		return 0, fmt.Errorf("failed to open source: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return 0, fmt.Errorf("failed to create destination: %w", err)
	}
	defer dstFile.Close()

	encoder, err := zstd.NewWriter(dstFile, zstd.WithEncoderLevel(zstd.SpeedBetterCompression))
	if err != nil {
		return 0, fmt.Errorf("failed to create zstd encoder: %w", err)
	}

	if _, err := io.Copy(encoder, srcFile); err != nil {
		encoder.Close()
		return 0, fmt.Errorf("failed to compress: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return 0, fmt.Errorf("failed to finalize compression: %w", err)
	}

	stat, err := dstFile.Stat()
	if err != nil {
		return 0, fmt.Errorf("failed to stat compressed file: %w", err)
	}
	return stat.Size(), nil
}

// decompressFile decompresses src to dst using zstd
func decompressFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source: %w", err)
	}
	defer srcFile.Close()

	decoder, err := zstd.NewReader(srcFile)
	if err != nil {
		return fmt.Errorf("failed to create zstd decoder: %w", err)
	}
	defer decoder.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination: %w", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, decoder); err != nil {
		return fmt.Errorf("failed to decompress: %w", err)
	}
	return nil
}

// copyDir copies a directory recursively (legacy support)
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		if err := os.MkdirAll(filepath.Dir(dstPath), 0o750); err != nil {
			return err
		}

		dstFile, err := os.Create(dstPath)
		if err != nil {
			return err
		}
		defer dstFile.Close()

		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}

// Legacy hot snapshot support for backwards compatibility

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

// Admin API request types and helpers

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
