package local

import (
	"context"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/netip"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"maps"
	"slices"

	"github.com/luxfi/config"
	"github.com/luxfi/crypto/bls"
	luxcrypto "github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
	"github.com/luxfi/keys"
	log "github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/netrunner/api"
	"github.com/luxfi/netrunner/network"
	"github.com/luxfi/netrunner/network/node"
	"github.com/luxfi/netrunner/network/node/status"
	"github.com/luxfi/netrunner/utils"
	"github.com/luxfi/netrunner/utils/constants"
	"github.com/luxfi/node/utils/beacon"
	"github.com/luxfi/p2p/peer"
	luxtls "github.com/luxfi/tls"

	"github.com/luxfi/address"
	"github.com/luxfi/node/utils/wrappers"
	"golang.org/x/mod/semver"
	"golang.org/x/sync/errgroup"
)

const (
	defaultNodeNamePrefix     = "node"
	configFileName            = "config.json"
	upgradeConfigFileName     = "upgrade.json"
	stakingKeyFileName        = "luxtls.key"
	stakingCertFileName       = "luxtls.crt"
	stakingSigningKeyFileName = "signer.key"
	genesisFileName           = "genesis.json"
	stopTimeout               = 30 * time.Second
	// healthCheckFreq reduced from 3s to 100ms for ultra-fast health polling
	healthCheckFreq      = 100 * time.Millisecond
	DefaultNumNodes      = 5
	snapshotPrefix       = "lux-snapshot-"
	networkRootDirPrefix = "network"
	defaultDBSubdir      = "db"
	defaultLogsSubdir    = "logs"
	// difference between unlock schedule locktime and startime in original genesis
	genesisLocktimeStartimeDelta = 2836800
)

// interface compliance
var (
	_ network.Network    = (*localNetwork)(nil)
	_ NodeProcessCreator = (*nodeProcessCreator)(nil)

	warnFlags = map[string]struct{}{
		config.NetworkIDKey:    {},
		config.BootstrapIPsKey: {},
		config.BootstrapIDsKey: {},
	}
	chainConfigSubDir  = "chainConfigs"
	pChainConfigSubDir = "pChainConfigs"

	snapshotsRelPath = filepath.Join(".netrunner", "snapshots")

	ErrSnapshotNotFound = errors.New("snapshot not found")
)

// network keeps information uses for network management, and accessing all the nodes
type localNetwork struct {
	lock   sync.RWMutex
	logger log.Logger
	// This network's ID.
	networkID uint32
	// This network's genesis file.
	// Must not be nil.
	genesis []byte
	// Used to create a new API client
	newAPIClientF api.NewAPIClientF
	// Used to create new node processes
	nodeProcessCreator NodeProcessCreator
	stopOnce           sync.Once
	// Closed when Stop begins.
	onStopCh chan struct{}
	// For node name generation
	nextNodeSuffix uint64
	// Node Name --> Node
	nodes map[string]*localNode
	// Set of nodes that new nodes will bootstrap from.
	bootstraps beacon.Set
	// rootDir is the root directory under which we write all node
	// logs, databases, etc.
	rootDir string
	// directory where networks can be persistently saved
	snapshotsDir string
	// flags to apply to all nodes per default
	flags map[string]interface{}
	// binary path to use per default
	binaryPath string
	// chain config files to use per default
	chainConfigFiles map[string]string
	// genesis config files for EVM chains (per blockchain ID)
	genesisConfigFiles map[string]string
	// upgrade config files to use per default
	upgradeConfigFiles map[string]string
	// P-chain config files to use per default
	pChainConfigFiles map[string]string
	// if true, for ports given in conf that are already taken, assign new random ones
	reassignPortsIfUsed bool
	// map from chain id to elastic chain tx id
	chainID2ElasticChainID map[ids.ID]ids.ID
}

type deprecatedFlagEsp struct {
	Version  string `json:"version"`
	OldName  string `json:"old_name"`
	NewName  string `json:"new_name"`
	ValueMap string `json:"value_map"`
}

var (
	//go:embed default
	embeddedDefaultNetworkConfigDir embed.FS
	//go:embed deprecatedFlagsSupport.json
	deprecatedFlagsSupportBytes []byte
	deprecatedFlagsSupport      []deprecatedFlagEsp
	// Pre-defined network configuration.
	// [defaultNetworkConfig] should not be modified.
	// TODO add method Copy() to network.Config to prevent
	// accidental overwriting
	defaultNetworkConfig network.Config
	// snapshots directory
	defaultSnapshotsDir string
)

