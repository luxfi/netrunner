// Copyright (C) 2021-2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

//go:build !grpc

// Package rpcpb holds the netrunner control-plane request/response types.
//
// Default build: hand-rolled plain Go structs with json: tags. No
// google.golang.org/protobuf, no protoimpl reflection — these are pure
// data containers that ride inside a ZAP envelope (see netrunner/zaprpc).
//
// Legacy build (-tags grpc): rpc.pb.go provides the protoc-gen-go version
// of the same names. Either file compiles, never both.
package rpcpb

// =============================================================================
// Cluster + shared info
// =============================================================================

// ClusterInfo describes a running netrunner cluster.
type ClusterInfo struct {
	NodeNames           []string                           `json:"nodeNames,omitempty"`
	NodeInfos           map[string]*NodeInfo               `json:"nodeInfos,omitempty"`
	Pid                 int32                              `json:"pid,omitempty"`
	RootDataDir         string                             `json:"rootDataDir,omitempty"`
	Healthy             bool                               `json:"healthy,omitempty"`
	AttachedPeerInfos   map[string]*ListOfAttachedPeerInfo `json:"attachedPeerInfos,omitempty"`
	CustomChainsHealthy bool                               `json:"customChainsHealthy,omitempty"`
	CustomChains        map[string]*CustomChainInfo        `json:"customChains,omitempty"`
	Chains              map[string]*ChainInfo              `json:"chains,omitempty"`
}

func (m *ClusterInfo) GetClusterInfo() *ClusterInfo { return m }
func (m *ClusterInfo) GetNodeNames() []string {
	if m == nil {
		return nil
	}
	return m.NodeNames
}
func (m *ClusterInfo) GetNodeInfos() map[string]*NodeInfo {
	if m == nil {
		return nil
	}
	return m.NodeInfos
}
func (m *ClusterInfo) GetPid() int32 {
	if m == nil {
		return 0
	}
	return m.Pid
}
func (m *ClusterInfo) GetRootDataDir() string {
	if m == nil {
		return ""
	}
	return m.RootDataDir
}
func (m *ClusterInfo) GetHealthy() bool {
	if m == nil {
		return false
	}
	return m.Healthy
}
func (m *ClusterInfo) GetAttachedPeerInfos() map[string]*ListOfAttachedPeerInfo {
	if m == nil {
		return nil
	}
	return m.AttachedPeerInfos
}
func (m *ClusterInfo) GetCustomChainsHealthy() bool {
	if m == nil {
		return false
	}
	return m.CustomChainsHealthy
}
func (m *ClusterInfo) GetCustomChains() map[string]*CustomChainInfo {
	if m == nil {
		return nil
	}
	return m.CustomChains
}
func (m *ClusterInfo) GetChains() map[string]*ChainInfo {
	if m == nil {
		return nil
	}
	return m.Chains
}

// NodeInfo describes a single node inside a cluster.
type NodeInfo struct {
	Name              string `json:"name,omitempty"`
	ExecPath          string `json:"execPath,omitempty"`
	Uri               string `json:"uri,omitempty"`
	Id                string `json:"id,omitempty"`
	LogDir            string `json:"logDir,omitempty"`
	DbDir             string `json:"dbDir,omitempty"`
	PluginDir         string `json:"pluginDir,omitempty"`
	WhitelistedChains string `json:"whitelistedChains,omitempty"`
	Config            []byte `json:"config,omitempty"`
	Paused            bool   `json:"paused,omitempty"`
}

func (m *NodeInfo) GetName() string {
	if m == nil {
		return ""
	}
	return m.Name
}
func (m *NodeInfo) GetExecPath() string {
	if m == nil {
		return ""
	}
	return m.ExecPath
}
func (m *NodeInfo) GetUri() string {
	if m == nil {
		return ""
	}
	return m.Uri
}
func (m *NodeInfo) GetId() string {
	if m == nil {
		return ""
	}
	return m.Id
}
func (m *NodeInfo) GetLogDir() string {
	if m == nil {
		return ""
	}
	return m.LogDir
}
func (m *NodeInfo) GetDbDir() string {
	if m == nil {
		return ""
	}
	return m.DbDir
}
func (m *NodeInfo) GetPluginDir() string {
	if m == nil {
		return ""
	}
	return m.PluginDir
}
func (m *NodeInfo) GetWhitelistedChains() string {
	if m == nil {
		return ""
	}
	return m.WhitelistedChains
}
func (m *NodeInfo) GetConfig() []byte {
	if m == nil {
		return nil
	}
	return m.Config
}
func (m *NodeInfo) GetPaused() bool {
	if m == nil {
		return false
	}
	return m.Paused
}

