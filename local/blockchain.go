// Copyright (C) 2021-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package local

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/luxfi/node/vms/platformvm/reward"

	"github.com/luxfi/node/vms/xvm"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/wallet/chain/x"

	"github.com/luxfi/netrunner/network"
	"github.com/luxfi/netrunner/network/node"
	"github.com/luxfi/netrunner/utils"
	"github.com/luxfi/node/api/admin"
	"github.com/luxfi/node/config"
	"github.com/luxfi/node/genesis"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/crypto/bls/signer/localsigner"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/vms/platformvm"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/secp256k1fx"
	p "github.com/luxfi/node/wallet/chain/p"
	pwallet "github.com/luxfi/node/wallet/chain/p/wallet"
	pbuilder "github.com/luxfi/node/wallet/chain/p/builder"
	psigner "github.com/luxfi/node/wallet/chain/p/signer"
	xbuilder "github.com/luxfi/node/wallet/chain/x/builder"
	xsigner "github.com/luxfi/node/wallet/chain/x/signer"
	walletpkg "github.com/luxfi/node/wallet"
	"github.com/luxfi/node/wallet/subnet/primary"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"
)

const (
	// offset of validation start from current time
	validationStartOffset               = 20 * time.Second
	permissionlessValidationStartOffset = 30 * time.Second
	// duration for primary network validators
	validationDuration = 365 * 24 * time.Hour
	// weight assigned to subnet validators
	subnetValidatorsWeight = 1000
	// check period for blockchain logs while waiting for custom chains to be ready
	blockchainLogPullFrequency = time.Second
	// check period while waiting for all validators to be ready
	waitForValidatorsPullFrequency = time.Second
	defaultTimeout                 = time.Minute
	stakingMinimumLeadTime         = 25 * time.Second
	minStakeDuration               = 24 * 14 * time.Hour
)

var (
	errAborted  = errors.New("aborted")
	defaultPoll = walletpkg.WithPollFrequency(100 * time.Millisecond)
)

type blockchainInfo struct {
	chainName    string
	vmID         ids.ID
	subnetID     ids.ID
	blockchainID ids.ID
}

// get node with minimum port number
func (ln *localNetwork) getNode() node.Node {
	var node node.Node
	minAPIPortNumber := uint16(MaxPort)
	for _, n := range ln.nodes {
		if n.paused {
			continue
		}
		if n.GetAPIPort() < minAPIPortNumber {
			minAPIPortNumber = n.GetAPIPort()
			node = n
		}
	}
	return node
}

// get node client URI for an arbitrary node in the network
func (ln *localNetwork) getClientURI() (string, error) { //nolint
	node := ln.getNode()
	clientURI := fmt.Sprintf("http://%s:%d", node.GetURL(), node.GetAPIPort())
	ln.log.Info("getClientURI",
		zap.String("nodeName", node.GetName()),
		zap.String("uri", clientURI))
	return clientURI, nil
}

func (ln *localNetwork) CreateBlockchains(
	ctx context.Context,
	chainSpecs []network.BlockchainSpec, // VM name + genesis bytes
) ([]ids.ID, error) {
	ln.lock.Lock()
	defer ln.lock.Unlock()

	chainInfos, err := ln.installCustomChains(ctx, chainSpecs)
	if err != nil {
		return nil, err
	}

	if err := ln.waitForCustomChainsReady(ctx, chainInfos); err != nil {
		return nil, err
	}

	if err := ln.RegisterBlockchainAliases(ctx, chainInfos, chainSpecs); err != nil {
		return nil, err
	}

	chainIDs := []ids.ID{}
	for _, chainInfo := range chainInfos {
		chainIDs = append(chainIDs, chainInfo.blockchainID)
	}

	return chainIDs, nil
}

// if alias is defined in blockchain-specs, registers an alias for the previously created blockchain
func (ln *localNetwork) RegisterBlockchainAliases(
	ctx context.Context,
	chainInfos []blockchainInfo,
	chainSpecs []network.BlockchainSpec,
) error {
	fmt.Println()
	ln.log.Info(logging.Blue.Wrap(logging.Bold.Wrap("registering blockchain aliases")))
	for i, chainSpec := range chainSpecs {
		if chainSpec.BlockchainAlias == "" {
			continue
		}
		blockchainAlias := chainSpec.BlockchainAlias
		chainID := chainInfos[i].blockchainID.String()
		ln.log.Info("registering blockchain alias",
			zap.String("alias", blockchainAlias),
			zap.String("chain-id", chainID))
		for nodeName, node := range ln.nodes {
			if node.paused {
				continue
			}
			if err := node.client.AdminAPI().AliasChain(ctx, chainID, blockchainAlias); err != nil {
				return fmt.Errorf("failure to register blockchain alias %v on node %v: %w", blockchainAlias, nodeName, err)
			}
		}
	}
	return nil
}

func (ln *localNetwork) RemoveSubnetValidators(
	ctx context.Context,
	removeSubnetSpecs []network.RemoveSubnetValidatorSpec,
) error {
	ln.lock.Lock()
	defer ln.lock.Unlock()

	return ln.removeSubnetValidators(ctx, removeSubnetSpecs)
}

func (ln *localNetwork) AddPermissionlessValidators(
	ctx context.Context,
	validatorSpec []network.PermissionlessValidatorSpec,
) error {
	ln.lock.Lock()
	defer ln.lock.Unlock()

	return ln.addPermissionlessValidators(ctx, validatorSpec)
}

func (ln *localNetwork) TransformSubnet(
	ctx context.Context,
	elasticSubnetConfig []network.ElasticSubnetSpec,
) ([]ids.ID, []ids.ID, error) {
	ln.lock.Lock()
	defer ln.lock.Unlock()

	return ln.transformToElasticSubnets(ctx, elasticSubnetConfig)
}

func (ln *localNetwork) CreateSubnets(
	ctx context.Context,
	subnetSpecs []network.SubnetSpec,
) ([]ids.ID, error) {
	ln.lock.Lock()
	defer ln.lock.Unlock()

	return ln.installSubnets(ctx, subnetSpecs)
}