// populate default network config from embedded default directory
func init() {
	// load genesis, updating validation start time
	genesisMap, err := network.LoadLocalGenesis()
	if err != nil {
		panic(err)
	}
	// load deprecated luxd flags support information
	if err = json.Unmarshal(deprecatedFlagsSupportBytes, &deprecatedFlagsSupport); err != nil {
		panic(err)
	}

	startTime := time.Now().Unix()
	lockTime := startTime + genesisLocktimeStartimeDelta
	genesisMap["startTime"] = float64(startTime)
	allocations, ok := genesisMap["allocations"].([]interface{})
	if !ok {
		panic(errors.New("could not get allocations in genesis"))
	}
	for _, allocIntf := range allocations {
		alloc, ok := allocIntf.(map[string]interface{})
		if !ok {
			panic(fmt.Errorf("unexpected type for allocation in genesis. got %T", allocIntf))
		}
		unlockSchedule, ok := alloc["unlockSchedule"].([]interface{})
		if !ok {
			panic(errors.New("could not get unlockSchedule in allocation"))
		}
		for _, schedIntf := range unlockSchedule {
			sched, ok := schedIntf.(map[string]interface{})
			if !ok {
				panic(fmt.Errorf("unexpected type for unlockSchedule elem in genesis. got %T", schedIntf))
			}
			if _, ok := sched["locktime"]; ok {
				sched["locktime"] = float64(lockTime)
			}
		}
	}

	// If LUX_PRIVATE_KEY is set, also allocate funds to that address for chain creation
	if privKeyHex := os.Getenv("LUX_PRIVATE_KEY"); privKeyHex != "" {
		privKeyBytes, err := hex.DecodeString(privKeyHex)
		if err == nil && len(privKeyBytes) == 32 {
			// Derive P-chain address from private key
			luxPrivKey, err := luxcrypto.ToPrivateKey(privKeyBytes)
			if err == nil {
				pubKey := luxPrivKey.PublicKey()
				pChainAddr := ids.ShortID(pubKey.Address())
				// For default network (local, ID 1337), use "custom" HRP
				luxAddr, err := address.Format("X", "custom", pChainAddr[:])
				if err == nil {
					fmt.Printf("🔑 Adding default network allocation for LUX_PRIVATE_KEY address: %s\n", luxAddr)
					privKeyAlloc := map[string]interface{}{
						"ethAddr":       "0x" + hex.EncodeToString(pChainAddr[:]),
						"luxAddr":       luxAddr,
						"initialAmount": float64(0),
						"unlockSchedule": []interface{}{
							map[string]interface{}{
								"amount":   float64(100000000000000000), // 100M LUX in nLUX
								"locktime": float64(0),                  // Immediately available
							},
						},
					}
					allocations = append(allocations, privKeyAlloc)
					genesisMap["allocations"] = allocations

					// CRITICAL: Also add to initialStakedFunds so funds are available on P-chain
					// This is required for CreateChainTx and other P-chain operations
					initialStakedFunds, _ := genesisMap["initialStakedFunds"].([]interface{})
					initialStakedFunds = append(initialStakedFunds, luxAddr)
					genesisMap["initialStakedFunds"] = initialStakedFunds
					fmt.Printf("🔑 Added %s to initialStakedFunds for P-chain access\n", luxAddr)
				}
			}
		}
	}

	// now we can marshal the *whole* thing into bytes
	updatedGenesis, err := json.Marshal(genesisMap)
	if err != nil {
		panic(err)
	}

	// load network flags
	configsDir, err := fs.Sub(embeddedDefaultNetworkConfigDir, "default")
	if err != nil {
		panic(err)
	}
	flagsBytes, err := fs.ReadFile(configsDir, "flags.json")
	if err != nil {
		panic(err)
	}
	flags := map[string]interface{}{}
	if err = json.Unmarshal(flagsBytes, &flags); err != nil {
		panic(err)
	}

	// load chain config
	cChainConfig, err := fs.ReadFile(configsDir, "cchain_config.json")
	if err != nil {
		panic(err)
	}

	defaultNetworkConfig = network.Config{
		NodeConfigs: make([]node.Config, DefaultNumNodes),
		Flags:       flags,
		Genesis:     string(updatedGenesis),
		ChainConfigFiles: map[string]string{
			"C": string(cChainConfig),
		},
		UpgradeConfigFiles: map[string]string{},
		PChainConfigFiles:  map[string]string{},
	}

	for i := 0; i < len(defaultNetworkConfig.NodeConfigs); i++ {
		flagsBytes, err := fs.ReadFile(configsDir, fmt.Sprintf("node%d/flags.json", i+1))
		if err != nil {
			panic(err)
		}
		flags := map[string]interface{}{}
		if err = json.Unmarshal(flagsBytes, &flags); err != nil {
			panic(err)
		}
		defaultNetworkConfig.NodeConfigs[i].Flags = flags
		stakingKey, err := fs.ReadFile(configsDir, fmt.Sprintf("node%d/luxtls.key", i+1))
		if err != nil {
			panic(err)
		}
		defaultNetworkConfig.NodeConfigs[i].StakingKey = string(stakingKey)
		stakingCert, err := fs.ReadFile(configsDir, fmt.Sprintf("node%d/luxtls.crt", i+1))
		if err != nil {
			panic(err)
		}
		defaultNetworkConfig.NodeConfigs[i].StakingCert = string(stakingCert)
		stakingSigningKey, err := fs.ReadFile(configsDir, fmt.Sprintf("node%d/signer.key", i+1))
		if err != nil {
			panic(err)
		}
		encodedStakingSigningKey := base64.StdEncoding.EncodeToString(stakingSigningKey)
		defaultNetworkConfig.NodeConfigs[i].StakingSigningKey = encodedStakingSigningKey
		defaultNetworkConfig.NodeConfigs[i].IsBeacon = true
	}

	// create default snapshots dir
	usr, err := user.Current()
	if err != nil {
		panic(err)
	}
	defaultSnapshotsDir = filepath.Join(usr.HomeDir, snapshotsRelPath)
}

// NewNetwork returns a new network that uses the given log.
// Files (e.g. logs, databases) default to being written at directory [rootDir].
// If there isn't a directory at [dir] one will be created.
// If len([dir]) == 0, files will be written underneath a new temporary directory.
// Snapshots are saved to snapshotsDir, defaults to defaultSnapshotsDir if not given
func NewNetwork(
	log log.Logger,
	networkConfig network.Config,
	rootDir string,
	snapshotsDir string,
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
	return net, net.loadConfig(context.Background(), networkConfig)
}

// See NewNetwork.
// [newAPIClientF] is used to create new API clients.
// [nodeProcessCreator] is used to launch new node processes.
func newNetwork(
	logger log.Logger,
	newAPIClientF api.NewAPIClientF,
	nodeProcessCreator NodeProcessCreator,
	rootDir string,
	snapshotsDir string,
	reassignPortsIfUsed bool,
) (*localNetwork, error) {
	var err error
	if rootDir == "" {
		anrRootDir := filepath.Join(os.TempDir(), constants.RootDirPrefix)
		err = os.MkdirAll(anrRootDir, os.ModePerm)
		if err != nil {
			return nil, err
		}
		networkRootDir := filepath.Join(anrRootDir, networkRootDirPrefix)
		rootDir, err = utils.MkDirWithTimestamp(networkRootDir)
		if err != nil {
			return nil, err
		}
	}
	if snapshotsDir == "" {
		snapshotsDir = defaultSnapshotsDir
	}
	// create the snapshots dir if not present
	err = os.MkdirAll(snapshotsDir, os.ModePerm)
	if err != nil {
		return nil, err
	}
	// Create the network
	net := &localNetwork{
		nextNodeSuffix:         1,
		nodes:                  map[string]*localNode{},
		onStopCh:               make(chan struct{}),
		logger:                 logger,
		bootstraps:             beacon.NewSet(),
		newAPIClientF:          newAPIClientF,
		nodeProcessCreator:     nodeProcessCreator,
		rootDir:                rootDir,
		snapshotsDir:           snapshotsDir,
		reassignPortsIfUsed:    reassignPortsIfUsed,
		chainID2ElasticChainID: map[ids.ID]ids.ID{},
	}
	return net, nil
}