type ChainInfo struct {
	IsElastic         bool               `json:"isElastic,omitempty"`
	ElasticChainId    string             `json:"elasticChainId,omitempty"`
	ChainParticipants *ChainParticipants `json:"chainParticipants,omitempty"`
}

func (m *ChainInfo) GetIsElastic() bool {
	if m == nil {
		return false
	}
	return m.IsElastic
}
func (m *ChainInfo) GetElasticChainId() string {
	if m == nil {
		return ""
	}
	return m.ElasticChainId
}
func (m *ChainInfo) GetChainParticipants() *ChainParticipants {
	if m == nil {
		return nil
	}
	return m.ChainParticipants
}

type ChainParticipants struct {
	NodeNames []string `json:"nodeNames,omitempty"`
}

func (m *ChainParticipants) GetNodeNames() []string {
	if m == nil {
		return nil
	}
	return m.NodeNames
}

type CustomChainInfo struct {
	ChainName    string `json:"chainName,omitempty"`
	VmId         string `json:"vmId,omitempty"`
	PchainId     string `json:"pchainId,omitempty"`
	BlockchainId string `json:"blockchainId,omitempty"`
}

func (m *CustomChainInfo) GetChainName() string {
	if m == nil {
		return ""
	}
	return m.ChainName
}
func (m *CustomChainInfo) GetVmId() string {
	if m == nil {
		return ""
	}
	return m.VmId
}
func (m *CustomChainInfo) GetPchainId() string {
	if m == nil {
		return ""
	}
	return m.PchainId
}
func (m *CustomChainInfo) GetBlockchainId() string {
	if m == nil {
		return ""
	}
	return m.BlockchainId
}

type AttachedPeerInfo struct {
	Id string `json:"id,omitempty"`
}

func (m *AttachedPeerInfo) GetId() string {
	if m == nil {
		return ""
	}
	return m.Id
}

type ListOfAttachedPeerInfo struct {
	Peers []*AttachedPeerInfo `json:"peers,omitempty"`
}

func (m *ListOfAttachedPeerInfo) GetPeers() []*AttachedPeerInfo {
	if m == nil {
		return nil
	}
	return m.Peers
}

type NetworkHealth struct {
	NetworkName    string   `json:"networkName,omitempty"`
	NetworkId      uint32   `json:"networkId,omitempty"`
	Healthy        bool     `json:"healthy,omitempty"`
	HealthyNodes   []string `json:"healthyNodes,omitempty"`
	UnhealthyNodes []string `json:"unhealthyNodes,omitempty"`
}

type NetworkStatus struct {
	NetworkName string       `json:"networkName,omitempty"`
	NetworkId   uint32       `json:"networkId,omitempty"`
	Status      string       `json:"status,omitempty"`
	NumNodes    uint32       `json:"numNodes,omitempty"`
	Nodes       []*NodeInfo  `json:"nodes,omitempty"`
	Chains      []*ChainInfo `json:"chains,omitempty"`
}

// =============================================================================
// Specs
// =============================================================================

type ChainSpec struct {
	Participants    []string `json:"participants,omitempty"`
	ChainConfigFile string   `json:"chainConfigFile,omitempty"`
}

func (m *ChainSpec) GetParticipants() []string {
	if m == nil {
		return nil
	}
	return m.Participants
}
func (m *ChainSpec) GetChainConfigFile() string {
	if m == nil {
		return ""
	}
	return m.ChainConfigFile
}

type BlockchainSpec struct {
	VmName             string     `json:"vmName,omitempty"`
	Genesis            string     `json:"genesis,omitempty"`
	ChainId            *string    `json:"chainId,omitempty"`
	ChainSpec          *ChainSpec `json:"chainSpec,omitempty"`
	ChainConfig        string     `json:"chainConfig,omitempty"`
	NetworkUpgrade     string     `json:"networkUpgrade,omitempty"`
	BlockchainAlias    string     `json:"blockchainAlias,omitempty"`
	PerNodeChainConfig string     `json:"perNodeChainConfig,omitempty"`
}