// provisions local cluster and install custom chains if applicable
// assumes the local cluster is already set up and healthy
func (ln *localNetwork) installCustomChains(
	ctx context.Context,
	chainSpecs []network.BlockchainSpec,
) ([]blockchainInfo, error) {
	fmt.Println()
	ln.log.Info(logging.Blue.Wrap(logging.Bold.Wrap("create and install custom chains")))

	clientURI, err := ln.getClientURI()
	if err != nil {
		return nil, err
	}
	platformCli := platformvm.NewClient(clientURI)

	// wallet needs txs for all previously created subnets
	var preloadTXs []ids.ID
	for _, chainSpec := range chainSpecs {
		// if subnet id for the blockchain is specified, we need to add the subnet id
		// tx info to the wallet so blockchain creation does not fail
		// if subnet id is not specified, a new subnet will later be created by using the wallet,
		// and the wallet will obtain the tx info at that moment
		if chainSpec.SubnetID != nil {
			subnetID, err := ids.FromString(*chainSpec.SubnetID)
			if err != nil {
				return nil, err
			}
			preloadTXs = append(preloadTXs, subnetID)
		}
	}

	w, err := newWallet(ctx, clientURI, preloadTXs)
	if err != nil {
		return nil, err
	}

	// get subnet specs for all new subnets to create
	// for the list of requested blockchains, we take those that have undefined subnet id
	// and use the provided subnet spec. if not given, use an empty default subnet spec
	// that subnets will be created and later on assigned to the blockchain requests
	subnetSpecs := []network.SubnetSpec{}
	for _, chainSpec := range chainSpecs {
		if chainSpec.SubnetID == nil {
			if chainSpec.SubnetSpec == nil {
				subnetSpecs = append(subnetSpecs, network.SubnetSpec{})
			} else {
				subnetSpecs = append(subnetSpecs, *chainSpec.SubnetSpec)
			}
		}
	}

	// if no participants are given for a new subnet, assume all nodes should be participants
	allNodeNames := maps.Keys(ln.nodes)
	sort.Strings(allNodeNames)
	for i := range subnetSpecs {
		if len(subnetSpecs[i].Participants) == 0 {
			subnetSpecs[i].Participants = allNodeNames
		}
	}

	// create new nodes
	for _, subnetSpec := range subnetSpecs {
		for _, nodeName := range subnetSpec.Participants {
			_, ok := ln.nodes[nodeName]
			if !ok {
				ln.log.Info(logging.Green.Wrap(fmt.Sprintf("adding new participant %s", nodeName)))
				if _, err := ln.addNode(node.Config{Name: nodeName}); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := ln.healthy(ctx); err != nil {
		return nil, err
	}

	// just ensure all nodes are primary validators (so can be subnet validators)
	if err := ln.addPrimaryValidators(ctx, platformCli, w); err != nil {
		return nil, err
	}

	// create missing subnets
	subnetIDs, err := createSubnets(ctx, uint32(len(subnetSpecs)), w, ln.log)
	if err != nil {
		return nil, err
	}

	if err := ln.setSubnetConfigFiles(subnetIDs, subnetSpecs); err != nil {
		return nil, err
	}

	// assign created subnets to blockchain requests with undefined subnet id
	j := 0
	for i := range chainSpecs {
		if chainSpecs[i].SubnetID == nil {
			subnetIDStr := subnetIDs[j].String()
			chainSpecs[i].SubnetID = &subnetIDStr
			j++
		}
	}

	// wait for nodes to be primary validators before trying to add them as subnet ones
	if err = ln.waitPrimaryValidators(ctx, platformCli); err != nil {
		return nil, err
	}

	if err = ln.addSubnetValidators(ctx, platformCli, w, subnetIDs, subnetSpecs); err != nil {
		return nil, err
	}

	blockchainTxs, err := createBlockchainTxs(ctx, chainSpecs, w, ln.log)
	if err != nil {
		return nil, err
	}

	nodesToRestartForBlockchainConfigUpdate, err := ln.setBlockchainConfigFiles(ctx, chainSpecs, blockchainTxs, subnetIDs, subnetSpecs, ln.log)
	if err != nil {
		return nil, err
	}

	if len(subnetSpecs) > 0 || len(nodesToRestartForBlockchainConfigUpdate) > 0 {
		// we need to restart if there are new subnets or if there are new network config files
		// add missing subnets, restarting network and waiting for subnet validation to start
		if err := ln.restartNodes(ctx, subnetIDs, subnetSpecs, nil, nil, nodesToRestartForBlockchainConfigUpdate); err != nil {
			return nil, err
		}
		clientURI, err = ln.getClientURI()
		if err != nil {
			return nil, err
		}
		w.reload(clientURI)
	}

	// refresh vm list
	if err := ln.reloadVMPlugins(ctx); err != nil {
		return nil, err
	}

	if err = ln.waitSubnetValidators(ctx, platformCli, subnetIDs, subnetSpecs); err != nil {
		return nil, err
	}

	// create blockchain from txs before spending more utxos
	if err := ln.createBlockchains(ctx, chainSpecs, blockchainTxs, w, ln.log); err != nil {
		return nil, err
	}

	chainInfos := make([]blockchainInfo, len(chainSpecs))
	for i, chainSpec := range chainSpecs {
		vmID, err := utils.VMID(chainSpec.VMName)
		if err != nil {
			return nil, err
		}
		subnetID, err := ids.FromString(*chainSpec.SubnetID)
		if err != nil {
			return nil, err
		}
		chainInfos[i] = blockchainInfo{
			// we keep a record of VM name in blockchain name field,
			// as there is no way to recover VM name from VM ID
			chainName:    chainSpec.VMName,
			vmID:         vmID,
			subnetID:     subnetID,
			blockchainID: blockchainTxs[i].ID(),
		}
	}

	return chainInfos, nil
}

func (ln *localNetwork) installSubnets(
	ctx context.Context,
	subnetSpecs []network.SubnetSpec,
) ([]ids.ID, error) {
	fmt.Println()
	ln.log.Info(logging.Blue.Wrap(logging.Bold.Wrap("create subnets")))

	clientURI, err := ln.getClientURI()
	if err != nil {
		return nil, err
	}
	platformCli := platformvm.NewClient(clientURI)

	w, err := newWallet(ctx, clientURI, []ids.ID{})
	if err != nil {
		return nil, err
	}

	// if no participants are given, assume all nodes should be participants
	allNodeNames := maps.Keys(ln.nodes)
	sort.Strings(allNodeNames)
	for i := range subnetSpecs {
		if len(subnetSpecs[i].Participants) == 0 {
			subnetSpecs[i].Participants = allNodeNames
		}
	}

	// create new nodes
	for _, subnetSpec := range subnetSpecs {
		for _, nodeName := range subnetSpec.Participants {
			_, ok := ln.nodes[nodeName]
			if !ok {
				ln.log.Info(logging.Green.Wrap(fmt.Sprintf("adding new participant %s", nodeName)))
				if _, err := ln.addNode(node.Config{Name: nodeName}); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := ln.healthy(ctx); err != nil {
		return nil, err
	}

	// just ensure all nodes are primary validators (so can be subnet validators)
	if err := ln.addPrimaryValidators(ctx, platformCli, w); err != nil {
		return nil, err
	}

	subnetIDs, err := createSubnets(ctx, uint32(len(subnetSpecs)), w, ln.log)
	if err != nil {
		return nil, err
	}

	if err := ln.setSubnetConfigFiles(subnetIDs, subnetSpecs); err != nil {
		return nil, err
	}

	// wait for nodes to be primary validators before trying to add them as subnet ones
	if err = ln.waitPrimaryValidators(ctx, platformCli); err != nil {
		return nil, err
	}

	if err = ln.addSubnetValidators(ctx, platformCli, w, subnetIDs, subnetSpecs); err != nil {
		return nil, err
	}

	if err := ln.restartNodes(ctx, subnetIDs, subnetSpecs, nil, nil, nil); err != nil {
		return nil, err
	}

	if err = ln.waitSubnetValidators(ctx, platformCli, subnetIDs, subnetSpecs); err != nil {
		return nil, err
	}

	return subnetIDs, nil
}

func (ln *localNetwork) getSubnetValidatorsNodenames(
	ctx context.Context,
	subnetID ids.ID,
) ([]string, error) {
	clientURI, err := ln.getClientURI()
	if err != nil {
		return nil, err
	}
	platformCli := platformvm.NewClient(clientURI)

	cctx, cancel := createDefaultCtx(ctx)
	vs, err := platformCli.GetCurrentValidators(cctx, subnetID, nil)
	cancel()
	if err != nil {
		return nil, err
	}
	nodeNames := []string{}
	for _, v := range vs {
		for nodeName, node := range ln.nodes {
			if v.NodeID == node.GetNodeID() {
				nodeNames = append(nodeNames, nodeName)
			}
		}
	}
	if len(nodeNames) != len(vs) {
		return nil, fmt.Errorf("not all validators for subnet %s are present in network", subnetID.String())
	}
	return nodeNames, nil
}

func (ln *localNetwork) waitForCustomChainsReady(
	ctx context.Context,
	chainInfos []blockchainInfo,
) error {
	fmt.Println()
	ln.log.Info(logging.Blue.Wrap(logging.Bold.Wrap("waiting for custom chains to report healthy...")))

	if err := ln.healthy(ctx); err != nil {
		return err
	}

	for _, chainInfo := range chainInfos {
		nodeNames, err := ln.getSubnetValidatorsNodenames(ctx, chainInfo.subnetID)
		if err != nil {
			return err
		}

		for _, nodeName := range nodeNames {
			node := ln.nodes[nodeName]
			if node.paused {
				continue
			}
			ln.log.Info("inspecting node log directory for custom chain logs", zap.String("log-dir", node.GetLogsDir()), zap.String("node-name", nodeName))
			p := filepath.Join(node.GetLogsDir(), chainInfo.blockchainID.String()+".log")
			ln.log.Info("checking log",
				zap.String("vm-ID", chainInfo.vmID.String()),
				zap.String("subnet-ID", chainInfo.subnetID.String()),
				zap.String("blockchain-ID", chainInfo.blockchainID.String()),
				zap.String("path", p),
			)
			for {
				if _, err := os.Stat(p); err == nil {
					ln.log.Info("found the log", zap.String("path", p))
					break
				}
				ln.log.Info("log not found yet, retrying...",
					zap.String("vm-ID", chainInfo.vmID.String()),
					zap.String("subnet-ID", chainInfo.subnetID.String()),
					zap.String("blockchain-ID", chainInfo.blockchainID.String()),
				)
				select {
				case <-ln.onStopCh:
					return errAborted
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(blockchainLogPullFrequency):
				}
			}
		}
	}

	fmt.Println()
	ln.log.Info(logging.Green.Wrap("all custom chains are running!!!"))

	fmt.Println()
	ln.log.Info(logging.Green.Wrap(logging.Bold.Wrap("all custom chains are ready on RPC server-side -- network-runner RPC client can poll and query the cluster status")))

	return nil
}

func (ln *localNetwork) restartNodes(
	ctx context.Context,
	subnetIDs []ids.ID,
	subnetSpecs []network.SubnetSpec,
	validatorSpecs []network.PermissionlessValidatorSpec,
	removeValidatorSpecs []network.RemoveSubnetValidatorSpec,
	nodesToRestartForBlockchainConfigUpdate set.Set[string],
) (err error) {
	if (subnetSpecs != nil && validatorSpecs != nil) || (subnetSpecs != nil && removeValidatorSpecs != nil) ||
		(validatorSpecs != nil && removeValidatorSpecs != nil) {
		return errors.New("only one type of spec between subnet specs, add permissionless validator specs and " +
			"remove validator specs can be supplied at one time")
	}
	fmt.Println()
	ln.log.Info(logging.Blue.Wrap(logging.Bold.Wrap("restarting network")))

	nodeNames := maps.Keys(ln.nodes)
	sort.Strings(nodeNames)

	for _, nodeName := range nodeNames {
		node := ln.nodes[nodeName]

		// delete node specific flag so as to use default one
		nodeConfig := node.GetConfig()

		previousTrackedSubnets := ""
		previousTrackedSubnetsIntf, ok := nodeConfig.Flags[config.TrackSubnetsKey]
		if ok {
			previousTrackedSubnets, ok = previousTrackedSubnetsIntf.(string)
			if !ok {
				return fmt.Errorf("expected node config %s to have type string obtained %T", config.TrackSubnetsKey, previousTrackedSubnetsIntf)
			}
		}

		trackSubnetIDsSet := set.Set[string]{}
		if previousTrackedSubnets != "" {
			for _, s := range strings.Split(previousTrackedSubnets, ",") {
				trackSubnetIDsSet.Add(s)
			}
		}
		needsRestart := false
		for _, validatorSpec := range validatorSpecs {
			if validatorSpec.NodeName == node.name {
				trackSubnetIDsSet.Add(validatorSpec.SubnetID)
				needsRestart = true
			}
		}

		for _, removeValidatorSpec := range removeValidatorSpecs {
			for _, toRemoveNode := range removeValidatorSpec.NodeNames {
				if toRemoveNode == node.name {
					trackSubnetIDsSet.Remove(removeValidatorSpec.SubnetID)
					needsRestart = true
				}
			}
		}

		for i, subnetID := range subnetIDs {
			for _, participant := range subnetSpecs[i].Participants {
				if participant == nodeName {
					trackSubnetIDsSet.Add(subnetID.String())
					needsRestart = true
				}
			}
		}

		trackSubnetIDs := trackSubnetIDsSet.List()
		sort.Strings(trackSubnetIDs)

		tracked := strings.Join(trackSubnetIDs, ",")
		nodeConfig.Flags[config.TrackSubnetsKey] = tracked

		if subnetSpecs != nil {
			if nodesToRestartForBlockchainConfigUpdate.Contains(nodeName) {
				needsRestart = true
			}
		}

		if !needsRestart {
			continue
		}

		if node.paused {
			continue
		}

		if removeValidatorSpecs != nil {
			ln.log.Info(logging.Green.Wrap(fmt.Sprintf("restarting node %s to stop tracking subnets %s", nodeName, tracked)))
		} else {
			ln.log.Info(logging.Green.Wrap(fmt.Sprintf("restarting node %s to track subnets %s", nodeName, tracked)))
		}

		if err := ln.restartNode(ctx, nodeName, "", "", "", nil, nil, nil); err != nil {
			return err
		}
	}
	if err := ln.healthy(ctx); err != nil {
		return err
	}
	return nil
}

type wallet struct {
	addr        ids.ShortID
	pWallet     pwallet.Wallet
	pBackend    pwallet.Backend
	pBuilder    pbuilder.Builder
	pSigner     psigner.Signer
	xWallet     x.Wallet
	xChainID    ids.ID
	luxAssetID  ids.ID
}

func newWallet(
	ctx context.Context,
	uri string,
	preloadTXs []ids.ID,
) (*wallet, error) {
	kc := secp256k1fx.NewKeychain(genesis.EWOQKey)
	luxState, err := primary.FetchState(ctx, uri, kc.Addresses())
	if err != nil {
		return nil, err
	}
	pClient := platformvm.NewClient(uri)
	pTXs := make(map[ids.ID]*txs.Tx)
	for _, id := range preloadTXs {
		txBytes, err := pClient.GetTx(ctx, id)
		if err != nil {
			return nil, err
		}
		tx, err := txs.Parse(txs.Codec, txBytes)
		if err != nil {
			return nil, err
		}
		pTXs[id] = tx
	}
	pUTXOs := walletpkg.NewChainUTXOs(constants.PlatformChainID, luxState.UTXOs)
	xChainID := luxState.XCTX.BlockchainID
	xUTXOs := walletpkg.NewChainUTXOs(xChainID, luxState.UTXOs)
	var w wallet
	w.addr = genesis.EWOQKey.PublicKey().Address()
	// TODO: Create owners map instead of pTXs
	owners := make(map[ids.ID]fx.Owner)
	w.pBackend = pwallet.NewBackend(pUTXOs, owners)
	w.pBuilder = pbuilder.New(kc.Addresses(), luxState.PCTX, w.pBackend)
	w.pSigner = psigner.New(kc, w.pBackend)
	pWalletClient := p.NewClient(pClient, w.pBackend)
	w.pWallet = pwallet.New(pWalletClient, w.pBuilder, w.pSigner)

	xBackend := x.NewBackend(luxState.XCTX, xUTXOs)
	xBuilder := xbuilder.New(kc.Addresses(), luxState.XCTX, xBackend)
	xSigner := xsigner.New(kc, xBackend)
	xClient := xvm.NewClient(uri, "X")
	w.xWallet = x.NewWallet(xBuilder, xSigner, xClient, xBackend)
	w.xChainID = xChainID
	w.luxAssetID = luxState.PCTX.LUXAssetID
	return &w, nil
}

func (w *wallet) reload(uri string) {
	pClient := platformvm.NewClient(uri)
	pWalletClient := p.NewClient(pClient, w.pBackend)
	w.pWallet = pwallet.New(pWalletClient, w.pBuilder, w.pSigner)
}

// add all nodes as validators of the primary network, in case they are not
// the validation starts as soon as possible and its duration is as long as possible, that is,
// it is set to max accepted duration by node
func (ln *localNetwork) addPrimaryValidators(
	ctx context.Context,
	platformCli *platformvm.Client,
	w *wallet,
) error {
	ln.log.Info(logging.Green.Wrap("adding the nodes as primary network validators"))
	// ref. https://docs.lux.network/build/node-apis/p-chain/#platformgetcurrentvalidators
	cctx, cancel := createDefaultCtx(ctx)
	vdrs, err := platformCli.GetCurrentValidators(cctx, constants.PrimaryNetworkID, nil)
	cancel()
	if err != nil {
		return err
	}
	curValidators := set.Set[ids.NodeID]{}
	for _, v := range vdrs {
		curValidators.Add(v.NodeID)
	}
	for nodeName, node := range ln.nodes {
		nodeID := node.GetNodeID()

		if curValidators.Contains(nodeID) {
			continue
		}

		// Prepare node BLS PoP
		// It is important to note that this will ONLY register BLS signers for
		// nodes registered AFTER genesis.
		blsKeyBytes, err := base64.StdEncoding.DecodeString(node.GetConfig().StakingSigningKey)
		if err != nil {
			return err
		}
		blsSigner, err := localsigner.FromBytes(blsKeyBytes)
		if err != nil {
			return err
		}
		proofOfPossession, err := signer.NewProofOfPossession(blsSigner)
		if err != nil {
			return err
		}
		cctx, cancel = createDefaultCtx(ctx)
		tx, err := w.pWallet.IssueAddPermissionlessValidatorTx(
			&txs.SubnetValidator{
				Validator: txs.Validator{
					NodeID: nodeID,
					Start:  uint64(time.Now().Add(validationStartOffset).Unix()),
					End:    uint64(time.Now().Add(validationDuration).Unix()),
					Wght:   genesis.LocalParams.MinValidatorStake,
				},
				Subnet: ids.Empty,
			},
			proofOfPossession,
			w.luxAssetID,
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{w.addr},
			},
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{w.addr},
			},
			10*10000, // 10% fee percent, times 10000 to make it as shares
			walletpkg.WithContext(cctx),
		)
		cancel()
		if err != nil {
			return fmt.Errorf("P-Wallet Tx Error %s %w, node ID %s", "IssueAddPermissionlessValidatorTx", err, nodeID.String())
		}
		ln.log.Info("added node as primary subnet validator", zap.String("node-name", nodeName), zap.String("node-ID", nodeID.String()), zap.String("tx-ID", tx.ID().String()))
	}
	return nil
}

func getXChainAssetID(ctx context.Context, w *wallet, tokenName string, tokenSymbol string, maxSupply uint64) (ids.ID, error) {
	owner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			w.addr,
		},
	}
	cctx, cancel := createDefaultCtx(ctx)
	defer cancel()
	tx, err := w.xWallet.IssueCreateAssetTx(
		tokenName,
		tokenSymbol,
		9, // denomination for UI purposes only in explorer
		map[uint32][]verify.State{
			0: {
				&secp256k1fx.TransferOutput{
					Amt:          maxSupply,
					OutputOwners: *owner,
				},
			},
		},
		walletpkg.WithContext(cctx),
		defaultPoll,
	)
	if err != nil {
		return ids.Empty, err
	}
	return tx.ID(), nil
}

func exportXChainToPChain(ctx context.Context, w *wallet, owner *secp256k1fx.OutputOwners, subnetAssetID ids.ID, assetAmount uint64) error {
	cctx, cancel := createDefaultCtx(ctx)
	defer cancel()
	_, err := w.xWallet.IssueExportTx(
		ids.Empty,
		[]*lux.TransferableOutput{
			{
				Asset: lux.Asset{
					ID: subnetAssetID,
				},
				Out: &secp256k1fx.TransferOutput{
					Amt:          assetAmount,
					OutputOwners: *owner,
				},
			},
		},
		walletpkg.WithContext(cctx),
		defaultPoll,
	)
	return err
}

func importPChainFromXChain(ctx context.Context, w *wallet, owner *secp256k1fx.OutputOwners, xChainID ids.ID) error {
	pWallet := w.pWallet
	cctx, cancel := createDefaultCtx(ctx)
	defer cancel()
	_, err := pWallet.IssueImportTx(
		xChainID,
		owner,
		walletpkg.WithContext(cctx),
		defaultPoll,
	)
	return err
}

func (ln *localNetwork) removeSubnetValidators(
	ctx context.Context,
	removeSubnetSpecs []network.RemoveSubnetValidatorSpec,
) error {
	ln.log.Info("removing subnet validator tx")
	removeSubnetSpecIDs := make([]ids.ID, len(removeSubnetSpecs))
	clientURI, err := ln.getClientURI()
	if err != nil {
		return err
	}
	platformCli := platformvm.NewClient(clientURI)
	// wallet needs txs for all previously created subnets
	preloadTXs := make([]ids.ID, len(removeSubnetSpecs))
	for i, removeSubnetSpec := range removeSubnetSpecs {
		subnetID, err := ids.FromString(removeSubnetSpec.SubnetID)
		if err != nil {
			return err
		}
		preloadTXs[i] = subnetID
	}
	w, err := newWallet(ctx, clientURI, preloadTXs)
	if err != nil {
		return err
	}
	ln.log.Info(logging.Green.Wrap("removing the nodes as subnet validators"))
	for i, subnetSpec := range removeSubnetSpecs {
		subnetID, err := ids.FromString(subnetSpec.SubnetID)
		if err != nil {
			return err
		}
		cctx, cancel := createDefaultCtx(ctx)
		vs, err := platformCli.GetCurrentValidators(cctx, subnetID, nil)
		cancel()
		if err != nil {
			return err
		}
		subnetValidators := set.Set[ids.NodeID]{}
		for _, v := range vs {
			subnetValidators.Add(v.NodeID)
		}
		toRemoveNodes := subnetSpec.NodeNames
		for _, nodeName := range toRemoveNodes {
			node, b := ln.nodes[nodeName]
			if !b {
				return fmt.Errorf("node %s is not in network nodes", nodeName)
			}
			nodeID := node.GetNodeID()
			if isValidator := subnetValidators.Contains(nodeID); !isValidator {
				return fmt.Errorf("node %s is currently not a subnet validator of subnet %s", nodeName, subnetID.String())
			}
			cctx, cancel := createDefaultCtx(ctx)
			tx, err := w.pWallet.IssueRemoveSubnetValidatorTx(
				nodeID,
				subnetID,
				walletpkg.WithContext(cctx),
				defaultPoll,
			)
			cancel()
			if err != nil {
				return fmt.Errorf("P-Wallet Tx Error %s %w, node ID %s, subnetID %s", "IssueRemoveSubnetValidatorTx", err, nodeID.String(), subnetID.String())
			}
			ln.log.Info("removed node as subnet validator",
				zap.String("node-name", nodeName),
				zap.String("node-ID", nodeID.String()),
				zap.String("subnet-ID", subnetID.String()),
				zap.String("tx-ID", tx.ID().String()),
			)
			removeSubnetSpecIDs[i] = tx.ID()
		}
	}
	return ln.restartNodes(ctx, nil, nil, nil, removeSubnetSpecs, nil)
}

func (ln *localNetwork) addPermissionlessValidators(
	ctx context.Context,
	validatorSpecs []network.PermissionlessValidatorSpec,
) error {
	ln.log.Info("adding permissionless validator tx")
	clientURI, err := ln.getClientURI()
	if err != nil {
		return err
	}
	platformCli := platformvm.NewClient(clientURI)
	// wallet needs txs for all previously created subnets
	preloadTXs := make([]ids.ID, len(validatorSpecs))
	for i, validatorSpec := range validatorSpecs {
		subnetID, err := ids.FromString(validatorSpec.SubnetID)
		if err != nil {
			return err
		}
		preloadTXs[i] = subnetID
	}
	w, err := newWallet(ctx, clientURI, preloadTXs)
	if err != nil {
		return err
	}
	owner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			w.addr,
		},
	}
	// create new nodes
	for _, validatorSpec := range validatorSpecs {
		_, ok := ln.nodes[validatorSpec.NodeName]
		if !ok {
			ln.log.Info(logging.Green.Wrap(fmt.Sprintf("adding new participant %s", validatorSpec.NodeName)))
			if _, err := ln.addNode(node.Config{Name: validatorSpec.NodeName}); err != nil {
				return err
			}
		}
	}
	if err := ln.healthy(ctx); err != nil {
		return err
	}

	// just ensure all nodes are primary validators (so can be subnet validators)
	if err := ln.addPrimaryValidators(ctx, platformCli, w); err != nil {
		return err
	}

	// wait for nodes to be primary validators before trying to add them as subnet ones
	if err = ln.waitPrimaryValidators(ctx, platformCli); err != nil {
		return err
	}

	cctx, cancel := createDefaultCtx(ctx)
	vs, err := platformCli.GetCurrentValidators(cctx, constants.PrimaryNetworkID, nil)
	cancel()
	if err != nil {
		return err
	}
	primaryValidatorsEndtime := make(map[ids.NodeID]time.Time)
	for _, v := range vs {
		primaryValidatorsEndtime[v.NodeID] = time.Unix(int64(v.EndTime), 0)
	}

	for _, validatorSpec := range validatorSpecs {
		ln.log.Info(logging.Green.Wrap("adding permissionless validator"), zap.String("node ", validatorSpec.NodeName))
		cctx, cancel := createDefaultCtx(ctx)
		validatorNodeID := ln.nodes[validatorSpec.NodeName].nodeID
		subnetID, err := ids.FromString(validatorSpec.SubnetID)
		if err != nil {
			return err
		}
		assetID, err := ids.FromString(validatorSpec.AssetID)
		if err != nil {
			return err
		}
		var startTime uint64
		var endTime uint64
		if validatorSpec.StartTime.IsZero() {
			startTime = uint64(time.Now().Add(permissionlessValidationStartOffset).Unix())
		} else {
			startTime = uint64(validatorSpec.StartTime.Unix())
		}

		if validatorSpec.StakeDuration == 0 {
			endTime = uint64(primaryValidatorsEndtime[validatorNodeID].Unix())
		} else {
			endTime = uint64(validatorSpec.StartTime.Add(validatorSpec.StakeDuration).Unix())
		}
		tx, err := w.pWallet.IssueAddPermissionlessValidatorTx(
			&txs.SubnetValidator{
				Validator: txs.Validator{
					NodeID: validatorNodeID,
					Start:  startTime,
					End:    endTime,
					Wght:   validatorSpec.StakedAmount,
				},
				Subnet: subnetID,
			},
			&signer.Empty{},
			assetID,
			owner,
			&secp256k1fx.OutputOwners{},
			reward.PercentDenominator,
			walletpkg.WithContext(cctx),
			defaultPoll,
		)
		cancel()
		if err != nil {
			return err
		}
		ln.log.Info("Validator successfully added as permissionless validator", zap.String("TX ID", tx.ID().String()))
	}
	return ln.restartNodes(ctx, nil, nil, validatorSpecs, nil, nil)
}