// NewDefaultNetwork returns a new network using a pre-defined
// network configuration.
// The following addresses are pre-funded (treasury address):
// X-Chain Address:       X-lux1c7wevm4667l4umtzh93r25wpxlpsadkhka6gv6
// P-Chain Address:       P-lux1c7wevm4667l4umtzh93r25wpxlpsadkhka6gv6
// C-Chain Address:       0x9011E888251AB053B7bD1cdB598Db4f9DEd94714
// Keys are dynamically generated per network - see ~/.lux/keys/
// The following nodes are validators:
// * NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg
// * NodeID-MFrZFVCXPv5iCn6M9K6XduxGTYp891xXZ
// * NodeID-NFBbbJ4qCmNaCzeW7sxErhvWqvEQMnYcN
// * NodeID-GWPcbFJZFfZreETSoWjPimr846mXEKCtu
// * NodeID-P7oB2McjBGgW2NXXWVYjV8JEDFoW9xDE5
func NewDefaultNetwork(
	log log.Logger,
	binaryPath string,
	reassignPortsIfUsed bool,
) (network.Network, error) {
	config := NewDefaultConfig(binaryPath)
	return NewNetwork(log, config, "", "", reassignPortsIfUsed)
}

// NewDefaultConfig creates a new default network config
func NewDefaultConfig(binaryPath string) network.Config {
	config := defaultNetworkConfig
	config.BinaryPath = binaryPath
	// Don't overwrite [DefaultNetworkConfig.NodeConfigs]
	config.NodeConfigs = make([]node.Config, len(defaultNetworkConfig.NodeConfigs))
	copy(config.NodeConfigs, defaultNetworkConfig.NodeConfigs)
	// copy maps
	config.ChainConfigFiles = maps.Clone(config.ChainConfigFiles)
	config.Flags = maps.Clone(config.Flags)
	for i := range config.NodeConfigs {
		config.NodeConfigs[i].Flags = maps.Clone(config.NodeConfigs[i].Flags)
	}
	return config
}

// NewDefaultConfigNNodes creates a new default network config, with an arbitrary number of nodes
func NewDefaultConfigNNodes(binaryPath string, numNodes uint32) (network.Config, error) {
	netConfig := NewDefaultConfig(binaryPath)
	if int(numNodes) > len(netConfig.NodeConfigs) {
		toAdd := int(numNodes) - len(netConfig.NodeConfigs)
		refNodeConfig := netConfig.NodeConfigs[len(netConfig.NodeConfigs)-1]
		refAPIPortIntf, ok := refNodeConfig.Flags[config.HTTPPortKey]
		if !ok {
			return netConfig, fmt.Errorf("could not get last standard api port from config")
		}
		refAPIPort, ok := refAPIPortIntf.(float64)
		if !ok {
			return netConfig, fmt.Errorf("expected float64 for last standard api port, got %T", refAPIPortIntf)
		}
		refStakingPortIntf, ok := refNodeConfig.Flags[config.StakingPortKey]
		if !ok {
			return netConfig, fmt.Errorf("could not get last standard staking port from config")
		}
		refStakingPort, ok := refStakingPortIntf.(float64)
		if !ok {
			return netConfig, fmt.Errorf("expected float64 for last standard api port, got %T", refStakingPortIntf)
		}
		for i := 0; i < toAdd; i++ {
			nodeConfig := refNodeConfig
			stakingCert, stakingKey, err := luxtls.NewCertAndKeyBytes()
			if err != nil {
				return netConfig, fmt.Errorf("couldn't generate staking Cert/Key: %w", err)
			}
			nodeConfig.StakingKey = string(stakingKey)
			nodeConfig.StakingCert = string(stakingCert)
			// replace ports
			nodeConfig.Flags = map[string]interface{}{
				config.HTTPPortKey:    int(refAPIPort) + (i+1)*2,
				config.StakingPortKey: int(refStakingPort) + (i+1)*2,
			}
			netConfig.NodeConfigs = append(netConfig.NodeConfigs, nodeConfig)
		}
	}
	if int(numNodes) < len(netConfig.NodeConfigs) {
		netConfig.NodeConfigs = netConfig.NodeConfigs[:numNodes]
	}

	// If LUX_MNEMONIC is set, add allocations for mnemonic-derived address
	mnemonic := os.Getenv("LUX_MNEMONIC")
	fmt.Printf("🔍 DEBUG: NewDefaultConfigNNodes called, LUX_MNEMONIC set=%v\n", mnemonic != "")
	if mnemonic != "" {
		vk, err := keys.DeriveValidatorFromMnemonic(mnemonic, 0)
		if err != nil {
			return netConfig, fmt.Errorf("failed to derive key from mnemonic: %w", err)
		}

		// Parse genesis
		var genesis map[string]interface{}
		if err := json.Unmarshal([]byte(netConfig.Genesis), &genesis); err != nil {
			return netConfig, fmt.Errorf("failed to parse genesis: %w", err)
		}

		// Create X-chain and P-chain addresses
		// Network ID 1337 uses "local" HRP (per constants.NetworkIDToHRP)
		const hrp = "local"
		fmt.Printf("🔍 DEBUG: vk.PChainAddr (base58): %s\n", vk.PChainAddr.String())
		fmt.Printf("🔍 DEBUG: vk.PChainAddr (hex): %x\n", vk.PChainAddr[:])
		xLuxAddr, err := address.Format("X", hrp, vk.PChainAddr[:])
		if err != nil {
			return netConfig, fmt.Errorf("failed to format X-chain address: %w", err)
		}
		pLuxAddr, err := address.Format("P", hrp, vk.PChainAddr[:])
		if err != nil {
			return netConfig, fmt.Errorf("failed to format P-chain address: %w", err)
		}

		fmt.Printf("🔑 Adding local network allocations for LUX_MNEMONIC: X=%s, P=%s (2B each)\n", xLuxAddr, pLuxAddr)

		// Get existing allocations
		allocations, _ := genesis["allocations"].([]interface{})

		// Add X-chain allocation
		allocations = append(allocations, map[string]interface{}{
			"luxAddr":        xLuxAddr,
			"ethAddr":        "0x0000000000000000000000000000000000000000",
			"initialAmount":  2_000_000_000_000_000_000, // 2B LUX
			"unlockSchedule": []interface{}{},
		})

		// Add P-chain allocation with unlockSchedule (builder only reads P-chain amounts from unlockSchedule)
		allocations = append(allocations, map[string]interface{}{
			"luxAddr":       pLuxAddr,
			"ethAddr":       "0x0000000000000000000000000000000000000000",
			"initialAmount": 0, // Not used for P-chain - amounts come from unlockSchedule
			"unlockSchedule": []interface{}{
				map[string]interface{}{
					"amount":   uint64(2_000_000_000_000_000_000), // 2B LUX
					"locktime": uint64(0),                         // Immediately available
				},
			},
		})

		genesis["allocations"] = allocations

		// Re-encode genesis
		updatedGenesis, err := json.MarshalIndent(genesis, "", "  ")
		if err != nil {
			return netConfig, fmt.Errorf("failed to encode updated genesis: %w", err)
		}
		netConfig.Genesis = string(updatedGenesis)
	}

	return netConfig, nil
}