func (m *BlockchainSpec) GetVmName() string {
	if m == nil {
		return ""
	}
	return m.VmName
}
func (m *BlockchainSpec) GetGenesis() string {
	if m == nil {
		return ""
	}
	return m.Genesis
}
func (m *BlockchainSpec) GetChainId() string {
	if m == nil || m.ChainId == nil {
		return ""
	}
	return *m.ChainId
}
func (m *BlockchainSpec) GetChainSpec() *ChainSpec {
	if m == nil {
		return nil
	}
	return m.ChainSpec
}
func (m *BlockchainSpec) GetChainConfig() string {
	if m == nil {
		return ""
	}
	return m.ChainConfig
}
func (m *BlockchainSpec) GetNetworkUpgrade() string {
	if m == nil {
		return ""
	}
	return m.NetworkUpgrade
}
func (m *BlockchainSpec) GetBlockchainAlias() string {
	if m == nil {
		return ""
	}
	return m.BlockchainAlias
}
func (m *BlockchainSpec) GetPerNodeChainConfig() string {
	if m == nil {
		return ""
	}
	return m.PerNodeChainConfig
}

type ElasticChainSpec struct {
	ChainId                  string `json:"chainId,omitempty"`
	AssetName                string `json:"assetName,omitempty"`
	AssetSymbol              string `json:"assetSymbol,omitempty"`
	InitialSupply            uint64 `json:"initialSupply,omitempty"`
	MaxSupply                uint64 `json:"maxSupply,omitempty"`
	MinConsumptionRate       uint64 `json:"minConsumptionRate,omitempty"`
	MaxConsumptionRate       uint64 `json:"maxConsumptionRate,omitempty"`
	MinValidatorStake        uint64 `json:"minValidatorStake,omitempty"`
	MaxValidatorStake        uint64 `json:"maxValidatorStake,omitempty"`
	MinStakeDuration         uint64 `json:"minStakeDuration,omitempty"`
	MaxStakeDuration         uint64 `json:"maxStakeDuration,omitempty"`
	MinDelegationFee         uint32 `json:"minDelegationFee,omitempty"`
	MinDelegatorStake        uint64 `json:"minDelegatorStake,omitempty"`
	MaxValidatorWeightFactor uint32 `json:"maxValidatorWeightFactor,omitempty"`
	UptimeRequirement        uint32 `json:"uptimeRequirement,omitempty"`
}

func (m *ElasticChainSpec) GetChainId() string {
	if m == nil {
		return ""
	}
	return m.ChainId
}

type PermissionlessValidatorSpec struct {
	ChainId           string `json:"chainId,omitempty"`
	NodeName          string `json:"nodeName,omitempty"`
	StakedTokenAmount uint64 `json:"stakedTokenAmount,omitempty"`
	AssetId           string `json:"assetId,omitempty"`
	StartTime         string `json:"startTime,omitempty"`
	StakeDuration     uint64 `json:"stakeDuration,omitempty"`
}

type RemoveChainValidatorSpec struct {
	ChainId   string   `json:"chainId,omitempty"`
	NodeNames []string `json:"nodeNames,omitempty"`
}

func (m *RemoveChainValidatorSpec) GetChainId() string {
	if m == nil {
		return ""
	}
	return m.ChainId
}
func (m *RemoveChainValidatorSpec) GetNodeNames() []string {
	if m == nil {
		return nil
	}
	return m.NodeNames
}

// =============================================================================
// Ping / RPCVersion
// =============================================================================

type PingRequest struct{}

type PingResponse struct {
	Pid int32 `json:"pid,omitempty"`
}

func (m *PingResponse) GetPid() int32 {
	if m == nil {
		return 0
	}
	return m.Pid
}

type RPCVersionRequest struct{}

type RPCVersionResponse struct {
	Version uint32 `json:"version,omitempty"`
}

func (m *RPCVersionResponse) GetVersion() uint32 {
	if m == nil {
		return 0
	}
	return m.Version
}

// =============================================================================
// Start
// =============================================================================

