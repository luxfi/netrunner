package network

import (
	"context"
	"errors"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/netrunner/network/node"
)

var (
	ErrUndefined    = errors.New("undefined network")
	ErrStopped      = errors.New("network stopped")
	ErrNodeNotFound = errors.New("node not found in network")
)

type PermissionlessValidatorSpec struct {
	ChainID       string
	AssetID       string
	NodeName      string
	StakedAmount  uint64
	StartTime     time.Time
	StakeDuration time.Duration
}

type ElasticChainSpec struct {
	ChainID                  *string
	AssetName                string
	AssetSymbol              string
	InitialSupply            uint64
	MaxSupply                uint64
	MinConsumptionRate       uint64
	MaxConsumptionRate       uint64
	MinValidatorStake        uint64
	MaxValidatorStake        uint64
	MinStakeDuration         time.Duration
	MaxStakeDuration         time.Duration
	MinDelegationFee         uint32
	MinDelegatorStake        uint64
	MaxValidatorWeightFactor byte
	UptimeRequirement        uint32
}

type ParticipantsSpec struct {
	Participants []string
	ChainConfig  []byte
}

type RemoveChainValidatorSpec struct {
	NodeNames []string
	ChainID   string
}

type ChainSpec struct {
	VMName             string
	Genesis            []byte
	ChainID            *string
	ParticipantsSpec   *ParticipantsSpec
	ChainConfig        []byte
	NetworkUpgrade     []byte
	Alias              string
	PerNodeChainConfig map[string][]byte
	BlockchainName     string // Unique name for the blockchain (defaults to VMName if empty)
}

// Network is an abstraction of an Lux network
type Network interface {
	// Returns nil if all the nodes in the network are healthy.
	// A stopped network is considered unhealthy.
	// Timeout is given by the context parameter.
	Healthy(context.Context) error
	// Stop all the nodes.
	// Returns ErrStopped if Stop() was previously called.
	Stop(context.Context) error
	// Start a new node with the given config.
	// Returns ErrStopped if Stop() was previously called.
	AddNode(node.Config) (node.Node, error)
	// Stop the node with this name.
	// Returns ErrStopped if Stop() was previously called.
	RemoveNode(ctx context.Context, name string) error
	// Pause the node with this name.
	// Returns ErrStopped if Stop() was previously called.
	PauseNode(ctx context.Context, name string) error
	// Resume the node with this name.
	// Returns ErrStopped if Stop() was previously called.
	ResumeNode(ctx context.Context, name string) error
	// Return the node with this name.
	// Returns ErrStopped if Stop() was previously called.
	GetNode(name string) (node.Node, error)
	// Return all the nodes in this network.
	// Node name --> Node.
	// Returns ErrStopped if Stop() was previously called.
	GetAllNodes() (map[string]node.Node, error)
	// Returns the names of all nodes in this network.
	// Returns ErrStopped if Stop() was previously called.
	GetNodeNames() ([]string, error)
	// Save network snapshot
	// Network is stopped in order to do a safe preservation
	// Returns the full local path to the snapshot dir
	SaveSnapshot(context.Context, string) (string, error)
	// SaveHotSnapshot saves a snapshot without stopping the network
	// Uses CoW on APFS (macOS) for instant snapshots
	// WARNING: May have minor inconsistencies if writes occur during copy
	SaveHotSnapshot(context.Context, string) (string, error)
	// Remove network snapshot
	RemoveSnapshot(string) error
	// Get name of available snapshots
	GetSnapshotNames() ([]string, error)
	// Restart a given node using the same config, optionally changing binary path, plugin dir,
	// track chains, a map of chain configs, a map of upgrade configs, and
	// a map of chain configs
	RestartNode(context.Context, string, string, string, string, map[string]string, map[string]string, map[string]string) error
	// Create the specified chains
	CreateChains(context.Context, []ChainSpec) ([]ids.ID, error)
	// Create chain participant groups (validators)
	CreateParticipantGroups(context.Context, []ParticipantsSpec) ([]ids.ID, error)
	// Transform chain into elastic chain
	TransformChain(context.Context, []ElasticChainSpec) ([]ids.ID, []ids.ID, error)
	// Add a validator into an elastic chain
	AddPermissionlessValidators(context.Context, []PermissionlessValidatorSpec) error
	// Remove a validator from a chain
	RemoveChainValidators(context.Context, []RemoveChainValidatorSpec) error
	// Get the elastic chain tx id for the given chain id
	GetElasticChainID(context.Context, ids.ID) (ids.ID, error)
}