func (ln *localNetwork) loadConfig(ctx context.Context, networkConfig network.Config) error {
	if err := networkConfig.Validate(); err != nil {
		return fmt.Errorf("config failed validation: %w", err)
	}
	ln.logger.Info("creating network", log.Int("node-num", len(networkConfig.NodeConfigs)))

	ln.genesis = []byte(networkConfig.Genesis)

	var err error
	ln.networkID, err = utils.NetworkIDFromGenesis([]byte(networkConfig.Genesis))
	if err != nil {
		return fmt.Errorf("couldn't get network ID from genesis: %w", err)
	}

	// save node defaults
	ln.flags = networkConfig.Flags
	ln.binaryPath = networkConfig.BinaryPath
	ln.chainConfigFiles = networkConfig.ChainConfigFiles
	if ln.chainConfigFiles == nil {
		ln.chainConfigFiles = map[string]string{}
	}
	ln.genesisConfigFiles = networkConfig.GenesisConfigFiles
	if ln.genesisConfigFiles == nil {
		ln.genesisConfigFiles = map[string]string{}
	}
	ln.upgradeConfigFiles = networkConfig.UpgradeConfigFiles
	if ln.upgradeConfigFiles == nil {
		ln.upgradeConfigFiles = map[string]string{}
	}

	// Sort node configs so beacons start first
	var nodeConfigs []node.Config
	for _, nodeConfig := range networkConfig.NodeConfigs {
		if nodeConfig.IsBeacon {
			nodeConfigs = append(nodeConfigs, nodeConfig)
		}
	}
	for _, nodeConfig := range networkConfig.NodeConfigs {
		if !nodeConfig.IsBeacon {
			nodeConfigs = append(nodeConfigs, nodeConfig)
		}
	}

	for _, nodeConfig := range nodeConfigs {
		if _, err := ln.addNode(nodeConfig); err != nil {
			if err := ln.stop(ctx); err != nil {
				// Clean up nodes already created
				ln.logger.Debug("error stopping network", log.Err(err))
			}
			return fmt.Errorf("error adding node %s: %w", nodeConfig.Name, err)
		}
	}

	return nil
}

// See network.Network
func (ln *localNetwork) AddNode(nodeConfig node.Config) (node.Node, error) {
	ln.lock.Lock()
	defer ln.lock.Unlock()

	if ln.stopCalled() {
		return nil, network.ErrStopped
	}

	return ln.addNode(nodeConfig)
}