type StartRequest struct {
	NetworkName         string            `json:"networkName,omitempty"`
	NodeType            string            `json:"nodeType,omitempty"`
	ExecPath            string            `json:"execPath,omitempty"`
	NumNodes            *uint32           `json:"numNodes,omitempty"`
	WhitelistedChains   *string           `json:"whitelistedChains,omitempty"`
	GlobalNodeConfig    *string           `json:"globalNodeConfig,omitempty"`
	RootDataDir         *string           `json:"rootDataDir,omitempty"`
	PluginDir           string            `json:"pluginDir,omitempty"`
	BlockchainSpecs     []*BlockchainSpec `json:"blockchainSpecs,omitempty"`
	CustomNodeConfigs   map[string]string `json:"customNodeConfigs,omitempty"`
	ChainConfigs        map[string]string `json:"chainConfigs,omitempty"`
	UpgradeConfigs      map[string]string `json:"upgradeConfigs,omitempty"`
	ReassignPortsIfUsed *bool             `json:"reassignPortsIfUsed,omitempty"`
	DynamicPorts        *bool             `json:"dynamicPorts,omitempty"`
	ChainConfigFiles    map[string]string `json:"chainConfigFiles,omitempty"`
}

func (m *StartRequest) GetNetworkName() string {
	if m == nil {
		return ""
	}
	return m.NetworkName
}
func (m *StartRequest) GetNodeType() string {
	if m == nil {
		return ""
	}
	return m.NodeType
}
func (m *StartRequest) GetExecPath() string {
	if m == nil {
		return ""
	}
	return m.ExecPath
}
func (m *StartRequest) GetNumNodes() uint32 {
	if m == nil || m.NumNodes == nil {
		return 0
	}
	return *m.NumNodes
}
func (m *StartRequest) GetWhitelistedChains() string {
	if m == nil || m.WhitelistedChains == nil {
		return ""
	}
	return *m.WhitelistedChains
}
func (m *StartRequest) GetGlobalNodeConfig() string {
	if m == nil || m.GlobalNodeConfig == nil {
		return ""
	}
	return *m.GlobalNodeConfig
}
func (m *StartRequest) GetRootDataDir() string {
	if m == nil || m.RootDataDir == nil {
		return ""
	}
	return *m.RootDataDir
}
func (m *StartRequest) GetPluginDir() string {
	if m == nil {
		return ""
	}
	return m.PluginDir
}
func (m *StartRequest) GetBlockchainSpecs() []*BlockchainSpec {
	if m == nil {
		return nil
	}
	return m.BlockchainSpecs
}
func (m *StartRequest) GetCustomNodeConfigs() map[string]string {
	if m == nil {
		return nil
	}
	return m.CustomNodeConfigs
}
func (m *StartRequest) GetChainConfigs() map[string]string {
	if m == nil {
		return nil
	}
	return m.ChainConfigs
}
func (m *StartRequest) GetUpgradeConfigs() map[string]string {
	if m == nil {
		return nil
	}
	return m.UpgradeConfigs
}
func (m *StartRequest) GetChainConfigFiles() map[string]string {
	if m == nil {
		return nil
	}
	return m.ChainConfigFiles
}
func (m *StartRequest) GetReassignPortsIfUsed() bool {
	if m == nil || m.ReassignPortsIfUsed == nil {
		return false
	}
	return *m.ReassignPortsIfUsed
}
func (m *StartRequest) GetDynamicPorts() bool {
	if m == nil || m.DynamicPorts == nil {
		return false
	}
	return *m.DynamicPorts
}

type StartResponse struct {
	ClusterInfo *ClusterInfo `json:"clusterInfo,omitempty"`
	ChainIds    []string     `json:"chainIds,omitempty"`
}

// =============================================================================
// CreateBlockchains / CreateChains
// =============================================================================

type CreateBlockchainsRequest struct {
	BlockchainSpecs []*BlockchainSpec `json:"blockchainSpecs,omitempty"`
}

func (m *CreateBlockchainsRequest) GetBlockchainSpecs() []*BlockchainSpec {
	if m == nil {
		return nil
	}
	return m.BlockchainSpecs
}

type CreateBlockchainsResponse struct {
	ClusterInfo *ClusterInfo `json:"clusterInfo,omitempty"`
	ChainIds    []string     `json:"chainIds,omitempty"`
}