func (ln *localNetwork) transformToElasticSubnets(
	ctx context.Context,
	elasticSubnetSpecs []network.ElasticSubnetSpec,
) ([]ids.ID, []ids.ID, error) {
	ln.log.Info("transforming elastic subnet tx")
	elasticSubnetIDs := make([]ids.ID, len(elasticSubnetSpecs))
	assetIDs := make([]ids.ID, len(elasticSubnetSpecs))
	clientURI, err := ln.getClientURI()
	if err != nil {
		return nil, nil, err
	}
	// wallet needs txs for all previously created subnets
	var preloadTXs []ids.ID
	for _, elasticSubnetSpec := range elasticSubnetSpecs {
		if elasticSubnetSpec.SubnetID == nil {
			return nil, nil, errors.New("elastic subnet spec has no subnet ID")
		} else {
			subnetID, err := ids.FromString(*elasticSubnetSpec.SubnetID)
			if err != nil {
				return nil, nil, err
			}
			preloadTXs = append(preloadTXs, subnetID)
		}
	}
	w, err := newWallet(ctx, clientURI, preloadTXs)
	if err != nil {
		return nil, nil, err
	}

	for i, elasticSubnetSpec := range elasticSubnetSpecs {
		ln.log.Info(logging.Green.Wrap("transforming elastic subnet"), zap.String("subnet ID", *elasticSubnetSpec.SubnetID))

		subnetAssetID, err := getXChainAssetID(ctx, w, elasticSubnetSpec.AssetName, elasticSubnetSpec.AssetSymbol, elasticSubnetSpec.MaxSupply)
		if err != nil {
			return nil, nil, err
		}
		assetIDs[i] = subnetAssetID
		ln.log.Info("created asset ID", zap.String("asset-ID", subnetAssetID.String()))
		owner := &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				w.addr,
			},
		}
		err = exportXChainToPChain(ctx, w, owner, subnetAssetID, elasticSubnetSpec.MaxSupply)
		if err != nil {
			return nil, nil, err
		}
		ln.log.Info("exported asset to P-Chain")
		err = importPChainFromXChain(ctx, w, owner, w.xChainID)
		if err != nil {
			return nil, nil, err
		}
		ln.log.Info("imported asset from X-Chain")
		subnetID, err := ids.FromString(*elasticSubnetSpec.SubnetID)
		if err != nil {
			return nil, nil, err
		}
		cctx, cancel := createDefaultCtx(ctx)
		transformSubnetTx, err := w.pWallet.IssueTransformSubnetTx(subnetID, subnetAssetID,
			elasticSubnetSpec.InitialSupply, elasticSubnetSpec.MaxSupply, elasticSubnetSpec.MinConsumptionRate,
			elasticSubnetSpec.MaxConsumptionRate, elasticSubnetSpec.MinValidatorStake, elasticSubnetSpec.MaxValidatorStake,
			elasticSubnetSpec.MinStakeDuration, elasticSubnetSpec.MaxStakeDuration, elasticSubnetSpec.MinDelegationFee,
			elasticSubnetSpec.MinDelegatorStake, elasticSubnetSpec.MaxValidatorWeightFactor, elasticSubnetSpec.UptimeRequirement,
			walletpkg.WithContext(cctx),
			defaultPoll,
		)
		cancel()
		if err != nil {
			return nil, nil, err
		}
		ln.log.Info("Subnet transformed into elastic subnet", zap.String("TX ID", transformSubnetTx.ID().String()))
		elasticSubnetIDs[i] = transformSubnetTx.ID()
		ln.subnetID2ElasticSubnetID[subnetID] = transformSubnetTx.ID()
	}
	return elasticSubnetIDs, assetIDs, nil
}