// Assumes [ln.lock] is held and [ln.Stop] hasn't been called.
func (ln *localNetwork) addNode(nodeConfig node.Config) (node.Node, error) {
	if nodeConfig.Flags == nil {
		nodeConfig.Flags = map[string]interface{}{}
	}
	if nodeConfig.ChainConfigFiles == nil {
		nodeConfig.ChainConfigFiles = map[string]string{}
	}
	if nodeConfig.UpgradeConfigFiles == nil {
		nodeConfig.UpgradeConfigFiles = map[string]string{}
	}
	if nodeConfig.ChainConfigFiles == nil {
		nodeConfig.ChainConfigFiles = map[string]string{}
	}

	// load node defaults
	if nodeConfig.BinaryPath == "" {
		nodeConfig.BinaryPath = ln.binaryPath
	}
	for k, v := range ln.chainConfigFiles {
		_, ok := nodeConfig.ChainConfigFiles[k]
		if !ok {
			nodeConfig.ChainConfigFiles[k] = v
		}
	}
	for k, v := range ln.upgradeConfigFiles {
		_, ok := nodeConfig.UpgradeConfigFiles[k]
		if !ok {
			nodeConfig.UpgradeConfigFiles[k] = v
		}
	}
	for k, v := range ln.chainConfigFiles {
		_, ok := nodeConfig.ChainConfigFiles[k]
		if !ok {
			nodeConfig.ChainConfigFiles[k] = v
		}
	}
	addNetworkFlags(ln.flags, nodeConfig.Flags)

	// it shouldn't happen that just one is empty, most probably both,
	// but in any case if just one is empty it's unusable so we just assign a new one.
	if nodeConfig.StakingCert == "" || nodeConfig.StakingKey == "" {
		ln.logger.Warn("staking cert/key empty, generating new ones",
			log.String("node", nodeConfig.Name),
			log.Int("certLen", len(nodeConfig.StakingCert)),
			log.Int("keyLen", len(nodeConfig.StakingKey)))
		stakingCert, stakingKey, err := luxtls.NewCertAndKeyBytes()
		if err != nil {
			return nil, fmt.Errorf("couldn't generate staking Cert/Key: %w", err)
		}
		nodeConfig.StakingCert = string(stakingCert)
		nodeConfig.StakingKey = string(stakingKey)
	} else {
		ln.logger.Info("using provided staking cert/key",
			log.String("node", nodeConfig.Name),
			log.Int("certLen", len(nodeConfig.StakingCert)),
			log.Int("keyLen", len(nodeConfig.StakingKey)))
	}
	if nodeConfig.StakingSigningKey == "" {
		secretKey, err := bls.NewSecretKey()
		if err != nil {
			return nil, fmt.Errorf("couldn't generate new signing key: %w", err)
		}
		keyBytes := bls.SecretKeyToBytes(secretKey)
		encodedKey := base64.StdEncoding.EncodeToString(keyBytes)
		nodeConfig.StakingSigningKey = encodedKey
	}

	if err := ln.setNodeName(&nodeConfig); err != nil {
		return nil, err
	}

	isPausedNode := ln.isPausedNode(&nodeConfig)

	nodeDir, err := makeNodeDir(ln.logger, ln.rootDir, nodeConfig.Name)
	if err != nil {
		return nil, err
	}

	// If config file is given, don't overwrite API port, P2P port, DB path, logs path
	var configFile map[string]interface{}
	if len(nodeConfig.ConfigFile) != 0 {
		if err := json.Unmarshal([]byte(nodeConfig.ConfigFile), &configFile); err != nil {
			return nil, fmt.Errorf("couldn't unmarshal config file: %w", err)
		}
	}

	// Get node version
	nodeSemVer, err := ln.getNodeSemVer(nodeConfig)
	if err != nil {
		return nil, err
	}

	nodeData, err := ln.buildArgs(nodeSemVer, configFile, nodeDir, &nodeConfig)
	if err != nil {
		return nil, err
	}

	// Parse this node's ID
	nodeID, err := utils.ToNodeID([]byte(nodeConfig.StakingKey), []byte(nodeConfig.StakingCert))
	if err != nil {
		return nil, fmt.Errorf("couldn't get node ID: %w", err)
	}

	// Start the Lux node and pass it the flags defined above
	nodeProcess, err := ln.nodeProcessCreator.NewNodeProcess(nodeConfig, nodeData.args...)
	if err != nil {
		return nil, fmt.Errorf(
			"couldn't create new node process with binary %q and args %v: %w",
			nodeConfig.BinaryPath, nodeData.args, err,
		)
	}

	ln.logger.Info(
		"adding node",
		log.String("node-name", nodeConfig.Name),
		log.String("node-dir", nodeData.dataDir),
		log.String("log-dir", nodeData.logsDir),
		log.String("db-dir", nodeData.dbDir),
		log.Uint16("p2p-port", nodeData.p2pPort),
		log.Uint16("api-port", nodeData.apiPort),
	)

	ln.logger.Debug(
		"starting node",
		log.String("name", nodeConfig.Name),
		log.String("binaryPath", nodeConfig.BinaryPath),
		log.Strings("args", nodeData.args),
	)

	// Create a wrapper for this node so we can reference it later
	node := &localNode{
		name:          nodeConfig.Name,
		nodeID:        nodeID,
		networkID:     ln.networkID,
		client:        ln.newAPIClientF("localhost", nodeData.apiPort),
		process:       nodeProcess,
		apiPort:       nodeData.apiPort,
		p2pPort:       nodeData.p2pPort,
		getConnFunc:   defaultGetConnFunc,
		dataDir:       nodeData.dataDir,
		dbDir:         nodeData.dbDir,
		logsDir:       nodeData.logsDir,
		config:        nodeConfig,
		pluginDir:     nodeData.pluginDir,
		httpHost:      nodeData.httpHost,
		attachedPeers: map[string]peer.Peer{},
	}
	ln.nodes[node.name] = node
	// If this node is a beacon, add its IP/ID to the beacon lists.
	// Note that we do this *after* we set this node's bootstrap IPs/IDs
	// so this node won't try to use itself as a beacon.
	if !isPausedNode && nodeConfig.IsBeacon {
		err = ln.bootstraps.Add(beacon.New(nodeID, netip.AddrPortFrom(
			netip.MustParseAddr("127.0.0.1"),
			nodeData.p2pPort,
		)))
	}
	return node, err
}

// See network.Network
func (ln *localNetwork) Healthy(ctx context.Context) error {
	ln.lock.RLock()
	defer ln.lock.RUnlock()

	return ln.healthy(ctx)
}

func (ln *localNetwork) healthy(ctx context.Context) error {
	ln.logger.Info("checking local network healthiness", log.Int("num-of-nodes", len(ln.nodes)))

	// Return unhealthy if the network is stopped
	if ln.stopCalled() {
		return network.ErrStopped
	}

	// Derive a new context that's cancelled when Stop is called,
	// so that calls to Healthy() below immediately return.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func(ctx context.Context) {
		// This goroutine runs until [ln.Stop] is called
		// or this function returns.
		select {
		case <-ln.onStopCh:
			cancel()
		case <-ctx.Done():
		}
	}(ctx)

	errGr, ctx := errgroup.WithContext(ctx)
	for _, node := range ln.nodes {
		if node.paused {
			// no health check for paused nodes
			continue
		}
		node := node
		nodeName := node.GetName()
		errGr.Go(func() error {
			// Every [healthCheckFreq], query node for health status.
			// Do this until ctx timeout or network closed.
			retryCount := 0
			ln.logger.Info("health check goroutine started", log.String("node", nodeName))
			for {
				retryCount++
				ln.logger.Info("health check iteration", log.String("node", nodeName), log.Int("retry", retryCount))

				nodeStatus := node.Status()
				if nodeStatus != status.Running {
					// If we had stopped this node ourselves, it wouldn't be in [ln.nodes].
					// Since it is, it means the node stopped unexpectedly.
					ln.logger.Warn("node not running during health check", log.String("node", nodeName), log.String("status", string(nodeStatus)))
					return fmt.Errorf("node %q stopped unexpectedly (status=%s)", nodeName, nodeStatus)
				}
				healthClient := node.client.HealthAPI()
				if healthClient == nil {
					ln.logger.Error("health client is nil", log.String("node", nodeName))
					return fmt.Errorf("health client is nil for node %v", nodeName)
				}
				// Use Readiness instead of Health for local testnets
				// Health fails on local networks due to "no inbound connections"
				// which is expected behavior for isolated test networks
				health, err := healthClient.Readiness(ctx, nil)
				if err == nil && health.Healthy {
					ln.logger.Info("node became ready", log.String("name", nodeName))
					return nil
				}
				if err != nil {
					ln.logger.Info("health check error (will retry)", log.String("node", nodeName), log.Err(err))
				} else {
					ln.logger.Info("health check unhealthy (will retry)", log.String("node", nodeName), log.Bool("healthy", health.Healthy))
				}
				select {
				case <-ctx.Done():
					ln.logger.Warn("context done during health check - RETURNING ERROR", log.String("node", nodeName), log.Int("retries", retryCount), log.Err(ctx.Err()))
					return fmt.Errorf("node %q failed to become healthy within timeout, or network stopped (retries=%d, ctxErr=%v)", nodeName, retryCount, ctx.Err())
				case <-time.After(healthCheckFreq):
					ln.logger.Info("will retry health check", log.String("node", nodeName))
				}
			}
		})
	}
	// Wait until all nodes are ready or timeout
	return errGr.Wait()
}