type CreateChainsRequest struct {
	ChainSpecs []*ChainSpec `json:"chainSpecs,omitempty"`
}

func (m *CreateChainsRequest) GetChainSpecs() []*ChainSpec {
	if m == nil {
		return nil
	}
	return m.ChainSpecs
}

type CreateChainsResponse struct {
	ClusterInfo *ClusterInfo `json:"clusterInfo,omitempty"`
	ChainIds    []string     `json:"chainIds,omitempty"`
}

// =============================================================================
// Elastic chain / validator
// =============================================================================

type TransformElasticChainsRequest struct {
	ElasticChainSpec []*ElasticChainSpec `json:"elasticChainSpec,omitempty"`
}

func (m *TransformElasticChainsRequest) GetElasticChainSpec() []*ElasticChainSpec {
	if m == nil {
		return nil
	}
	return m.ElasticChainSpec
}

type TransformElasticChainsResponse struct {
	ClusterInfo *ClusterInfo `json:"clusterInfo,omitempty"`
	TxIds       []string     `json:"txIds,omitempty"`
	AssetIds    []string     `json:"assetIds,omitempty"`
}

type AddPermissionlessValidatorRequest struct {
	ValidatorSpec []*PermissionlessValidatorSpec `json:"validatorSpec,omitempty"`
}

func (m *AddPermissionlessValidatorRequest) GetValidatorSpec() []*PermissionlessValidatorSpec {
	if m == nil {
		return nil
	}
	return m.ValidatorSpec
}

type AddPermissionlessValidatorResponse struct {
	ClusterInfo *ClusterInfo `json:"clusterInfo,omitempty"`
}

type RemoveChainValidatorRequest struct {
	ValidatorSpec []*RemoveChainValidatorSpec `json:"validatorSpec,omitempty"`
}

func (m *RemoveChainValidatorRequest) GetValidatorSpec() []*RemoveChainValidatorSpec {
	if m == nil {
		return nil
	}
	return m.ValidatorSpec
}

type RemoveChainValidatorResponse struct {
	ClusterInfo *ClusterInfo `json:"clusterInfo,omitempty"`
}

// =============================================================================
// Health / URIs / WaitForHealthy / Status / StreamStatus
// =============================================================================

type HealthRequest struct {
	NetworkName *string `json:"networkName,omitempty"`
}

type HealthResponse struct {
	ClusterInfo   *ClusterInfo   `json:"clusterInfo,omitempty"`
	NetworkHealth *NetworkHealth `json:"networkHealth,omitempty"`
}

type URIsRequest struct{}

type URIsResponse struct {
	Uris []string `json:"uris,omitempty"`
}

type WaitForHealthyRequest struct {
	NetworkName *string `json:"networkName,omitempty"`
}

type WaitForHealthyResponse struct {
	ClusterInfo *ClusterInfo `json:"clusterInfo,omitempty"`
}

type StatusRequest struct {
	NetworkName *string `json:"networkName,omitempty"`
}

type StatusResponse struct {
	ClusterInfo   *ClusterInfo   `json:"clusterInfo,omitempty"`
	NetworkStatus *NetworkStatus `json:"networkStatus,omitempty"`
}

type StreamStatusRequest struct {
	PushInterval int64 `json:"pushInterval,omitempty"`
}

type StreamStatusResponse struct {
	ClusterInfo *ClusterInfo `json:"clusterInfo,omitempty"`
}

// =============================================================================
// Node lifecycle: Add/Remove/Restart/Pause/Resume/Stop
// =============================================================================

type AddNodeRequest struct {
	Name             string            `json:"name,omitempty"`
	ExecPath         string            `json:"execPath,omitempty"`
	NodeConfig       *string           `json:"nodeConfig,omitempty"`
	ChainConfigs     map[string]string `json:"chainConfigs,omitempty"`
	UpgradeConfigs   map[string]string `json:"upgradeConfigs,omitempty"`
	ChainConfigFiles map[string]string `json:"chainConfigFiles,omitempty"`
	PluginDir        string            `json:"pluginDir,omitempty"`
}