func (ln *localNetwork) GetElasticSubnetID(_ context.Context, subnetID ids.ID) (ids.ID, error) {
	elasticSubnetID, ok := ln.subnetID2ElasticSubnetID[subnetID]
	if !ok {
		return ids.Empty, fmt.Errorf("subnetID not found on map: %s", subnetID)
	}
	return elasticSubnetID, nil
}

func createSubnets(
	ctx context.Context,
	numSubnets uint32,
	w *wallet,
	log logging.Logger,
) ([]ids.ID, error) {
	fmt.Println()
	log.Info(logging.Green.Wrap("creating subnets"), zap.Uint32("num-subnets", numSubnets))
	subnetIDs := make([]ids.ID, numSubnets)
	for i := uint32(0); i < numSubnets; i++ {
		log.Info("creating subnet tx")
		cctx, cancel := createDefaultCtx(ctx)
		tx, err := w.pWallet.IssueCreateSubnetTx(
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{w.addr},
			},
			walletpkg.WithContext(cctx),
			defaultPoll,
		)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("P-Wallet Tx Error %s %w", "IssueCreateSubnetTx", err)
		}
		// Get the subnet ID from the transaction
		subnetID := tx.ID()
		log.Info("created subnet tx", zap.String("subnet-ID", subnetID.String()))
		subnetIDs[i] = subnetID
	}
	return subnetIDs, nil
}