// See network.Network
func (ln *localNetwork) GetNode(nodeName string) (node.Node, error) {
	ln.lock.RLock()
	defer ln.lock.RUnlock()

	if ln.stopCalled() {
		return nil, network.ErrStopped
	}

	node, ok := ln.nodes[nodeName]
	if !ok {
		return nil, network.ErrNodeNotFound
	}
	return node, nil
}

// See network.Network
func (ln *localNetwork) GetNodeNames() ([]string, error) {
	ln.lock.RLock()
	defer ln.lock.RUnlock()

	if ln.stopCalled() {
		return nil, network.ErrStopped
	}

	return slices.Collect(maps.Keys(ln.nodes)), nil
}

// See network.Network
func (ln *localNetwork) GetAllNodes() (map[string]node.Node, error) {
	ln.lock.RLock()
	defer ln.lock.RUnlock()

	if ln.stopCalled() {
		return nil, network.ErrStopped
	}

	nodesCopy := make(map[string]node.Node, len(ln.nodes))
	for name, node := range ln.nodes {
		nodesCopy[name] = node
	}
	return nodesCopy, nil
}

func (ln *localNetwork) Stop(ctx context.Context) error {
	ln.logger.Info("Stop() called, capturing stack trace")
	// Log stack trace to understand who's calling Stop
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	ln.logger.Info("Stop() stack trace", log.String("stack", string(buf[:n])))

	err := network.ErrStopped
	ln.stopOnce.Do(
		func() {
			close(ln.onStopCh)

			ln.lock.Lock()
			defer ln.lock.Unlock()

			err = ln.stop(ctx)
		},
	)
	return err
}

// Assumes [ln.lock] is held.
func (ln *localNetwork) stop(ctx context.Context) error {
	errs := wrappers.Errs{}
	for nodeName := range ln.nodes {
		stopCtx, stopCtxCancel := context.WithTimeout(ctx, stopTimeout)
		if err := ln.removeNode(stopCtx, nodeName); err != nil {
			ln.logger.Error("error stopping node", log.String("name", nodeName), log.Err(err))
			errs.Add(err)
		}
		stopCtxCancel()
	}
	ln.logger.Info("done stopping network")
	return errs.Err
}

// Sends a SIGTERM to the given node and removes it from this network.
func (ln *localNetwork) RemoveNode(ctx context.Context, nodeName string) error {
	ln.lock.Lock()
	defer ln.lock.Unlock()

	if ln.stopCalled() {
		return network.ErrStopped
	}
	return ln.removeNode(ctx, nodeName)
}

// Assumes [ln.lock] is held.
func (ln *localNetwork) removeNode(ctx context.Context, nodeName string) error {
	ln.logger.Debug("removing node", log.String("name", nodeName))
	node, ok := ln.nodes[nodeName]
	if !ok {
		return fmt.Errorf("node %q not found", nodeName)
	}

	paused := node.paused

	// If the node wasn't a beacon, we don't care
	_ = ln.bootstraps.RemoveByID(node.nodeID)
	delete(ln.nodes, nodeName)

	if !paused {
		// Note: CChainEthAPI returns an empty interface, so no Close() method available
		// The websocket connection cleanup is handled internally
		exitCode := node.process.Stop(ctx)
		// Accept graceful shutdown exit codes:
		// - 0: normal exit
		// - -1: process was killed before it could be waited on (signal handling)
		// - 2: luxd uses this for graceful shutdown via signal
		// - 130 (128+2): SIGINT (Ctrl+C)
		// - 143 (128+15): SIGTERM
		// Only treat other non-zero exit codes as errors
		if exitCode != 0 && exitCode != -1 && exitCode != 2 && exitCode != 130 && exitCode != 143 {
			return fmt.Errorf("node %q exited with exit code: %d", nodeName, exitCode)
		}
	}
	return nil
}

// Sends a SIGTERM to the given node and keeps it in the network with paused state
func (ln *localNetwork) PauseNode(ctx context.Context, nodeName string) error {
	ln.lock.Lock()
	defer ln.lock.Unlock()
	if ln.stopCalled() {
		return network.ErrStopped
	}
	return ln.pauseNode(ctx, nodeName)
}

// Assumes [ln.lock] is held.
func (ln *localNetwork) pauseNode(ctx context.Context, nodeName string) error {
	ln.logger.Debug("pausing node", log.String("name", nodeName))
	node, ok := ln.nodes[nodeName]
	if !ok {
		return fmt.Errorf("node %q not found", nodeName)
	}
	if node.paused {
		return fmt.Errorf("node has been paused already")
	}
	// Note: CChainEthAPI returns an empty interface, so no Close() method available
	// The websocket connection cleanup is handled internally
	exitCode := node.process.Stop(ctx)
	// Accept graceful shutdown exit codes (see removeNode for details)
	if exitCode != 0 && exitCode != -1 && exitCode != 2 && exitCode != 130 && exitCode != 143 {
		return fmt.Errorf("node %q exited with exit code: %d", nodeName, exitCode)
	}
	syncFilesystem()
	node.paused = true
	return nil
}