func (m *AddNodeRequest) GetName() string {
	if m == nil {
		return ""
	}
	return m.Name
}
func (m *AddNodeRequest) GetExecPath() string {
	if m == nil {
		return ""
	}
	return m.ExecPath
}
func (m *AddNodeRequest) GetNodeConfig() string {
	if m == nil || m.NodeConfig == nil {
		return ""
	}
	return *m.NodeConfig
}
func (m *AddNodeRequest) GetChainConfigs() map[string]string {
	if m == nil {
		return nil
	}
	return m.ChainConfigs
}
func (m *AddNodeRequest) GetUpgradeConfigs() map[string]string {
	if m == nil {
		return nil
	}
	return m.UpgradeConfigs
}
func (m *AddNodeRequest) GetChainConfigFiles() map[string]string {
	if m == nil {
		return nil
	}
	return m.ChainConfigFiles
}
func (m *AddNodeRequest) GetPluginDir() string {
	if m == nil {
		return ""
	}
	return m.PluginDir
}

type AddNodeResponse struct {
	ClusterInfo *ClusterInfo `json:"clusterInfo,omitempty"`
}

type RemoveNodeRequest struct {
	Name string `json:"name,omitempty"`
}

type RemoveNodeResponse struct {
	ClusterInfo *ClusterInfo `json:"clusterInfo,omitempty"`
}

type RestartNodeRequest struct {
	Name              string            `json:"name,omitempty"`
	ExecPath          *string           `json:"execPath,omitempty"`
	WhitelistedChains *string           `json:"whitelistedChains,omitempty"`
	ChainConfigs      map[string]string `json:"chainConfigs,omitempty"`
	UpgradeConfigs    map[string]string `json:"upgradeConfigs,omitempty"`
	ChainConfigFiles  map[string]string `json:"chainConfigFiles,omitempty"`
	PluginDir         string            `json:"pluginDir,omitempty"`
}

func (m *RestartNodeRequest) GetName() string {
	if m == nil {
		return ""
	}
	return m.Name
}
func (m *RestartNodeRequest) GetExecPath() string {
	if m == nil || m.ExecPath == nil {
		return ""
	}
	return *m.ExecPath
}
func (m *RestartNodeRequest) GetWhitelistedChains() string {
	if m == nil || m.WhitelistedChains == nil {
		return ""
	}
	return *m.WhitelistedChains
}
func (m *RestartNodeRequest) GetChainConfigs() map[string]string {
	if m == nil {
		return nil
	}
	return m.ChainConfigs
}
func (m *RestartNodeRequest) GetUpgradeConfigs() map[string]string {
	if m == nil {
		return nil
	}
	return m.UpgradeConfigs
}
func (m *RestartNodeRequest) GetChainConfigFiles() map[string]string {
	if m == nil {
		return nil
	}
	return m.ChainConfigFiles
}
func (m *RestartNodeRequest) GetPluginDir() string {
	if m == nil {
		return ""
	}
	return m.PluginDir
}

type RestartNodeResponse struct {
	ClusterInfo *ClusterInfo `json:"clusterInfo,omitempty"`
}

type PauseNodeRequest struct {
	Name string `json:"name,omitempty"`
}

type PauseNodeResponse struct {
	ClusterInfo *ClusterInfo `json:"clusterInfo,omitempty"`
}

type ResumeNodeRequest struct {
	Name string `json:"name,omitempty"`
}

type ResumeNodeResponse struct {
	ClusterInfo *ClusterInfo `json:"clusterInfo,omitempty"`
}

type StopRequest struct{}

type StopResponse struct {
	ClusterInfo *ClusterInfo `json:"clusterInfo,omitempty"`
}

// =============================================================================
// AttachPeer / SendOutboundMessage
// =============================================================================

type AttachPeerRequest struct {
	NodeName string `json:"nodeName,omitempty"`
}

type AttachPeerResponse struct {
	ClusterInfo      *ClusterInfo      `json:"clusterInfo,omitempty"`
	AttachedPeerInfo *AttachedPeerInfo `json:"attachedPeerInfo,omitempty"`
}

type SendOutboundMessageRequest struct {
	NodeName string `json:"nodeName,omitempty"`
	PeerId   string `json:"peerId,omitempty"`
	Op       uint32 `json:"op,omitempty"`
	Bytes    []byte `json:"bytes,omitempty"`
}