// add the nodes in subnet participant as validators of the given subnets, in case they are not
// the validation starts as soon as possible and its duration is as long as possible, that is,
// it ends at the time the primary network validation ends for the node
func (ln *localNetwork) addSubnetValidators(
	ctx context.Context,
	platformCli *platformvm.Client,
	w *wallet,
	subnetIDs []ids.ID,
	subnetSpecs []network.SubnetSpec,
) error {
	ln.log.Info(logging.Green.Wrap("adding the nodes as subnet validators"))
	for i, subnetID := range subnetIDs {
		cctx, cancel := createDefaultCtx(ctx)
		vs, err := platformCli.GetCurrentValidators(cctx, constants.PrimaryNetworkID, nil)
		cancel()
		if err != nil {
			return err
		}
		primaryValidatorsEndtime := make(map[ids.NodeID]time.Time)
		for _, v := range vs {
			primaryValidatorsEndtime[v.NodeID] = time.Unix(int64(v.EndTime), 0)
		}
		cctx, cancel = createDefaultCtx(ctx)
		vs, err = platformCli.GetCurrentValidators(cctx, subnetID, nil)
		cancel()
		if err != nil {
			return err
		}
		subnetValidators := set.Set[ids.NodeID]{}
		for _, v := range vs {
			subnetValidators.Add(v.NodeID)
		}
		participants := subnetSpecs[i].Participants
		for _, nodeName := range participants {
			node, b := ln.nodes[nodeName]
			if !b {
				return fmt.Errorf("participant node %s is not in network nodes", nodeName)
			}
			nodeID := node.GetNodeID()
			if isValidator := subnetValidators.Contains(nodeID); isValidator {
				continue
			}
			cctx, cancel := createDefaultCtx(ctx)
			tx, err := w.pWallet.IssueAddSubnetValidatorTx(
				&txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: nodeID,
						// reasonable delay in most/slow test environments
						Start: uint64(time.Now().Add(validationStartOffset).Unix()),
						End:   uint64(primaryValidatorsEndtime[nodeID].Unix()),
						Wght:  subnetValidatorsWeight,
					},
					Subnet: subnetID,
				},
				walletpkg.WithContext(cctx),
				defaultPoll,
			)
			cancel()
			if err != nil {
				return fmt.Errorf("P-Wallet Tx Error %s %w, node ID %s, subnetID %s", "IssueAddSubnetValidatorTx", err, nodeID.String(), subnetID.String())
			}
			ln.log.Info("added node as a subnet validator to subnet",
				zap.String("node-name", nodeName),
				zap.String("node-ID", nodeID.String()),
				zap.String("subnet-ID", subnetID.String()),
				zap.String("tx-ID", tx.ID().String()),
			)
		}
	}
	return nil
}