// Resume previously paused [nodeName] using the same config.
func (ln *localNetwork) ResumeNode(
	ctx context.Context,
	nodeName string,
) error {
	ln.lock.Lock()
	defer ln.lock.Unlock()

	return ln.resumeNode(
		ctx,
		nodeName,
	)
}

// Assumes [ln.lock] is held.
func (ln *localNetwork) resumeNode(
	_ context.Context,
	nodeName string,
) error {
	node, ok := ln.nodes[nodeName]
	if !ok {
		return fmt.Errorf("node %q not found", nodeName)
	}
	if !node.paused {
		return fmt.Errorf("node has not been paused")
	}
	nodeConfig := node.GetConfig()
	nodeConfig.Flags[config.DataDirKey] = node.GetDataDir()
	nodeConfig.Flags[config.DBPathKey] = node.GetDbDir()
	nodeConfig.Flags[config.LogsDirKey] = node.GetLogsDir()
	nodeConfig.Flags[config.HTTPPortKey] = int(node.GetAPIPort())
	nodeConfig.Flags[config.StakingPortKey] = int(node.GetP2PPort())
	if _, err := ln.addNode(nodeConfig); err != nil {
		return err
	}
	return nil
}

// Restart [nodeName] using the same config, optionally changing [binaryPath],
// [pluginDir], [trackChains], [chainConfigs], [upgradeConfigs], [pChainConfigs]
func (ln *localNetwork) RestartNode(
	ctx context.Context,
	nodeName string,
	binaryPath string,
	pluginDir string,
	trackChains string,
	chainConfigs map[string]string,
	upgradeConfigs map[string]string,
	pChainConfigs map[string]string,
) error {
	ln.lock.Lock()
	defer ln.lock.Unlock()

	return ln.restartNode(
		ctx,
		nodeName,
		binaryPath,
		pluginDir,
		trackChains,
		chainConfigs,
		upgradeConfigs,
		pChainConfigs,
	)
}

func (ln *localNetwork) restartNode(
	ctx context.Context,
	nodeName string,
	binaryPath string,
	pluginDir string,
	trackChains string,
	chainConfigs map[string]string,
	upgradeConfigs map[string]string,
	pChainConfigs map[string]string,
) error {
	node, ok := ln.nodes[nodeName]
	if !ok {
		return fmt.Errorf("node %q not found", nodeName)
	}

	nodeConfig := node.GetConfig()

	if binaryPath != "" {
		nodeConfig.BinaryPath = binaryPath
	}
	if pluginDir != "" {
		nodeConfig.Flags[config.PluginDirKey] = pluginDir
	}

	if trackChains != "" {
		nodeConfig.Flags[config.TrackChainsKey] = trackChains
	}

	// keep same ports, dbdir in node flags
	nodeConfig.Flags[config.DataDirKey] = node.GetDataDir()
	nodeConfig.Flags[config.DBPathKey] = node.GetDbDir()
	nodeConfig.Flags[config.LogsDirKey] = node.GetLogsDir()
	nodeConfig.Flags[config.HTTPPortKey] = int(node.GetAPIPort())
	nodeConfig.Flags[config.StakingPortKey] = int(node.GetP2PPort())
	// apply chain configs
	for k, v := range chainConfigs {
		nodeConfig.ChainConfigFiles[k] = v
	}
	// apply upgrade configs
	for k, v := range upgradeConfigs {
		nodeConfig.UpgradeConfigFiles[k] = v
	}
	// apply chain configs
	for k, v := range chainConfigs {
		nodeConfig.ChainConfigFiles[k] = v
	}

	if !node.paused {
		// Get the ports before removing the node
		apiPort := node.GetAPIPort()
		p2pPort := node.GetP2PPort()

		if err := ln.removeNode(ctx, nodeName); err != nil {
			return err
		}
		syncFilesystem()

		// Wait for ports to be released (TCP TIME_WAIT)
		// This prevents "bind: address already in use" errors
		// Use shorter timeout (5s) since ports are usually available quickly after process stop
		waitForPortsAvailable(apiPort, p2pPort, 5*time.Second)
	}

	if _, err := ln.addNode(nodeConfig); err != nil {
		return err
	}

	return nil
}

// Returns whether Stop has been called.
func (ln *localNetwork) stopCalled() bool {
	select {
	case <-ln.onStopCh:
		return true
	default:
		return false
	}
}

func (ln *localNetwork) isPausedNode(nodeConfig *node.Config) bool {
	if node, ok := ln.nodes[nodeConfig.Name]; ok && node.paused {
		return true
	}
	return false
}

// waitForPortsAvailable waits until the specified ports are available for binding.
// This is necessary after stopping a node to ensure the ports are released from TIME_WAIT state.
func waitForPortsAvailable(apiPort, p2pPort uint16, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	checkInterval := 100 * time.Millisecond

	for time.Now().Before(deadline) {
		apiAvailable := isPortAvailable(apiPort)
		p2pAvailable := isPortAvailable(p2pPort)

		if apiAvailable && p2pAvailable {
			return
		}

		time.Sleep(checkInterval)
	}
}

// isPortAvailable checks if a TCP port is available for binding
func isPortAvailable(port uint16) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// Set [nodeConfig].Name if it isn't given and assert it's unique.
func (ln *localNetwork) setNodeName(nodeConfig *node.Config) error {
	// If no name was given, use default name pattern
	if len(nodeConfig.Name) == 0 {
		for {
			nodeConfig.Name = fmt.Sprintf("%s%d", defaultNodeNamePrefix, ln.nextNodeSuffix)
			_, ok := ln.nodes[nodeConfig.Name]
			if !ok {
				break
			}
			ln.nextNodeSuffix++
		}
	}
	// Enforce name uniqueness
	// Only paused nodes are enabled to be started with repeated name
	if node, ok := ln.nodes[nodeConfig.Name]; ok && !node.paused {
		return fmt.Errorf("repeated node name %q", nodeConfig.Name)
	}
	return nil
}

type buildArgsReturn struct {
	args      []string
	apiPort   uint16
	p2pPort   uint16
	dataDir   string
	dbDir     string
	logsDir   string
	pluginDir string
	httpHost  string
}