type SendOutboundMessageResponse struct {
	Sent bool `json:"sent,omitempty"`
}

// =============================================================================
// Snapshots
// =============================================================================

type SaveSnapshotRequest struct {
	NetworkName  string `json:"networkName,omitempty"`
	SnapshotName string `json:"snapshotName,omitempty"`
}

func (m *SaveSnapshotRequest) GetNetworkName() string {
	if m == nil {
		return ""
	}
	return m.NetworkName
}
func (m *SaveSnapshotRequest) GetSnapshotName() string {
	if m == nil {
		return ""
	}
	return m.SnapshotName
}

type SaveSnapshotResponse struct {
	SnapshotPath string `json:"snapshotPath,omitempty"`
}

type LoadSnapshotRequest struct {
	NetworkName         string            `json:"networkName,omitempty"`
	SnapshotName        string            `json:"snapshotName,omitempty"`
	ExecPath            *string           `json:"execPath,omitempty"`
	PluginDir           string            `json:"pluginDir,omitempty"`
	RootDataDir         *string           `json:"rootDataDir,omitempty"`
	ChainConfigs        map[string]string `json:"chainConfigs,omitempty"`
	UpgradeConfigs      map[string]string `json:"upgradeConfigs,omitempty"`
	GlobalNodeConfig    *string           `json:"globalNodeConfig,omitempty"`
	ReassignPortsIfUsed *bool             `json:"reassignPortsIfUsed,omitempty"`
	ChainConfigFiles    map[string]string `json:"chainConfigFiles,omitempty"`
}

func (m *LoadSnapshotRequest) GetNetworkName() string {
	if m == nil {
		return ""
	}
	return m.NetworkName
}
func (m *LoadSnapshotRequest) GetSnapshotName() string {
	if m == nil {
		return ""
	}
	return m.SnapshotName
}
func (m *LoadSnapshotRequest) GetExecPath() string {
	if m == nil || m.ExecPath == nil {
		return ""
	}
	return *m.ExecPath
}
func (m *LoadSnapshotRequest) GetPluginDir() string {
	if m == nil {
		return ""
	}
	return m.PluginDir
}
func (m *LoadSnapshotRequest) GetRootDataDir() string {
	if m == nil || m.RootDataDir == nil {
		return ""
	}
	return *m.RootDataDir
}
func (m *LoadSnapshotRequest) GetChainConfigs() map[string]string {
	if m == nil {
		return nil
	}
	return m.ChainConfigs
}
func (m *LoadSnapshotRequest) GetUpgradeConfigs() map[string]string {
	if m == nil {
		return nil
	}
	return m.UpgradeConfigs
}
func (m *LoadSnapshotRequest) GetChainConfigFiles() map[string]string {
	if m == nil {
		return nil
	}
	return m.ChainConfigFiles
}
func (m *LoadSnapshotRequest) GetGlobalNodeConfig() string {
	if m == nil || m.GlobalNodeConfig == nil {
		return ""
	}
	return *m.GlobalNodeConfig
}
func (m *LoadSnapshotRequest) GetReassignPortsIfUsed() bool {
	if m == nil || m.ReassignPortsIfUsed == nil {
		return false
	}
	return *m.ReassignPortsIfUsed
}

type LoadSnapshotResponse struct {
	ClusterInfo *ClusterInfo `json:"clusterInfo,omitempty"`
}

type RemoveSnapshotRequest struct {
	NetworkName  string `json:"networkName,omitempty"`
	SnapshotName string `json:"snapshotName,omitempty"`
}

func (m *RemoveSnapshotRequest) GetNetworkName() string {
	if m == nil {
		return ""
	}
	return m.NetworkName
}
func (m *RemoveSnapshotRequest) GetSnapshotName() string {
	if m == nil {
		return ""
	}
	return m.SnapshotName
}

type RemoveSnapshotResponse struct{}

type GetSnapshotNamesRequest struct {
	NetworkName string `json:"networkName,omitempty"`
}

func (m *GetSnapshotNamesRequest) GetNetworkName() string {
	if m == nil {
		return ""
	}
	return m.NetworkName
}

type GetSnapshotNamesResponse struct {
	SnapshotNames []string `json:"snapshotNames,omitempty"`
}