// waits until all nodes start validating the primary network
func (ln *localNetwork) waitPrimaryValidators(
	ctx context.Context,
	platformCli *platformvm.Client,
) error {
	ln.log.Info(logging.Green.Wrap("waiting for the nodes to become primary validators"))
	for {
		ready := true
		cctx, cancel := createDefaultCtx(ctx)
		vs, err := platformCli.GetCurrentValidators(cctx, constants.PrimaryNetworkID, nil)
		cancel()
		if err != nil {
			return err
		}
		primaryValidators := set.Set[ids.NodeID]{}
		for _, v := range vs {
			primaryValidators.Add(v.NodeID)
		}
		for _, node := range ln.nodes {
			nodeID := node.GetNodeID()
			if isValidator := primaryValidators.Contains(nodeID); !isValidator {
				ready = false
			}
		}
		if ready {
			return nil
		}
		select {
		case <-ln.onStopCh:
			return errAborted
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitForValidatorsPullFrequency):
		}
	}
}

// waits until all subnet participants start validating the subnetID, for all given subnets
func (ln *localNetwork) waitSubnetValidators(
	ctx context.Context,
	platformCli *platformvm.Client,
	subnetIDs []ids.ID,
	subnetSpecs []network.SubnetSpec,
) error {
	ln.log.Info(logging.Green.Wrap("waiting for the nodes to become subnet validators"))
	for {
		ready := true
		for i, subnetID := range subnetIDs {
			cctx, cancel := createDefaultCtx(ctx)
			vs, err := platformCli.GetCurrentValidators(cctx, subnetID, nil)
			cancel()
			if err != nil {
				return err
			}
			subnetValidators := set.Set[ids.NodeID]{}
			for _, v := range vs {
				subnetValidators.Add(v.NodeID)
			}
			participants := subnetSpecs[i].Participants
			for _, nodeName := range participants {
				node, b := ln.nodes[nodeName]
				if !b {
					return fmt.Errorf("participant node %s is not in network nodes", nodeName)
				}
				nodeID := node.GetNodeID()
				if isValidator := subnetValidators.Contains(nodeID); !isValidator {
					ready = false
				}
			}
			if !ready {
				break
			}
		}
		if ready {
			return nil
		}
		select {
		case <-ln.onStopCh:
			return errAborted
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitForValidatorsPullFrequency):
		}
	}
}