// buildArgs returns the:
// 1) Args for luxd execution
// 2) API port
// 3) P2P port
// of the node being added with config [nodeConfig], config file [configFile],
// and directory at [nodeDir].
// [nodeConfig.Flags] must not be nil
func (ln *localNetwork) buildArgs(
	nodeSemVer string,
	configFile map[string]interface{},
	nodeDir string,
	nodeConfig *node.Config,
) (buildArgsReturn, error) {
	// httpHost from all configs for node
	httpHost, err := getConfigEntry(nodeConfig.Flags, configFile, config.HTTPHostKey, "")
	if err != nil {
		return buildArgsReturn{}, err
	}

	// Tell the node to put all node related data in [nodeDir] unless given in config file
	dataDir, err := getConfigEntry(nodeConfig.Flags, configFile, config.DataDirKey, nodeDir)
	if err != nil {
		return buildArgsReturn{}, err
	}

	// pluginDir from config, default from luxconfig
	pluginDir, err := getConfigEntry(nodeConfig.Flags, configFile, config.PluginDirKey, config.ResolvePluginDir())
	if err != nil {
		return buildArgsReturn{}, err
	}

	// Tell the node to put the database in [dataDir/db] unless given in config file
	dbDir, err := getConfigEntry(nodeConfig.Flags, configFile, config.DBPathKey, filepath.Join(dataDir, defaultDBSubdir))
	if err != nil {
		return buildArgsReturn{}, err
	}

	// Tell the node to put the log directory in [dataDir/logs] unless given in config file
	logsDir, err := getConfigEntry(nodeConfig.Flags, configFile, config.LogsDirKey, filepath.Join(dataDir, defaultLogsSubdir))
	if err != nil {
		return buildArgsReturn{}, err
	}

	// Use random free API port unless given in config file
	apiPort, err := getPort(nodeConfig.Flags, configFile, config.HTTPPortKey, ln.reassignPortsIfUsed)
	if err != nil {
		return buildArgsReturn{}, err
	}

	// Use a random free P2P (staking) port unless given in config file
	// Use random free API port unless given in config file
	p2pPort, err := getPort(nodeConfig.Flags, configFile, config.StakingPortKey, ln.reassignPortsIfUsed)
	if err != nil {
		return buildArgsReturn{}, err
	}

	// Flags for Lux
	flags := map[string]string{
		config.NetworkIDKey:    fmt.Sprintf("%d", ln.networkID),
		config.DataDirKey:      dataDir,
		config.DBPathKey:       dbDir,
		config.LogsDirKey:      logsDir,
		config.PluginDirKey:    pluginDir, // Always pass plugin dir for consistency
		config.HTTPPortKey:     fmt.Sprintf("%d", apiPort),
		config.StakingPortKey:  fmt.Sprintf("%d", p2pPort),
		config.BootstrapIPsKey: ln.bootstraps.IPsArg(),
		config.BootstrapIDsKey: ln.bootstraps.IDsArg(),
	}

	// Write staking key/cert etc. to disk so the new node can use them,
	// and get flag that point the node to those files
	fileFlags, err := writeFiles(ln.networkID, ln.genesis, dataDir, nodeConfig)
	if err != nil {
		return buildArgsReturn{}, err
	}
	for k := range fileFlags {
		flags[k] = fileFlags[k]
	}

	// avoid given these again, as apiPort/p2pPort can be dynamic even if given in nodeConfig
	portFlags := set.Set[string]{
		config.HTTPPortKey:    {},
		config.StakingPortKey: {},
	}

	// Add flags given in node config.
	// Note these will overwrite existing flags if the same flag is given twice.
	for flagName, flagVal := range nodeConfig.Flags {
		if _, ok := warnFlags[flagName]; ok {
			ln.logger.Warn("A provided flag can create conflicts with the runner. The suggestion is to remove this flag", log.String("flag-name", flagName))
		}
		if portFlags.Contains(flagName) {
			continue
		}
		flags[flagName] = fmt.Sprintf("%v", flagVal)
	}

	// map input flags to the corresponding luxd version, making sure that latest flags don't break
	// old luxd versions
	flagsForLuxdVersion := getFlagsForLuxdVersion(nodeSemVer, flags)

	// create args
	args := []string{}
	for k, v := range flagsForLuxdVersion {
		args = append(args, fmt.Sprintf("--%s=%s", k, v))
	}

	return buildArgsReturn{
		args:      args,
		apiPort:   apiPort,
		p2pPort:   p2pPort,
		dataDir:   dataDir,
		dbDir:     dbDir,
		logsDir:   logsDir,
		pluginDir: pluginDir,
		httpHost:  httpHost,
	}, nil
}

// Get Lux version
func (ln *localNetwork) getNodeSemVer(nodeConfig node.Config) (string, error) {
	nodeVersionOutput, err := ln.nodeProcessCreator.GetNodeVersion(nodeConfig)
	if err != nil {
		return "", fmt.Errorf(
			"couldn't get node version with binary %q: %w",
			nodeConfig.BinaryPath, err,
		)
	}
	re := regexp.MustCompile(`\/([^ ]+)`)
	matchs := re.FindStringSubmatch(nodeVersionOutput)
	if len(matchs) != 2 {
		return "", fmt.Errorf(
			"invalid version output %q for binary %q: version pattern not found",
			nodeVersionOutput, nodeConfig.BinaryPath,
		)
	}
	nodeSemVer := "v" + matchs[1]
	return nodeSemVer, nil
}

// ensure flags are compatible with the running node version
func getFlagsForLuxdVersion(luxdVersion string, givenFlags map[string]string) map[string]string {
	flags := maps.Clone(givenFlags)
	for _, deprecatedFlagInfo := range deprecatedFlagsSupport {
		if semver.Compare(luxdVersion, deprecatedFlagInfo.Version) < 0 {
			if v, ok := flags[deprecatedFlagInfo.NewName]; ok {
				if v != "" {
					if deprecatedFlagInfo.ValueMap == "parent-dir" {
						v = filepath.Dir(strings.TrimSuffix(v, "/"))
					}
					flags[deprecatedFlagInfo.OldName] = v
				}
				delete(flags, deprecatedFlagInfo.NewName)
			}
		}
	}
	return flags
}