// reload VM plugins on all nodes
func (ln *localNetwork) reloadVMPlugins(ctx context.Context) error {
	ln.log.Info(logging.Green.Wrap("reloading plugin binaries"))
	for _, node := range ln.nodes {
		if node.paused {
			continue
		}
		uri := fmt.Sprintf("http://%s:%d", node.GetURL(), node.GetAPIPort())
		adminCli := admin.NewClient(uri)
		cctx, cancel := createDefaultCtx(ctx)
		_, failedVMs, err := adminCli.LoadVMs(cctx)
		cancel()
		if err != nil {
			return err
		}
		if len(failedVMs) > 0 {
			return fmt.Errorf("%d VMs failed to load: %v", len(failedVMs), failedVMs)
		}
	}
	return nil
}

func createBlockchainTxs(
	ctx context.Context,
	chainSpecs []network.BlockchainSpec,
	w *wallet,
	log logging.Logger,
) ([]*txs.Tx, error) {
	fmt.Println()
	log.Info(logging.Green.Wrap("creating tx for each custom chain"))
	blockchainTxs := make([]*txs.Tx, len(chainSpecs))
	for i, chainSpec := range chainSpecs {
		vmName := chainSpec.VMName
		vmID, err := utils.VMID(vmName)
		if err != nil {
			return nil, err
		}
		genesisBytes := chainSpec.Genesis

		log.Info("creating blockchain tx",
			zap.String("vm-name", vmName),
			zap.String("vm-ID", vmID.String()),
			zap.Int("bytes length of genesis", len(genesisBytes)),
		)
		cctx, cancel := createDefaultCtx(ctx)
		defer cancel()
		subnetID, err := ids.FromString(*chainSpec.SubnetID)
		if err != nil {
			return nil, err
		}
		tx, err := w.pWallet.IssueCreateChainTx(
			subnetID,
			genesisBytes,
			vmID,
			nil,
			vmName,
			walletpkg.WithContext(cctx),
		)
		if err != nil {
			return nil, fmt.Errorf("failure creating blockchain tx: %w", err)
		}

		blockchainTxs[i] = tx
	}

	return blockchainTxs, nil
}

func (ln *localNetwork) setBlockchainConfigFiles(
	ctx context.Context,
	chainSpecs []network.BlockchainSpec,
	blockchainTxs []*txs.Tx,
	subnetIDs []ids.ID,
	subnetSpecs []network.SubnetSpec,
	log logging.Logger,
) (set.Set[string], error) {
	fmt.Println()
	log.Info(logging.Green.Wrap("creating config files for each custom chain"))
	nodesToRestart := set.Set[string]{}
	for i, chainSpec := range chainSpecs {
		// get subnet participants
		participants := []string{}
		chainSubnetID, err := ids.FromString(*chainSpec.SubnetID)
		if err != nil {
			return nil, err
		}
		for j, newSubnetID := range subnetIDs {
			if chainSubnetID == newSubnetID {
				// subnet is new, use participants from spec
				participants = subnetSpecs[j].Participants
			}
		}
		if len(participants) == 0 {
			// get participants from network
			nodeNames, err := ln.getSubnetValidatorsNodenames(ctx, chainSubnetID)
			if err != nil {
				return nil, err
			}
			participants = nodeNames
		}
		chainAlias := blockchainTxs[i].ID().String()
		// update config info. set defaults and node specifics
		if chainSpec.ChainConfig != nil || len(chainSpec.PerNodeChainConfig) != 0 {
			for _, nodeName := range participants {
				_, b := ln.nodes[nodeName]
				if !b {
					return nil, fmt.Errorf("participant node %s is not in network nodes", nodeName)
				}
				chainConfig := chainSpec.ChainConfig
				if cfg, ok := chainSpec.PerNodeChainConfig[nodeName]; ok {
					chainConfig = cfg
				}
				ln.nodes[nodeName].config.ChainConfigFiles[chainAlias] = string(chainConfig)
				nodesToRestart.Add(nodeName)
			}
		}
		if chainSpec.NetworkUpgrade != nil {
			for _, nodeName := range participants {
				_, b := ln.nodes[nodeName]
				if !b {
					return nil, fmt.Errorf("participant node %s is not in network nodes", nodeName)
				}
				ln.nodes[nodeName].config.UpgradeConfigFiles[chainAlias] = string(chainSpec.NetworkUpgrade)
				nodesToRestart.Add(nodeName)
			}
		}
	}
	return nodesToRestart, nil
}

func (ln *localNetwork) setSubnetConfigFiles(
	subnetIDs []ids.ID,
	subnetSpecs []network.SubnetSpec,
) error {
	for i, subnetID := range subnetIDs {
		participants := subnetSpecs[i].Participants
		subnetConfig := subnetSpecs[i].SubnetConfig
		if subnetConfig != nil {
			for _, nodeName := range participants {
				_, b := ln.nodes[nodeName]
				if !b {
					return fmt.Errorf("participant node %s is not in network nodes", nodeName)
				}
				ln.nodes[nodeName].config.SubnetConfigFiles[subnetID.String()] = string(subnetConfig)
			}
		}
	}
	return nil
}

func (*localNetwork) createBlockchains(
	ctx context.Context,
	chainSpecs []network.BlockchainSpec,
	blockchainTxs []*txs.Tx,
	w *wallet,
	log logging.Logger,
) error {
	fmt.Println()
	log.Info(logging.Green.Wrap("creating each custom chain"))
	for i, chainSpec := range chainSpecs {
		vmName := chainSpec.VMName
		vmID, err := utils.VMID(vmName)
		if err != nil {
			return err
		}
		log.Info("creating blockchain",
			zap.String("vm-name", vmName),
			zap.String("vm-ID", vmID.String()),
		)

		cctx, cancel := createDefaultCtx(ctx)
		defer cancel()

		err = w.pWallet.IssueTx(
			blockchainTxs[i],
			walletpkg.WithContext(cctx),
			defaultPoll,
		)
		if err != nil {
			return fmt.Errorf("P-Wallet Tx Error %s %w, blockchainID %s", "IssueCreateBlockchainTx", err, blockchainTxs[i].ID().String())
		}
		blockchainID := blockchainTxs[i].ID()

		log.Info("created a new blockchain",
			zap.String("vm-name", vmName),
			zap.String("vm-ID", vmID.String()),
			zap.String("blockchain-ID", blockchainID.String()),
		)
	}

	return nil
}

func createDefaultCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, defaultTimeout)
}
