// Copyright (C) 2021-2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package local

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/luxfi/keys"

	"github.com/luxfi/genesis/pkg/genesis"
	"github.com/luxfi/proto/p/reward"

	"github.com/luxfi/sdk/wallet/chain/x"
	"github.com/luxfi/utxo"
	"github.com/luxfi/vm/components/verify"

	"maps"
	"slices"

	"github.com/luxfi/config"
	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
	log "github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/netrunner/network"
	"github.com/luxfi/netrunner/network/node"
	"github.com/luxfi/netrunner/utils"
	"github.com/luxfi/proto/p/signer"
	"github.com/luxfi/proto/p/txs"
	"github.com/luxfi/sdk/admin"
	"github.com/luxfi/sdk/platformvm"
	pwallet "github.com/luxfi/sdk/wallet/chain/p"
	pwalletwallet "github.com/luxfi/sdk/wallet/chain/p/wallet"
	"github.com/luxfi/utxo/secp256k1fx"

	pbuilder "github.com/luxfi/sdk/wallet/chain/p/builder"
	psigner "github.com/luxfi/sdk/wallet/chain/p/signer"
	xbuilder "github.com/luxfi/sdk/wallet/chain/x/builder"
	xsigner "github.com/luxfi/sdk/wallet/chain/x/signer"
	primary "github.com/luxfi/sdk/wallet/primary"
	common "github.com/luxfi/sdk/wallet/primary/common"
)

const (
	// offset of validation start from current time
	// Reduced from 20s to 5s for local networks - validators activate faster
	validationStartOffset               = 5 * time.Second
	permissionlessValidationStartOffset = 10 * time.Second
	// duration for primary network validators
	validationDuration = 365 * 24 * time.Hour
	// weight assigned to chain validators
	chainValidatorsWeight = 1000
	// check period for blockchain logs while waiting for custom chains to be ready
	blockchainLogPullFrequency = time.Second
	// check period while waiting for all validators to be ready
	// Reduced from 1s to 200ms for faster validator detection
	waitForValidatorsPullFrequency = 200 * time.Millisecond
	// FAIL FAST: Reduced from 10 minutes to 30 seconds for local deployments
	// Most P-chain API calls should complete in <5s on localhost
	defaultTimeout         = 30 * time.Second
	stakingMinimumLeadTime = 25 * time.Second
	minStakeDuration       = 24 * 14 * time.Hour
	// FAIL FAST: Reduced from 45s to 30s for local deployments
	// For local networks, validators activate quickly (5-10s). If it takes longer,
	// something is wrong - fail fast with clear error rather than waiting forever.
	chainValidatorWaitTimeout = 30 * time.Second
)

var (
	errAborted  = errors.New("aborted")
	defaultPoll = common.WithPollFrequency(100 * time.Millisecond)
)

type blockchainInfo struct {
	chainName    string
	vmID         ids.ID
	chainID      ids.ID
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
	clientURI := node.GetURL() // GetURL now returns full URL http://host:port
	ln.logger.Info("getClientURI",
		"nodeName", node.GetName(),
		"uri", clientURI)
	return clientURI, nil
}

func (ln *localNetwork) CreateChains(
	ctx context.Context,
	chainSpecs []network.ChainSpec, // VM name + genesis bytes
) ([]ids.ID, error) {
	ln.lock.Lock()
	defer ln.lock.Unlock()

	// Log the chain specs we're trying to create for debugging
	for i, spec := range chainSpecs {
		ln.logger.Info("CreateChains: processing chain spec",
			"index", i,
			"vmName", spec.VMName,
			"hasGenesis", len(spec.Genesis) > 0,
			"hasChainID", spec.ChainID != nil,
		)
	}

	chainInfos, err := ln.installCustomChains(ctx, chainSpecs)
	if err != nil {
		ln.logger.Error("CreateChains: failed to install custom chains",
			"error", err.Error(),
			"numChainSpecs", len(chainSpecs),
		)
		return nil, fmt.Errorf("failed to install custom chains: %w", err)
	}

	if err := ln.waitForCustomChainsReady(ctx, chainInfos); err != nil {
		ln.logger.Error("CreateChains: chains failed to become ready",
			"error", err.Error(),
			"numChains", len(chainInfos),
		)
		return nil, fmt.Errorf("chains failed to become ready: %w", err)
	}

	// Wait for all nodes to discover the chains before aliasing
	if err := ln.waitForChainsDiscoveredOnAllNodes(ctx, chainInfos); err != nil {
		ln.logger.Error("CreateChains: chains not discovered on all nodes",
			"error", err.Error(),
		)
		return nil, fmt.Errorf("chains not discovered on all nodes: %w", err)
	}

	if err := ln.RegisterAliases(ctx, chainInfos, chainSpecs); err != nil {
		ln.logger.Error("CreateChains: failed to register aliases",
			"error", err.Error(),
		)
		return nil, fmt.Errorf("failed to register aliases: %w", err)
	}

	chainIDs := []ids.ID{}
	for _, chainInfo := range chainInfos {
		chainIDs = append(chainIDs, chainInfo.blockchainID)
	}

	return chainIDs, nil
}

// if alias is defined in blockchain-specs, registers an alias for the previously created blockchain
func (ln *localNetwork) RegisterAliases(
	ctx context.Context,
	chainInfos []blockchainInfo,
	chainSpecs []network.ChainSpec,
) error {
	fmt.Println()
	ln.logger.Info(log.Blue.Wrap(log.Bold.Wrap("registering blockchain aliases")))
	for i, chainSpec := range chainSpecs {
		if chainSpec.Alias == "" {
			continue
		}
		blockchainAlias := chainSpec.Alias
		chainID := chainInfos[i].blockchainID.String()
		ln.logger.Info("registering blockchain alias",
			"alias", blockchainAlias,
			"chain-id", chainID)
		for nodeName, node := range ln.nodes {
			if node.paused {
				continue
			}
			adminClient := node.client.AdminAPI()
			if adminClient == nil {
				return fmt.Errorf("admin client is nil for node %v", nodeName)
			}
			// Retry with exponential backoff - nodes may not have discovered chain yet
			// FAIL FAST: Reduced retries from 10 to 5, max delay from 5s to 2s
			maxRetries := 5
			baseDelay := 300 * time.Millisecond
			var lastErr error
			for attempt := 0; attempt < maxRetries; attempt++ {
				if err := (*adminClient).AliasChain(ctx, chainID, blockchainAlias); err != nil {
					// Check if alias is already mapped - this is not an error, the alias is already set
					if strings.Contains(err.Error(), "alias already mapped to an ID") {
						ln.logger.Info("alias already registered",
							"node", nodeName,
							"alias", blockchainAlias,
							"chain", chainID,
						)
						lastErr = nil
						break
					}
					lastErr = err
					delay := baseDelay * time.Duration(1<<attempt)
					if delay > 2*time.Second {
						delay = 2 * time.Second // FAIL FAST: reduced max delay
					}
					ln.logger.Debug("alias registration failed, retrying",
						"node", nodeName,
						"chain", chainID,
						"attempt", attempt+1,
						"delay", delay,
						"error", err.Error(),
					)
					select {
					case <-ctx.Done():
						return fmt.Errorf("context cancelled while registering alias: %w", ctx.Err())
					case <-time.After(delay):
						continue
					}
				}
				lastErr = nil
				break
			}
			if lastErr != nil {
				return fmt.Errorf("failure to register blockchain alias %v on node %v after %d attempts: %w",
					blockchainAlias, nodeName, maxRetries, lastErr)
			}
		}
	}
	return nil
}

// waitForChainsDiscoveredOnAllNodes waits until all nodes recognize the blockchain IDs
// This is needed because even with track-chains=all, nodes need time to sync P-Chain
// and discover newly created chains
func (ln *localNetwork) waitForChainsDiscoveredOnAllNodes(
	ctx context.Context,
	chainInfos []blockchainInfo,
) error {
	fmt.Println()
	ln.logger.Info(log.Blue.Wrap(log.Bold.Wrap("waiting for chains to be discovered on all nodes...")))

	// FAIL FAST: Respect the context deadline, don't use hardcoded timeout
	// For local networks, chain discovery should complete in <30s
	// The context already has a timeout from the caller
	maxWait := 30 * time.Second // Max wait for local chain discovery
	pollInterval := 500 * time.Millisecond

	// Use the earlier of: context deadline or our maxWait
	deadline := time.Now().Add(maxWait)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}

	for _, chainInfo := range chainInfos {
		blockchainID := chainInfo.blockchainID.String()
		ln.logger.Info("waiting for chain discovery",
			"blockchain-id", blockchainID,
		)

		for nodeName, node := range ln.nodes {
			if node.paused {
				continue
			}

			discovered := false
			attemptCount := 0
			for time.Now().Before(deadline) {
				attemptCount++
				// Try to query the chain RPC endpoint with eth_chainId - if it responds, the chain is discovered
				chainRPCURL := fmt.Sprintf("%s/v1/chain/%s/rpc", node.GetURL(), blockchainID)
				// Use a proper JSON-RPC request body to ensure the EVM endpoint responds
				jsonRPCBody := strings.NewReader(`{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}`)
				req, err := http.NewRequestWithContext(ctx, "POST", chainRPCURL, jsonRPCBody)
				if err != nil {
					return fmt.Errorf("failed to create request: %w", err)
				}
				req.Header.Set("Content-Type", "application/json")

				client := &http.Client{Timeout: 5 * time.Second}
				resp, err := client.Do(req)
				if err == nil {
					body, _ := io.ReadAll(resp.Body)
					resp.Body.Close()
					// Check if we got a valid JSON-RPC response with a result (not an error)
					if resp.StatusCode == 200 {
						// Parse JSON-RPC response to verify it's a valid chainId response
						var jsonRPCResp struct {
							Result string `json:"result"`
							Error  *struct {
								Code    int    `json:"code"`
								Message string `json:"message"`
							} `json:"error"`
						}
						if err := json.Unmarshal(body, &jsonRPCResp); err == nil {
							// Only consider discovered if we got a result, not an error
							if jsonRPCResp.Result != "" && jsonRPCResp.Error == nil {
								discovered = true
								ln.logger.Info("chain discovered on node",
									"node", nodeName,
									"blockchain-id", blockchainID,
									"attempts", attemptCount,
									"chainId", jsonRPCResp.Result,
								)
								break
							}
							if attemptCount%10 == 0 && jsonRPCResp.Error != nil {
								ln.logger.Warn("chain RPC returned error, VM may still be initializing",
									"node", nodeName,
									"blockchain-id", blockchainID,
									"error", jsonRPCResp.Error.Message,
									"attempts", attemptCount,
								)
							}
						}
					}
					if attemptCount%10 == 0 {
						ln.logger.Warn("chain endpoint returned non-200",
							"node", nodeName,
							"blockchain-id", blockchainID,
							"status", resp.StatusCode,
							"attempts", attemptCount,
							"response", string(body),
						)
					}
				} else {
					if attemptCount%10 == 0 {
						ln.logger.Warn("chain not yet discovered, waiting",
							"node", nodeName,
							"blockchain-id", blockchainID,
							"error", err.Error(),
							"attempts", attemptCount,
							"url", chainRPCURL,
						)
					}
				}

				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(pollInterval):
					continue
				}
			}

			if !discovered {
				// Provide more diagnostic information
				var lastErr string
				// Try one more request to get the exact error
				chainRPCURL := fmt.Sprintf("%s/v1/chain/%s/rpc", node.GetURL(), blockchainID)
				jsonRPCBody := strings.NewReader(`{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}`)
				req, _ := http.NewRequestWithContext(ctx, "POST", chainRPCURL, jsonRPCBody)
				if req != nil {
					req.Header.Set("Content-Type", "application/json")
					client := &http.Client{Timeout: 5 * time.Second}
					if resp, err := client.Do(req); err != nil {
						lastErr = fmt.Sprintf("connection error: %s", err.Error())
					} else {
						body, _ := io.ReadAll(resp.Body)
						resp.Body.Close()
						lastErr = fmt.Sprintf("status=%d, body=%s", resp.StatusCode, string(body))
					}
				}
				return fmt.Errorf("chain %s not discovered on node %s after %v (%d attempts)\n"+
					"Last error: %s\n"+
					"This may indicate:\n"+
					"  1. VM plugin not loaded - check if plugin file exists in plugin directory\n"+
					"  2. Genesis configuration mismatch - verify genesis is compatible with VM\n"+
					"  3. Network not tracking chain - ensure track-chains is configured correctly",
					blockchainID, nodeName, maxWait, attemptCount, lastErr)
			}
		}
	}

	ln.logger.Info(log.Green.Wrap("all chains discovered on all nodes"))
	return nil
}

// waitForBlockchainOnPChain waits for all nodes to see the blockchain on their P-Chain
// This ensures P-Chain state is synchronized across all nodes after blockchain creation
func (ln *localNetwork) waitForBlockchainOnPChain(
	ctx context.Context,
	blockchainIDs []ids.ID,
) error {
	fmt.Println()
	ln.logger.Info(log.Blue.Wrap(log.Bold.Wrap("waiting for P-Chain to sync blockchain across all nodes...")))

	// FAIL FAST: Respect context deadline, don't use hardcoded 60s
	maxWait := 30 * time.Second // P-chain sync should be fast on localhost
	pollInterval := 500 * time.Millisecond

	// Use the earlier of: context deadline or our maxWait
	deadline := time.Now().Add(maxWait)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}

	for _, blockchainID := range blockchainIDs {
		ln.logger.Info("waiting for blockchain on P-Chain",
			"blockchain-id", blockchainID.String(),
		)

		for nodeName, node := range ln.nodes {
			if node.paused {
				continue
			}

			found := false
			attemptCount := 0
			for time.Now().Before(deadline) {
				attemptCount++
				pClient := node.GetAPIClient().PChainAPI()
				if pClient == nil {
					if attemptCount%10 == 0 {
						ln.logger.Warn("P-Chain client not available",
							"node", nodeName,
							"attempts", attemptCount,
						)
					}
					time.Sleep(pollInterval)
					continue
				}

				blockchains, err := (*pClient).GetBlockchains(ctx)
				if err != nil {
					if attemptCount%10 == 0 {
						ln.logger.Warn("failed to get blockchains from P-Chain",
							"node", nodeName,
							"error", err.Error(),
							"attempts", attemptCount,
						)
					}
					time.Sleep(pollInterval)
					continue
				}

				for _, bc := range blockchains {
					if bc.ID == blockchainID {
						found = true
						ln.logger.Info("blockchain visible on P-Chain",
							"node", nodeName,
							"blockchain-id", blockchainID.String(),
							"attempts", attemptCount,
						)
						break
					}
				}

				if found {
					break
				}

				if attemptCount%10 == 0 {
					ln.logger.Warn("blockchain not yet visible on P-Chain",
						"node", nodeName,
						"blockchain-id", blockchainID.String(),
						"numBlockchainsFound", len(blockchains),
						"attempts", attemptCount,
					)
				}
				time.Sleep(pollInterval)
			}

			if !found {
				return fmt.Errorf("blockchain %s not visible on P-Chain of node %s after %v (%d attempts)",
					blockchainID.String(), nodeName, maxWait, attemptCount)
			}
		}
	}

	ln.logger.Info(log.Green.Wrap("P-Chain synchronized - all nodes see all blockchains"))
	return nil
}

// waitForPeerConnectivity waits for all nodes to have at least one peer connected
// This ensures the network is properly connected after node restarts
func (ln *localNetwork) waitForPeerConnectivity(ctx context.Context) error {
	ln.logger.Info(log.Blue.Wrap(log.Bold.Wrap("waiting for peer connectivity...")))

	maxWait := 30 * time.Second
	pollInterval := 500 * time.Millisecond
	deadline := time.Now().Add(maxWait)
	minPeers := 1 // At least 1 peer required

	for nodeName, node := range ln.nodes {
		if node.paused {
			continue
		}

		connected := false
		attemptCount := 0
	peerLoop:
		for time.Now().Before(deadline) {
			attemptCount++
			infoClient := node.GetAPIClient().InfoAPI()
			if infoClient == nil {
				if attemptCount%10 == 0 {
					ln.logger.Warn("Info client not available",
						"node", nodeName,
						"attempts", attemptCount,
					)
				}
				select {
				case <-ctx.Done():
					return fmt.Errorf("context cancelled while waiting for peers on node %s: %w", nodeName, ctx.Err())
				case <-time.After(pollInterval):
				}
				continue
			}

			peers, err := (*infoClient).Peers(ctx, nil)
			if err != nil {
				if attemptCount%10 == 0 {
					ln.logger.Warn("failed to get peers",
						"node", nodeName,
						"error", err.Error(),
						"attempts", attemptCount,
					)
				}
				select {
				case <-ctx.Done():
					return fmt.Errorf("context cancelled while waiting for peers on node %s: %w", nodeName, ctx.Err())
				case <-time.After(pollInterval):
				}
				continue
			}

			if len(peers) >= minPeers {
				connected = true
				ln.logger.Debug("node has peers",
					"node", nodeName,
					"numPeers", len(peers),
					"attempts", attemptCount,
				)
				break peerLoop
			}

			if attemptCount%10 == 0 {
				ln.logger.Warn("waiting for peers",
					"node", nodeName,
					"currentPeers", len(peers),
					"minRequired", minPeers,
					"attempts", attemptCount,
				)
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("context cancelled while waiting for peers on node %s: %w", nodeName, ctx.Err())
			case <-time.After(pollInterval):
			}
		}

		if !connected {
			return fmt.Errorf("node %s failed to connect to peers after %v (%d attempts)",
				nodeName, maxWait, attemptCount)
		}
	}

	ln.logger.Info(log.Green.Wrap("all nodes have peer connectivity"))
	return nil
}

// waitForPChainHeightSync waits for all nodes to sync to the same P-Chain height
// This is critical after node restarts to ensure P-Chain consensus is consistent
func (ln *localNetwork) waitForPChainHeightSync(ctx context.Context) error {
	ln.logger.Info(log.Blue.Wrap(log.Bold.Wrap("waiting for P-Chain height to sync across all nodes...")))

	maxWait := 60 * time.Second
	pollInterval := 500 * time.Millisecond
	deadline := time.Now().Add(maxWait)

	for time.Now().Before(deadline) {
		// Get heights from all nodes
		heights := make(map[string]uint64)
		var maxHeight uint64 = 0

		for nodeName, node := range ln.nodes {
			if node.paused {
				continue
			}

			pClient := node.GetAPIClient().PChainAPI()
			if pClient == nil {
				continue
			}

			height, err := (*pClient).GetHeight(ctx)
			if err != nil {
				ln.logger.Warn("failed to get P-Chain height",
					"node", nodeName,
					"error", err.Error(),
				)
				continue
			}

			heights[nodeName] = height
			if height > maxHeight {
				maxHeight = height
			}
		}

		// Check if all nodes have same height as max
		allSynced := true
		for nodeName, height := range heights {
			if height < maxHeight {
				allSynced = false
				ln.logger.Debug("node P-Chain height behind",
					"node", nodeName,
					"height", height,
					"maxHeight", maxHeight,
				)
			}
		}

		if allSynced && maxHeight > 0 {
			ln.logger.Info(log.Green.Wrap("P-Chain heights synchronized"),
				"height", maxHeight,
				"numNodes", len(heights),
			)
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for P-Chain sync: %w", ctx.Err())
		case <-time.After(pollInterval):
		}
	}

	// Log final state for debugging
	for nodeName, node := range ln.nodes {
		if node.paused {
			continue
		}
		pClient := node.GetAPIClient().PChainAPI()
		if pClient != nil {
			height, _ := (*pClient).GetHeight(ctx)
			ln.logger.Error("P-Chain sync timeout - node height",
				"node", nodeName,
				"height", height,
			)
		}
	}

	return fmt.Errorf("P-Chain heights did not sync within %v", maxWait)
}

func (ln *localNetwork) RemoveChainValidators(
	ctx context.Context,
	removeParticipantsSpecs []network.RemoveChainValidatorSpec,
) error {
	ln.lock.Lock()
	defer ln.lock.Unlock()

	return ln.removeChainValidators(ctx, removeParticipantsSpecs)
}

func (ln *localNetwork) AddPermissionlessValidators(
	ctx context.Context,
	validatorSpec []network.PermissionlessValidatorSpec,
) error {
	ln.lock.Lock()
	defer ln.lock.Unlock()

	return ln.addPermissionlessValidators(ctx, validatorSpec)
}

func (ln *localNetwork) TransformChain(
	ctx context.Context,
	elasticChainConfig []network.ElasticChainSpec,
) ([]ids.ID, []ids.ID, error) {
	ln.lock.Lock()
	defer ln.lock.Unlock()

	return ln.transformToElasticChains(ctx, elasticChainConfig)
}

func (ln *localNetwork) CreateParticipantGroups(
	ctx context.Context,
	participantsSpecs []network.ParticipantsSpec,
) ([]ids.ID, error) {
	ln.lock.Lock()
	defer ln.lock.Unlock()

	return ln.installChains(ctx, participantsSpecs)
}

// provisions local cluster and install custom chains if applicable
// assumes the local cluster is already set up and healthy
func (ln *localNetwork) installCustomChains(
	ctx context.Context,
	chainSpecs []network.ChainSpec,
) ([]blockchainInfo, error) {
	fmt.Println()
	ln.logger.Info(log.Blue.Wrap(log.Bold.Wrap("create and install custom chains")))

	// Ensure nodes are healthy before proceeding (nodes may have been restarted by a prior
	// CreateParticipantGroups call which restarts nodes to track chains)
	if err := ln.healthy(ctx); err != nil {
		ln.logger.Error("installCustomChains: network not healthy before chain installation",
			"error", err.Error(),
		)
		return nil, fmt.Errorf("network not healthy at start of installCustomChains: %w", err)
	}

	clientURI, err := ln.getClientURI()
	if err != nil {
		ln.logger.Error("installCustomChains: failed to get client URI",
			"error", err.Error(),
		)
		return nil, fmt.Errorf("failed to get client URI: %w", err)
	}
	platformCli := platformvm.NewClient(clientURI)

	// wallet needs txs for all previously created chains
	var preloadTXs []ids.ID
	for _, chainSpec := range chainSpecs {
		// if chain id for the blockchain is specified, we need to add the chain id
		// tx info to the wallet so blockchain creation does not fail
		// if chain id is not specified, a new chain will later be created by using the wallet,
		// and the wallet will obtain the tx info at that moment
		if chainSpec.ChainID != nil {
			chainID, err := ids.FromString(*chainSpec.ChainID)
			if err != nil {
				ln.logger.Error("installCustomChains: invalid chain ID format",
					"error", err.Error(),
					"chainID", *chainSpec.ChainID,
					"vmName", chainSpec.VMName,
				)
				return nil, fmt.Errorf("invalid chain ID %q for VM %s: %w", *chainSpec.ChainID, chainSpec.VMName, err)
			}
			preloadTXs = append(preloadTXs, chainID)
		}
	}

	w, err := newWallet(ctx, clientURI, preloadTXs)
	if err != nil {
		ln.logger.Error("installCustomChains: failed to create wallet",
			"error", err.Error(),
			"clientURI", clientURI,
		)
		return nil, fmt.Errorf("failed to create wallet for chain installation: %w", err)
	}

	// get chain specs for all new chains to create
	// for the list of requested blockchains, we take those that have undefined chain id
	// and use the provided chain spec. if not given, use an empty default chain spec
	// that chains will be created and later on assigned to the blockchain requests
	participantsSpecs := []network.ParticipantsSpec{}
	for _, chainSpec := range chainSpecs {
		if chainSpec.ChainID == nil {
			if chainSpec.ParticipantsSpec == nil {
				participantsSpecs = append(participantsSpecs, network.ParticipantsSpec{})
			} else {
				participantsSpecs = append(participantsSpecs, *chainSpec.ParticipantsSpec)
			}
		}
	}

	// if no participants are given for a new chain, assume all nodes should be participants
	allNodeNames := slices.Collect(maps.Keys(ln.nodes))
	sort.Strings(allNodeNames)
	for i := range participantsSpecs {
		if len(participantsSpecs[i].Participants) == 0 {
			participantsSpecs[i].Participants = allNodeNames
		}
	}

	// create new nodes
	for _, participantsSpec := range participantsSpecs {
		for _, nodeName := range participantsSpec.Participants {
			_, ok := ln.nodes[nodeName]
			if !ok {
				ln.logger.Info(log.Green.Wrap(fmt.Sprintf("adding new participant %s", nodeName)))
				if _, err := ln.addNode(node.Config{Name: nodeName}); err != nil {
					ln.logger.Error("installCustomChains: failed to add participant node",
						"error", err.Error(),
						"nodeName", nodeName,
					)
					return nil, fmt.Errorf("failed to add participant node %s: %w", nodeName, err)
				}
			}
		}
	}
	if err := ln.healthy(ctx); err != nil {
		ln.logger.Error("installCustomChains: network not healthy after adding nodes",
			"error", err.Error(),
		)
		return nil, fmt.Errorf("network not healthy after adding nodes: %w", err)
	}

	// just ensure all nodes are primary validators (so can be chain validators)
	if err := ln.addPrimaryValidators(ctx, platformCli, w); err != nil {
		ln.logger.Error("installCustomChains: failed to add primary validators",
			"error", err.Error(),
		)
		return nil, fmt.Errorf("failed to add primary validators: %w", err)
	}

	// Ensure P-chain has liquid funds for chain creation
	// Genesis allocations go to X-chain, so we may need to export/import to P-chain
	if err := fundPChainFromXChain(ctx, w, ln.logger); err != nil {
		ln.logger.Error("installCustomChains: failed to fund P-chain from X-chain",
			"error", err.Error(),
		)
		return nil, fmt.Errorf("failed to fund P-chain from X-chain: %w", err)
	}

	// create missing chains
	chainIDs, err := createChains(ctx, uint32(len(participantsSpecs)), w, ln.logger)
	if err != nil {
		ln.logger.Error("installCustomChains: failed to create chains",
			"error", err.Error(),
			"numChains", len(participantsSpecs),
		)
		return nil, fmt.Errorf("failed to create chains: %w", err)
	}

	if err := ln.setChainConfigFiles(chainIDs, participantsSpecs); err != nil {
		ln.logger.Error("installCustomChains: failed to set chain config files",
			"error", err.Error(),
		)
		return nil, fmt.Errorf("failed to set chain config files: %w", err)
	}

	// assign created chains to blockchain requests with undefined chain id
	j := 0
	for i := range chainSpecs {
		if chainSpecs[i].ChainID == nil {
			chainIDStr := chainIDs[j].String()
			chainSpecs[i].ChainID = &chainIDStr
			j++
		}
	}

	// wait for nodes to be primary validators before trying to add them as chain ones
	if err = ln.waitPrimaryValidators(ctx, platformCli); err != nil {
		ln.logger.Error("installCustomChains: timeout waiting for primary validators",
			"error", err.Error(),
		)
		return nil, fmt.Errorf("timeout waiting for primary validators: %w", err)
	}

	// Wait for chain creation transactions to be fully committed before adding validators
	// The P-Chain needs time to commit the chain creation blocks and propagate state to all nodes
	// For local networks with fast consensus (K=5), 500ms is sufficient
	ln.logger.Info("waiting for chain creation to be committed...")
	time.Sleep(500 * time.Millisecond)

	if err = ln.addChainValidators(ctx, platformCli, w, chainIDs, participantsSpecs); err != nil {
		ln.logger.Error("installCustomChains: failed to add chain validators",
			"error", err.Error(),
		)
		return nil, fmt.Errorf("failed to add chain validators: %w", err)
	}

	// Use separate context for blockchain tx creation with longer timeout
	// This prevents context deadline cascade from previous operations
	txCtx, txCancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer txCancel()

	blockchainTxs, err := createBlockchainTxs(txCtx, chainSpecs, w, ln.logger)
	if err != nil {
		ln.logger.Error("installCustomChains: failed to create blockchain transactions",
			"error", err.Error(),
		)
		return nil, fmt.Errorf("failed to create blockchain transactions: %w", err)
	}

	nodesToRestartForBlockchainConfigUpdate, err := ln.setBlockchainConfigFiles(ctx, chainSpecs, blockchainTxs, chainIDs, participantsSpecs, ln.logger)
	if err != nil {
		ln.logger.Error("installCustomChains: failed to set blockchain config files",
			"error", err.Error(),
		)
		return nil, fmt.Errorf("failed to set blockchain config files: %w", err)
	}

	// Hot-reload VM plugins - this is the ONLY path now (no restarts)
	// VMs are loaded dynamically without requiring node restart
	ln.logger.Info(log.Blue.Wrap("hot-reloading VM plugins (no restart needed)..."))
	if err := ln.reloadVMPlugins(ctx); err != nil {
		ln.logger.Error("VM hot-reload failed",
			"error", err.Error(),
			"errorDetail", fmt.Sprintf("%+v", err),
		)
		return nil, fmt.Errorf("VM hot-reload failed: %w", err)
	}

	// Log if there were config file updates (we skip restart now)
	if len(nodesToRestartForBlockchainConfigUpdate) > 0 {
		ln.logger.Info("skipping node restart for config file updates (using hot-reload instead)",
			"numNodesWithUpdates", len(nodesToRestartForBlockchainConfigUpdate),
		)
	}

	if err = ln.waitChainValidators(ctx, platformCli, chainIDs, participantsSpecs); err != nil {
		ln.logger.Error("installCustomChains: timeout waiting for chain validators",
			"error", err.Error(),
		)
		return nil, fmt.Errorf("timeout waiting for chain validators: %w", err)
	}

	// create blockchain from txs before spending more utxos
	if err := ln.createChains(ctx, chainSpecs, blockchainTxs, w, ln.logger); err != nil {
		ln.logger.Error("installCustomChains: failed to create blockchains from transactions",
			"error", err.Error(),
			"numChainSpecs", len(chainSpecs),
		)
		return nil, fmt.Errorf("failed to create blockchains from transactions: %w", err)
	}

	// Collect blockchain IDs for sync and hot-reload
	blockchainIDs := make([]ids.ID, len(blockchainTxs))
	for i, tx := range blockchainTxs {
		blockchainIDs[i] = tx.ID()
	}
	for i, blockchainID := range blockchainIDs {
		ln.logger.Info("blockchain created",
			"index", i,
			"blockchainID", blockchainID.String(),
			"chainID", *chainSpecs[i].ChainID,
		)
	}

	// CRITICAL: Wait for all nodes to see the blockchain on P-Chain BEFORE hot-reload
	// This prevents the race condition where hot-reload is called before P-Chain tx is finalized
	if err := ln.waitForBlockchainOnPChain(ctx, blockchainIDs); err != nil {
		ln.logger.Error("installCustomChains: P-Chain sync failed",
			"error", err.Error(),
		)
		return nil, fmt.Errorf("P-Chain sync failed: %w", err)
	}

	// Now hot-reload VMs - P-Chain state is synchronized, chains will be discovered
	ln.logger.Info(log.Blue.Wrap("hot-reloading VMs for newly created blockchains (no restart)..."))
	if err := ln.reloadVMPlugins(ctx); err != nil {
		ln.logger.Error("VM hot-reload after chain creation failed",
			"error", err.Error(),
		)
		return nil, fmt.Errorf("VM hot-reload failed after chain creation: %w", err)
	}

	chainInfos := make([]blockchainInfo, len(chainSpecs))
	for i, chainSpec := range chainSpecs {
		vmID, err := utils.VMID(chainSpec.VMName)
		if err != nil {
			ln.logger.Error("installCustomChains: invalid VM name",
				"error", err.Error(),
				"vmName", chainSpec.VMName,
			)
			return nil, fmt.Errorf("invalid VM name %s: %w", chainSpec.VMName, err)
		}
		chainID, err := ids.FromString(*chainSpec.ChainID)
		if err != nil {
			ln.logger.Error("installCustomChains: invalid chain ID",
				"error", err.Error(),
				"chainID", *chainSpec.ChainID,
				"vmName", chainSpec.VMName,
			)
			return nil, fmt.Errorf("invalid chain ID %s for VM %s: %w", *chainSpec.ChainID, chainSpec.VMName, err)
		}
		chainInfos[i] = blockchainInfo{
			// we keep a record of VM name in blockchain name field,
			// as there is no way to recover VM name from VM ID
			chainName:    chainSpec.VMName,
			vmID:         vmID,
			chainID:      chainID,
			blockchainID: blockchainTxs[i].ID(),
		}
	}

	return chainInfos, nil
}

func (ln *localNetwork) installChains(
	ctx context.Context,
	participantsSpecs []network.ParticipantsSpec,
) ([]ids.ID, error) {
	fmt.Println()
	ln.logger.Info(log.Blue.Wrap(log.Bold.Wrap("create chains")))

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
	allNodeNames := slices.Collect(maps.Keys(ln.nodes))
	sort.Strings(allNodeNames)
	for i := range participantsSpecs {
		if len(participantsSpecs[i].Participants) == 0 {
			participantsSpecs[i].Participants = allNodeNames
		}
	}

	// create new nodes
	for _, participantsSpec := range participantsSpecs {
		for _, nodeName := range participantsSpec.Participants {
			_, ok := ln.nodes[nodeName]
			if !ok {
				ln.logger.Info(log.Green.Wrap(fmt.Sprintf("adding new participant %s", nodeName)))
				if _, err := ln.addNode(node.Config{Name: nodeName}); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := ln.healthy(ctx); err != nil {
		return nil, err
	}

	// just ensure all nodes are primary validators (so can be chain validators)
	if err := ln.addPrimaryValidators(ctx, platformCli, w); err != nil {
		return nil, err
	}

	chainIDs, err := createChains(ctx, uint32(len(participantsSpecs)), w, ln.logger)
	if err != nil {
		return nil, err
	}

	if err := ln.setChainConfigFiles(chainIDs, participantsSpecs); err != nil {
		return nil, err
	}

	// wait for nodes to be primary validators before trying to add them as chain ones
	if err = ln.waitPrimaryValidators(ctx, platformCli); err != nil {
		return nil, err
	}

	if err = ln.addChainValidators(ctx, platformCli, w, chainIDs, participantsSpecs); err != nil {
		return nil, err
	}

	if err := ln.restartNodes(ctx, chainIDs, participantsSpecs, nil, nil, nil); err != nil {
		return nil, err
	}

	// Wait for nodes to be healthy after restart before querying P-Chain
	ln.logger.Info("waiting for nodes to be healthy after restart...")
	if err := ln.healthy(ctx); err != nil {
		return nil, fmt.Errorf("nodes not healthy after restart: %w", err)
	}

	if err = ln.waitChainValidators(ctx, platformCli, chainIDs, participantsSpecs); err != nil {
		return nil, err
	}

	return chainIDs, nil
}

func (ln *localNetwork) getChainValidatorsNodenames(
	ctx context.Context,
	chainID ids.ID,
) ([]string, error) {
	clientURI, err := ln.getClientURI()
	if err != nil {
		return nil, err
	}
	platformCli := platformvm.NewClient(clientURI)

	// Use different variable name to avoid shadowing outer context
	apiCtx, cancel := createDefaultCtx(ctx)
	vs, err := platformCli.GetCurrentValidators(apiCtx, chainID, nil)
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
		return nil, fmt.Errorf("not all validators for chain %s are present in network", chainID.String())
	}
	return nodeNames, nil
}

func (ln *localNetwork) waitForCustomChainsReady(
	ctx context.Context,
	chainInfos []blockchainInfo,
) error {
	fmt.Println()

	// Log chain info immediately for user feedback
	for _, chainInfo := range chainInfos {
		ln.logger.Info("custom chain created",
			"vm-ID", chainInfo.vmID.String(),
			"chain-ID", chainInfo.chainID.String(),
			"blockchain-ID", chainInfo.blockchainID.String(),
		)
	}

	// FAIL FAST: 15s timeout for local chain health check
	// If chain isn't healthy in 15s on localhost, something is wrong - return error immediately
	healthCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	if err := ln.healthy(healthCtx); err != nil {
		// CRITICAL FIX: Return error instead of swallowing it
		// This was causing CLI to wait forever while errors were logged but not returned
		ln.logger.Error("chain health check failed - returning error immediately",
			"error", err.Error(),
		)
		return fmt.Errorf("custom chains not ready within 15s: %w", err)
	}

	fmt.Println()
	ln.logger.Info(log.Green.Wrap("custom chains ready"))
	return nil
}

func (ln *localNetwork) restartNodes(
	ctx context.Context,
	chainIDs []ids.ID,
	participantsSpecs []network.ParticipantsSpec,
	validatorSpecs []network.PermissionlessValidatorSpec,
	removeValidatorSpecs []network.RemoveChainValidatorSpec,
	nodesToRestartForBlockchainConfigUpdate set.Set[string],
) (err error) {
	if (participantsSpecs != nil && validatorSpecs != nil) || (participantsSpecs != nil && removeValidatorSpecs != nil) ||
		(validatorSpecs != nil && removeValidatorSpecs != nil) {
		return errors.New("only one type of spec between chain specs, add permissionless validator specs and " +
			"remove validator specs can be supplied at one time")
	}
	fmt.Println()
	ln.logger.Info(log.Blue.Wrap(log.Bold.Wrap("restarting network")))

	nodeNames := slices.Collect(maps.Keys(ln.nodes))
	sort.Strings(nodeNames)

	for _, nodeName := range nodeNames {
		node := ln.nodes[nodeName]

		// delete node specific flag so as to use default one
		nodeConfig := node.GetConfig()

		previousTrackedChains := ""
		previousTrackedChainsIntf, ok := nodeConfig.Flags[config.TrackChainsKey]
		ln.logger.Info("checking track-chains for node",
			"nodeName", nodeName,
			"hasFlag", ok,
			"flagValue", previousTrackedChainsIntf,
		)
		if ok {
			previousTrackedChains, ok = previousTrackedChainsIntf.(string)
			if !ok {
				return fmt.Errorf("expected node config %s to have type string obtained %T", config.TrackChainsKey, previousTrackedChainsIntf)
			}
		}

		trackChainIDsSet := set.Set[string]{}
		if previousTrackedChains != "" {
			for _, s := range strings.Split(previousTrackedChains, ",") {
				trackChainIDsSet.Add(s)
			}
		}

		// OPTIMIZATION: Nodes with track-chains=all auto-discover all chains.
		// They don't need restart for new chain deployment - VM hot-reload is enough.
		// This prevents race conditions where VMs are killed during initialization.
		hasTrackAll := trackChainIDsSet.Contains("all")
		ln.logger.Info("track-chains check result",
			"nodeName", nodeName,
			"previousTrackedChains", previousTrackedChains,
			"hasTrackAll", hasTrackAll,
		)
		if hasTrackAll {
			ln.logger.Info("node has track-chains=all, skipping restart",
				"nodeName", nodeName,
			)
			continue
		}

		needsRestart := false
		for _, validatorSpec := range validatorSpecs {
			if validatorSpec.NodeName == node.name {
				trackChainIDsSet.Add(validatorSpec.ChainID)
				needsRestart = true
			}
		}

		for _, removeValidatorSpec := range removeValidatorSpecs {
			for _, toRemoveNode := range removeValidatorSpec.NodeNames {
				if toRemoveNode == node.name {
					trackChainIDsSet.Remove(removeValidatorSpec.ChainID)
					needsRestart = true
				}
			}
		}

		for i, chainID := range chainIDs {
			for _, participant := range participantsSpecs[i].Participants {
				if participant == nodeName {
					trackChainIDsSet.Add(chainID.String())
					needsRestart = true
				}
			}
		}

		trackChainIDs := trackChainIDsSet.List()
		sort.Strings(trackChainIDs)

		tracked := strings.Join(trackChainIDs, ",")
		nodeConfig.Flags[config.TrackChainsKey] = tracked

		if participantsSpecs != nil {
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
			ln.logger.Info(log.Green.Wrap(fmt.Sprintf("restarting node %s to stop tracking chains %s", nodeName, tracked)))
		} else {
			ln.logger.Info(log.Green.Wrap(fmt.Sprintf("restarting node %s to track chains %s", nodeName, tracked)))
		}

		if err := ln.restartNode(ctx, nodeName, "", "", "", nil, nil, nil); err != nil {
			return err
		}
	}
	// Use a fresh context for health check during planned restarts to avoid
	// context cancellation from onStopCh which is triggered during node restarts.
	// The parent context (ctx) can be canceled by the stop channel during restartNode,
	// but we still need to wait for nodes to become healthy after restart.
	healthCtx, healthCancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer healthCancel()
	if err := ln.healthy(healthCtx); err != nil {
		return err
	}

	// Wait for peers to reconnect after restart
	// This is critical for P-Chain sync to work
	if err := ln.waitForPeerConnectivity(healthCtx); err != nil {
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
	utxoAssetID ids.ID
}

// getDefaultKey loads the first key from ~/.lux/keys for wallet operations.
// Priority: MNEMONIC > PRIVATE_KEY > disk keys
func getDefaultKey() (*secp256k1.PrivateKey, error) {
	// If MNEMONIC is set, derive key using BIP44 path m/44'/60'/0'/0/0 (Ethereum standard)
	// CRITICAL: This MUST match the derivation path used in genesis allocations
	// (keys.DeriveValidatorFromMnemonic uses m/44'/60'/0'/0/{index})
	if mnemonic := os.Getenv("MNEMONIC"); mnemonic != "" {
		fmt.Printf("🔑 getDefaultKey: Using MNEMONIC (len=%d)\n", len(mnemonic))

		// Use the SAME derivation function as genesis allocations
		// This ensures wallet operations use keys that have funds allocated in genesis
		vk, err := keys.DeriveValidatorFromMnemonic(mnemonic, 0)
		if err != nil {
			return nil, fmt.Errorf("failed to derive key from mnemonic: %w", err)
		}
		privKey, err := secp256k1.ToPrivateKey(vk.ECPrivateKey)
		if err != nil {
			return nil, fmt.Errorf("failed to convert key: %w", err)
		}

		pubKey := privKey.PublicKey()
		walletAddr := ids.ShortID(pubKey.Address())
		fmt.Printf("🔑 Wallet address (from mnemonic m/44'/60'/0'/0/0): %s\n", walletAddr.String())

		return privKey, nil
	}

	// If PRIVATE_KEY is set, use it directly (hex encoded, 64 chars = 32 bytes)
	if privKeyHex := os.Getenv("PRIVATE_KEY"); privKeyHex != "" {
		fmt.Printf("🔑 getDefaultKey: Using PRIVATE_KEY (len=%d): %s...\n", len(privKeyHex), privKeyHex[:16])
		privKeyBytes, err := hex.DecodeString(privKeyHex)
		if err != nil {
			return nil, fmt.Errorf("failed to decode PRIVATE_KEY: %w", err)
		}
		privKey, err := secp256k1.ToPrivateKey(privKeyBytes)
		if err != nil {
			return nil, err
		}
		pubKey := privKey.PublicKey()
		addr := ids.ShortID(pubKey.Address())
		fmt.Printf("🔑 getDefaultKey: Derived address ShortID: %s\n", addr.String())
		return privKey, nil
	}

	// Fall back to loading from disk
	fmt.Printf("🔑 getDefaultKey: Falling back to disk keys\n")
	loadedKeys, err := LoadOrGenerateKeys("", 1)
	if err != nil {
		return nil, fmt.Errorf("failed to load keys from ~/.lux/keys: %w", err)
	}
	if len(loadedKeys) == 0 {
		return nil, errors.New("no keys found in ~/.lux/keys")
	}
	fmt.Printf("🔑 getDefaultKey: Loaded key from disk: %s...\n", loadedKeys[0].PrivKeyHex[:16])
	// Convert hex to private key
	privKeyBytes, err := hex.DecodeString(loadedKeys[0].PrivKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid key format: %w", err)
	}
	return secp256k1.ToPrivateKey(privKeyBytes)
}

func newWallet(
	ctx context.Context,
	uri string,
	preloadTXs []ids.ID,
) (*wallet, error) {
	// Load key from ~/.lux/keys - no hardcoded keys
	privKey, err := getDefaultKey()
	if err != nil {
		return nil, fmt.Errorf("failed to get wallet key: %w", err)
	}
	kc := secp256k1fx.NewKeychain(privKey)

	// Use dedicated timeout context for FetchState to avoid parent context cancellation propagation
	fetchCtx, fetchCancel := createDefaultCtx(ctx)
	luxState, err := primary.FetchState(fetchCtx, uri, kc.Addrs)
	fetchCancel()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch state from %s: %w", uri, err)
	}

	pClient := platformvm.NewClient(uri)
	pTXs := make(map[ids.ID]*txs.Tx)
	for _, id := range preloadTXs {
		// Use dedicated timeout context for GetTx to avoid parent context cancellation propagation
		getTxCtx, getTxCancel := createDefaultCtx(ctx)
		txBytes, err := pClient.GetTx(getTxCtx, id)
		getTxCancel()
		if err != nil {
			return nil, fmt.Errorf("failed to get tx %s: %w", id, err)
		}
		tx, err := txs.Parse(txBytes)
		if err != nil {
			return nil, err
		}
		pTXs[id] = tx
	}
	pUTXOs := common.NewChainUTXOs(constants.PlatformChainID, luxState.UTXOs)
	xChainID := luxState.XCTX.BlockchainID
	xUTXOs := common.NewChainUTXOs(xChainID, luxState.UTXOs)
	var w wallet
	w.addr = privKey.PublicKey().Address()
	// TODO: Create owners map instead of pTXs
	w.pBackend = pwallet.NewBackend(luxState.PCTX, pUTXOs, pTXs)
	w.pBuilder = pbuilder.New(kc.Addrs, luxState.PCTX, w.pBackend)
	// Wrap the keychain for wallet compatibility
	wrappedKC := secp256k1fx.WrapKeychain(kc)
	w.pSigner = psigner.New(wrappedKC, w.pBackend)
	// Create P-Chain wallet client
	walletClient := pwallet.NewClient(pClient, w.pBackend)
	w.pWallet = pwalletwallet.New(walletClient, w.pBuilder, w.pSigner)

	xBackend := x.NewBackend(luxState.XCTX, xUTXOs)
	xBuilder := xbuilder.New(kc.Addrs, luxState.XCTX, xBackend)
	// Reuse the wrapped keychain from above
	xSigner := xsigner.New(wrappedKC, xBackend)
	w.xWallet = x.NewWallet(xBuilder, xSigner, xBackend)
	w.xChainID = xChainID
	w.utxoAssetID = luxState.PCTX.UTXOAssetID
	return &w, nil
}

func (w *wallet) reload(uri string) {
	pClient := platformvm.NewClient(uri)
	walletClient := pwallet.NewClient(pClient, w.pBackend)
	w.pWallet = pwalletwallet.New(walletClient, w.pBuilder, w.pSigner)
}

// add all nodes as validators of the primary network, in case they are not
// the validation starts as soon as possible and its duration is as long as possible, that is,
// it is set to max accepted duration by node
func (ln *localNetwork) addPrimaryValidators(
	ctx context.Context,
	platformCli *platformvm.Client,
	w *wallet,
) error {
	ln.logger.Info(log.Green.Wrap("adding the nodes as primary network validators"))
	// ref. https://docs.lux.network/build/node-apis/p-chain/#platformgetcurrentvalidators
	// Use different variable name to avoid shadowing outer context
	apiCtx, cancel := createDefaultCtx(ctx)
	vdrs, err := platformCli.GetCurrentValidators(apiCtx, constants.PrimaryNetworkID, nil)
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
		blsSecretKey, err := bls.SecretKeyFromBytes(blsKeyBytes)
		if err != nil {
			return err
		}
		proofOfPossession, err := signer.NewProofOfPossession(blsSecretKey)
		if err != nil {
			return err
		}
		// Use different variable name to avoid shadowing outer context
		txCtx, txCancel := createDefaultCtx(ctx)
		tx, err := w.pWallet.IssueAddPermissionlessValidatorTx(
			&txs.ChainValidator{
				Validator: txs.Validator{
					NodeID: nodeID,
					Start:  uint64(time.Now().Add(validationStartOffset).Unix()),
					End:    uint64(time.Now().Add(validationDuration).Unix()),
					Wght:   genesis.LocalParams.MinValidatorStake,
				},
				Chain: ids.Empty,
			},
			proofOfPossession,
			w.utxoAssetID,
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{w.addr},
			},
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{w.addr},
			},
			10*10000, // 10% fee percent, times 10000 to make it as shares
			common.WithContext(txCtx),
			defaultPoll,
		)
		txCancel()
		if err != nil {
			return fmt.Errorf("P-Wallet Tx Error %s %w, node ID %s", "IssueAddPermissionlessValidatorTx", err, nodeID.String())
		}
		ln.logger.Info("added node as primary chain validator", "node-name", nodeName, "node-ID", nodeID.String(), "tx-ID", tx.ID().String())
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
	// Use different variable name to avoid shadowing outer context
	txCtx, cancel := createDefaultCtx(ctx)
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
		common.WithContext(txCtx),
		defaultPoll,
	)
	if err != nil {
		return ids.Empty, err
	}
	return tx.ID(), nil
}

func exportXChainToPChain(ctx context.Context, w *wallet, owner *secp256k1fx.OutputOwners, chainAssetID ids.ID, assetAmount uint64) error {
	// Use different variable name to avoid shadowing outer context
	txCtx, cancel := createDefaultCtx(ctx)
	defer cancel()
	_, err := w.xWallet.IssueExportTx(
		ids.Empty,
		[]*utxo.TransferableOutput{
			{
				Asset: utxo.Asset{
					ID: chainAssetID,
				},
				Out: &secp256k1fx.TransferOutput{
					Amt:          assetAmount,
					OutputOwners: *owner,
				},
			},
		},
		common.WithContext(txCtx),
		defaultPoll,
	)
	return err
}

func importPChainFromXChain(ctx context.Context, w *wallet, owner *secp256k1fx.OutputOwners, xChainID ids.ID) error {
	pWallet := w.pWallet
	// Use different variable name to avoid shadowing outer context
	txCtx, cancel := createDefaultCtx(ctx)
	defer cancel()
	_, err := pWallet.IssueImportTx(
		xChainID,
		owner,
		common.WithContext(txCtx),
		defaultPoll,
	)
	return err
}

// fundPChainFromXChain ensures the wallet has liquid P-chain funds for chain creation.
// With direct P-chain genesis allocations, this may not need to do anything.
// Falls back to X->P export/import if P-chain has insufficient funds.
func fundPChainFromXChain(ctx context.Context, w *wallet, logger log.Logger) error {
	// Amount needed for chain operations (1 LUX should be plenty for fees)
	const requiredAmount = uint64(1_000_000_000) // 1 LUX in nLUX

	// First check if P-chain already has sufficient funds
	balances, err := w.pBuilder.GetBalance()
	if err != nil {
		log.Warn("failed to check P-chain balance, will try X->P transfer", "error", err.Error())
	} else {
		pBalance := balances[w.utxoAssetID]
		log.Info("P-chain balance check", "balance", pBalance, "required", requiredAmount)
		if pBalance >= requiredAmount {
			log.Info(log.Green.Wrap("P-chain already has sufficient funds, skipping X->P transfer"))
			return nil
		}
	}

	// P-chain doesn't have enough, try to export from X-chain
	owner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{w.addr},
	}

	log.Info(log.Green.Wrap("funding P-chain from X-chain for chain creation"))
	fmt.Printf("💰 Exporting %d nLUX from X-chain to P-chain for address %s\n", requiredAmount, w.addr.String())

	// Export LUX from X-chain to P-chain
	if err := exportXChainToPChain(ctx, w, owner, w.utxoAssetID, requiredAmount); err != nil {
		return fmt.Errorf("failed to export from X-chain: %w", err)
	}
	fmt.Printf("✅ Export from X-chain completed\n")

	// Import on P-chain
	if err := importPChainFromXChain(ctx, w, owner, w.xChainID); err != nil {
		return fmt.Errorf("failed to import on P-chain: %w", err)
	}
	fmt.Printf("✅ Import on P-chain completed, wallet funded\n")

	return nil
}

func (ln *localNetwork) removeChainValidators(
	ctx context.Context,
	removeParticipantsSpecs []network.RemoveChainValidatorSpec,
) error {
	ln.logger.Info("removing chain validator tx")
	removeParticipantsSpecIDs := make([]ids.ID, len(removeParticipantsSpecs))
	clientURI, err := ln.getClientURI()
	if err != nil {
		return err
	}
	platformCli := platformvm.NewClient(clientURI)
	// wallet needs txs for all previously created chains
	preloadTXs := make([]ids.ID, len(removeParticipantsSpecs))
	for i, removeParticipantsSpec := range removeParticipantsSpecs {
		chainID, err := ids.FromString(removeParticipantsSpec.ChainID)
		if err != nil {
			return err
		}
		preloadTXs[i] = chainID
	}
	w, err := newWallet(ctx, clientURI, preloadTXs)
	if err != nil {
		return err
	}
	ln.logger.Info(log.Green.Wrap("removing the nodes as chain validators"))
	for i, participantsSpec := range removeParticipantsSpecs {
		chainID, err := ids.FromString(participantsSpec.ChainID)
		if err != nil {
			return err
		}
		// Use different variable name to avoid shadowing outer context
		apiCtx, apiCancel := createDefaultCtx(ctx)
		vs, err := platformCli.GetCurrentValidators(apiCtx, chainID, nil)
		apiCancel()
		if err != nil {
			return err
		}
		chainValidators := set.Set[ids.NodeID]{}
		for _, v := range vs {
			chainValidators.Add(v.NodeID)
		}
		toRemoveNodes := participantsSpec.NodeNames
		for _, nodeName := range toRemoveNodes {
			node, b := ln.nodes[nodeName]
			if !b {
				return fmt.Errorf("node %s is not in network nodes", nodeName)
			}
			nodeID := node.GetNodeID()
			if isValidator := chainValidators.Contains(nodeID); !isValidator {
				return fmt.Errorf("node %s is currently not a chain validator of chain %s", nodeName, chainID.String())
			}
			// Use different variable name to avoid shadowing outer context
			txCtx, txCancel := createDefaultCtx(ctx)
			tx, err := w.pWallet.IssueRemoveChainValidatorTx(
				nodeID,
				chainID,
				common.WithContext(txCtx),
				defaultPoll,
			)
			txCancel()
			if err != nil {
				return fmt.Errorf("P-Wallet Tx Error %s %w, node ID %s, chainID %s", "IssueRemoveChainValidatorTx", err, nodeID.String(), chainID.String())
			}
			ln.logger.Info("removed node as chain validator",
				"node-name", nodeName,
				"node-ID", nodeID.String(),
				"chain-ID", chainID.String(),
				"tx-ID", tx.ID().String(),
			)
			removeParticipantsSpecIDs[i] = tx.ID()
		}
	}
	return ln.restartNodes(ctx, nil, nil, nil, removeParticipantsSpecs, nil)
}

func (ln *localNetwork) addPermissionlessValidators(
	ctx context.Context,
	validatorSpecs []network.PermissionlessValidatorSpec,
) error {
	ln.logger.Info("adding permissionless validator tx")
	clientURI, err := ln.getClientURI()
	if err != nil {
		return err
	}
	platformCli := platformvm.NewClient(clientURI)
	// wallet needs txs for all previously created chains
	preloadTXs := make([]ids.ID, len(validatorSpecs))
	for i, validatorSpec := range validatorSpecs {
		chainID, err := ids.FromString(validatorSpec.ChainID)
		if err != nil {
			return err
		}
		preloadTXs[i] = chainID
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
			ln.logger.Info(log.Green.Wrap(fmt.Sprintf("adding new participant %s", validatorSpec.NodeName)))
			if _, err := ln.addNode(node.Config{Name: validatorSpec.NodeName}); err != nil {
				return err
			}
		}
	}
	if err := ln.healthy(ctx); err != nil {
		return err
	}

	// just ensure all nodes are primary validators (so can be chain validators)
	if err := ln.addPrimaryValidators(ctx, platformCli, w); err != nil {
		return err
	}

	// wait for nodes to be primary validators before trying to add them as chain ones
	if err = ln.waitPrimaryValidators(ctx, platformCli); err != nil {
		return err
	}

	// Use different variable name to avoid shadowing outer context
	apiCtx, cancel := createDefaultCtx(ctx)
	vs, err := platformCli.GetCurrentValidators(apiCtx, constants.PrimaryNetworkID, nil)
	cancel()
	if err != nil {
		return err
	}
	primaryValidatorsEndtime := make(map[ids.NodeID]time.Time)
	for _, v := range vs {
		primaryValidatorsEndtime[v.NodeID] = time.Unix(int64(v.EndTime), 0)
	}

	for _, validatorSpec := range validatorSpecs {
		ln.logger.Info(log.Green.Wrap("adding permissionless validator"), "node ", validatorSpec.NodeName)
		// Use different variable name to avoid shadowing outer context
		txCtx, txCancel := createDefaultCtx(ctx)
		validatorNodeID := ln.nodes[validatorSpec.NodeName].nodeID
		chainID, err := ids.FromString(validatorSpec.ChainID)
		if err != nil {
			txCancel()
			return err
		}
		assetID, err := ids.FromString(validatorSpec.AssetID)
		if err != nil {
			txCancel()
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
			&txs.ChainValidator{
				Validator: txs.Validator{
					NodeID: validatorNodeID,
					Start:  startTime,
					End:    endTime,
					Wght:   validatorSpec.StakedAmount,
				},
				Chain: chainID,
			},
			&signer.Empty{},
			assetID,
			owner,
			&secp256k1fx.OutputOwners{},
			reward.PercentDenominator,
			common.WithContext(txCtx),
			defaultPoll,
		)
		txCancel()
		if err != nil {
			return err
		}
		ln.logger.Info("Validator successfully added as permissionless validator", "TX ID", tx.ID().String())
	}
	return ln.restartNodes(ctx, nil, nil, validatorSpecs, nil, nil)
}

func (ln *localNetwork) transformToElasticChains(
	ctx context.Context,
	elasticParticipantsSpecs []network.ElasticChainSpec,
) ([]ids.ID, []ids.ID, error) {
	ln.logger.Info("transforming elastic chain tx")
	elasticChainIDs := make([]ids.ID, len(elasticParticipantsSpecs))
	assetIDs := make([]ids.ID, len(elasticParticipantsSpecs))
	clientURI, err := ln.getClientURI()
	if err != nil {
		return nil, nil, err
	}
	// wallet needs txs for all previously created chains
	var preloadTXs []ids.ID
	for _, elasticParticipantsSpec := range elasticParticipantsSpecs {
		if elasticParticipantsSpec.ChainID == nil {
			return nil, nil, errors.New("elastic chain spec has no chain ID")
		} else {
			chainID, err := ids.FromString(*elasticParticipantsSpec.ChainID)
			if err != nil {
				return nil, nil, err
			}
			preloadTXs = append(preloadTXs, chainID)
		}
	}
	w, err := newWallet(ctx, clientURI, preloadTXs)
	if err != nil {
		return nil, nil, err
	}

	for i, elasticParticipantsSpec := range elasticParticipantsSpecs {
		ln.logger.Info(log.Green.Wrap("transforming elastic chain"), "chain ID", *elasticParticipantsSpec.ChainID)

		chainAssetID, err := getXChainAssetID(ctx, w, elasticParticipantsSpec.AssetName, elasticParticipantsSpec.AssetSymbol, elasticParticipantsSpec.MaxSupply)
		if err != nil {
			return nil, nil, err
		}
		assetIDs[i] = chainAssetID
		ln.logger.Info("created asset ID", "asset-ID", chainAssetID.String())
		owner := &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				w.addr,
			},
		}
		err = exportXChainToPChain(ctx, w, owner, chainAssetID, elasticParticipantsSpec.MaxSupply)
		if err != nil {
			return nil, nil, err
		}
		ln.logger.Info("exported asset to P-Chain")
		err = importPChainFromXChain(ctx, w, owner, w.xChainID)
		if err != nil {
			return nil, nil, err
		}
		ln.logger.Info("imported asset from X-Chain")
		chainID, err := ids.FromString(*elasticParticipantsSpec.ChainID)
		if err != nil {
			return nil, nil, err
		}
		// Use different variable name to avoid shadowing outer context
		txCtx, txCancel := createDefaultCtx(ctx)
		transformChainTx, err := w.pWallet.IssueTransformChainTx(chainID, chainAssetID,
			elasticParticipantsSpec.InitialSupply, elasticParticipantsSpec.MaxSupply, elasticParticipantsSpec.MinConsumptionRate,
			elasticParticipantsSpec.MaxConsumptionRate, elasticParticipantsSpec.MinValidatorStake, elasticParticipantsSpec.MaxValidatorStake,
			elasticParticipantsSpec.MinStakeDuration, elasticParticipantsSpec.MaxStakeDuration, elasticParticipantsSpec.MinDelegationFee,
			elasticParticipantsSpec.MinDelegatorStake, elasticParticipantsSpec.MaxValidatorWeightFactor, elasticParticipantsSpec.UptimeRequirement,
			common.WithContext(txCtx),
			defaultPoll,
		)
		txCancel()
		if err != nil {
			return nil, nil, err
		}
		ln.logger.Info("Chain transformed into elastic chain", "TX ID", transformChainTx.ID().String())
		elasticChainIDs[i] = transformChainTx.ID()
		ln.chainID2ElasticChainID[chainID] = transformChainTx.ID()
	}
	return elasticChainIDs, assetIDs, nil
}

func (ln *localNetwork) GetElasticChainID(_ context.Context, chainID ids.ID) (ids.ID, error) {
	elasticChainID, ok := ln.chainID2ElasticChainID[chainID]
	if !ok {
		return ids.Empty, fmt.Errorf("chainID not found on map: %s", chainID)
	}
	return elasticChainID, nil
}

func createChains(
	ctx context.Context,
	numChains uint32,
	w *wallet,
	logger log.Logger,
) ([]ids.ID, error) {
	fmt.Println()
	log.Info(log.Green.Wrap("creating chains"), "num-chains", numChains)
	chainIDs := make([]ids.ID, numChains)
	for i := uint32(0); i < numChains; i++ {
		log.Info("creating chain tx")
		// Use different variable name to avoid shadowing outer context
		txCtx, txCancel := createDefaultCtx(ctx)
		tx, err := w.pWallet.IssueCreateNetworkTx(
			&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{w.addr},
			},
			common.WithContext(txCtx),
			defaultPoll,
		)
		txCancel()
		if err != nil {
			return nil, fmt.Errorf("P-Wallet Tx Error %s %w", "IssueCreateChainTx", err)
		}
		// Get the chain ID from the transaction
		chainID := tx.ID()
		log.Info("created chain tx", "chain-ID", chainID.String())
		chainIDs[i] = chainID
	}
	return chainIDs, nil
}

// add the nodes in chain participant as validators of the given chains, in case they are not
// the validation starts as soon as possible and its duration is as long as possible, that is,
// it ends at the time the primary network validation ends for the node
func (ln *localNetwork) addChainValidators(
	ctx context.Context,
	platformCli *platformvm.Client,
	w *wallet,
	chainIDs []ids.ID,
	participantsSpecs []network.ParticipantsSpec,
) error {
	ln.logger.Info(log.Green.Wrap("adding the nodes as chain validators"))
	for i, chainID := range chainIDs {
		ln.logger.Info("getting primary validators for chain", "index", i, "chain-ID", chainID.String())
		// Use different variable name to avoid shadowing outer context
		apiCtx1, cancel1 := createDefaultCtx(ctx)
		vs, err := platformCli.GetCurrentValidators(apiCtx1, constants.PrimaryNetworkID, nil)
		cancel1()
		if err != nil {
			ln.logger.Error("failed to get primary validators", "error", err.Error())
			return fmt.Errorf("failed to get primary validators: %w", err)
		}
		ln.logger.Info("got primary validators", "count", len(vs))
		primaryValidatorsEndtime := make(map[ids.NodeID]time.Time)
		for _, v := range vs {
			primaryValidatorsEndtime[v.NodeID] = time.Unix(int64(v.EndTime), 0)
		}
		ln.logger.Info("getting current validators for chain", "chain-ID", chainID.String())
		// Use different variable name to avoid shadowing outer context
		apiCtx2, cancel2 := createDefaultCtx(ctx)
		vs, err = platformCli.GetCurrentValidators(apiCtx2, chainID, nil)
		cancel2()
		if err != nil {
			ln.logger.Error("failed to get current validators for chain", "chain-ID", chainID.String(), "error", err.Error())
			return fmt.Errorf("failed to get current validators for chain %s: %w", chainID.String(), err)
		}
		ln.logger.Info("got current validators for chain", "chain-ID", chainID.String(), "count", len(vs))
		chainValidators := set.Set[ids.NodeID]{}
		for _, v := range vs {
			chainValidators.Add(v.NodeID)
		}
		participants := participantsSpecs[i].Participants
		for _, nodeName := range participants {
			node, b := ln.nodes[nodeName]
			if !b {
				return fmt.Errorf("participant node %s is not in network nodes", nodeName)
			}
			nodeID := node.GetNodeID()
			if isValidator := chainValidators.Contains(nodeID); isValidator {
				continue
			}
			// Use different variable name to avoid shadowing outer context
			txCtx, txCancel := createDefaultCtx(ctx)
			tx, err := w.pWallet.IssueAddChainValidatorTx(
				&txs.ChainValidator{
					Validator: txs.Validator{
						NodeID: nodeID,
						// reasonable delay in most/slow test environments
						Start: uint64(time.Now().Add(validationStartOffset).Unix()),
						End:   uint64(primaryValidatorsEndtime[nodeID].Unix()),
						Wght:  chainValidatorsWeight,
					},
					Chain: chainID,
				},
				common.WithContext(txCtx),
				defaultPoll,
			)
			txCancel()
			if err != nil {
				return fmt.Errorf("P-Wallet Tx Error %s %w, node ID %s, chainID %s", "IssueAddChainValidatorTx", err, nodeID.String(), chainID.String())
			}
			ln.logger.Info("added node as a chain validator to chain",
				"node-name", nodeName,
				"node-ID", nodeID.String(),
				"chain-ID", chainID.String(),
				"tx-ID", tx.ID().String(),
			)
		}
	}
	return nil
}

// waits until all nodes start validating the primary network
// Uses a dedicated timeout context to avoid inheriting an already-depleted parent context.
func (ln *localNetwork) waitPrimaryValidators(
	ctx context.Context,
	platformCli *platformvm.Client,
) error {
	ln.logger.Info(log.Green.Wrap("waiting for the nodes to become primary validators"),
		"numNodes", len(ln.nodes),
		"timeout", chainValidatorWaitTimeout.String(),
	)

	// Use a dedicated timeout context for this wait operation.
	waitCtx, waitCancel := context.WithTimeout(context.Background(), chainValidatorWaitTimeout)
	defer waitCancel()

	startTime := time.Now()
	pollCount := 0

	for {
		pollCount++
		ready := true
		var missingValidators []string

		// Use a different variable name to avoid shadowing the outer context
		apiCtx, cancel := createDefaultCtx(waitCtx)
		vs, err := platformCli.GetCurrentValidators(apiCtx, constants.PrimaryNetworkID, nil)
		cancel()
		if err != nil {
			ln.logger.Warn("waitPrimaryValidators: failed to get validators, retrying",
				"error", err.Error(),
				"pollCount", pollCount,
			)
			ready = false
		} else {
			primaryValidators := set.Set[ids.NodeID]{}
			for _, v := range vs {
				primaryValidators.Add(v.NodeID)
			}
			for nodeName, node := range ln.nodes {
				nodeID := node.GetNodeID()
				if isValidator := primaryValidators.Contains(nodeID); !isValidator {
					ready = false
					missingValidators = append(missingValidators, fmt.Sprintf("%s(%s)", nodeName, nodeID.String()[:8]))
				}
			}
		}

		if ready {
			ln.logger.Info(log.Green.Wrap("all nodes are now primary validators"),
				"elapsed", time.Since(startTime).String(),
				"pollCount", pollCount,
			)
			return nil
		}

		// Log progress every 25 polls (5 seconds at 200ms interval)
		if pollCount%25 == 0 {
			ln.logger.Info("waitPrimaryValidators: still waiting for validators",
				"elapsed", time.Since(startTime).String(),
				"pollCount", pollCount,
				"missingValidators", len(missingValidators),
			)
		}

		select {
		case <-ln.onStopCh:
			return errAborted
		case <-waitCtx.Done():
			return fmt.Errorf("timeout waiting for primary validators after %s (%d polls): missing validators: %v",
				time.Since(startTime).String(), pollCount, missingValidators)
		case <-ctx.Done():
			// Parent context cancelled - but we should still try to complete if within our timeout
			ln.logger.Warn("waitPrimaryValidators: parent context cancelled, but continuing within our timeout")
		case <-time.After(waitForValidatorsPullFrequency):
		}
	}
}

// waits until all chain participants start validating the chainID, for all given chains
// Uses a dedicated timeout context to avoid inheriting an already-depleted parent context.
func (ln *localNetwork) waitChainValidators(
	ctx context.Context,
	platformCli *platformvm.Client,
	chainIDs []ids.ID,
	participantsSpecs []network.ParticipantsSpec,
) error {
	ln.logger.Info(log.Green.Wrap("waiting for the nodes to become chain validators"),
		"numChains", len(chainIDs),
		"timeout", chainValidatorWaitTimeout.String(),
		"validatorStartOffset", validationStartOffset.String(),
	)

	// Use a dedicated timeout context for this wait operation.
	// The parent context may already be partially consumed by earlier operations.
	waitCtx, waitCancel := context.WithTimeout(context.Background(), chainValidatorWaitTimeout)
	defer waitCancel()

	startTime := time.Now()
	pollCount := 0

	for {
		pollCount++
		ready := true
		var missingValidators []string

		for i, chainID := range chainIDs {
			// Use a different variable name to avoid shadowing the outer context
			apiCtx, cancel := createDefaultCtx(waitCtx)
			vs, err := platformCli.GetCurrentValidators(apiCtx, chainID, nil)
			cancel()
			if err != nil {
				// Log the error but continue - the chain may not be registered yet
				ln.logger.Warn("waitChainValidators: failed to get validators, retrying",
					"chainID", chainID.String(),
					"error", err.Error(),
					"pollCount", pollCount,
				)
				ready = false
				continue
			}
			chainValidators := set.Set[ids.NodeID]{}
			for _, v := range vs {
				chainValidators.Add(v.NodeID)
			}
			participants := participantsSpecs[i].Participants
			for _, nodeName := range participants {
				node, b := ln.nodes[nodeName]
				if !b {
					return fmt.Errorf("participant node %s is not in network nodes", nodeName)
				}
				nodeID := node.GetNodeID()
				if isValidator := chainValidators.Contains(nodeID); !isValidator {
					ready = false
					missingValidators = append(missingValidators, fmt.Sprintf("%s(%s)", nodeName, nodeID.String()[:8]))
				}
			}
		}

		if ready {
			ln.logger.Info(log.Green.Wrap("all nodes are now chain validators"),
				"elapsed", time.Since(startTime).String(),
				"pollCount", pollCount,
			)
			return nil
		}

		// Log progress every 25 polls (5 seconds at 200ms interval)
		if pollCount%25 == 0 {
			ln.logger.Info("waitChainValidators: still waiting for validators",
				"elapsed", time.Since(startTime).String(),
				"pollCount", pollCount,
				"missingValidators", len(missingValidators),
			)
		}

		select {
		case <-ln.onStopCh:
			return errAborted
		case <-waitCtx.Done():
			return fmt.Errorf("timeout waiting for chain validators after %s (%d polls): missing validators: %v",
				time.Since(startTime).String(), pollCount, missingValidators)
		case <-ctx.Done():
			// Parent context cancelled - but we should still try to complete if within our timeout
			ln.logger.Warn("waitChainValidators: parent context cancelled, but continuing within our timeout")
		case <-time.After(waitForValidatorsPullFrequency):
		}
	}
}

// reload VM plugins on all nodes and verify they're available
func (ln *localNetwork) reloadVMPlugins(ctx context.Context) error {
	// Use unified config to resolve plugin directory for diagnostics
	pluginDir := config.ResolvePluginDir()
	ln.logger.Info(log.Green.Wrap("reloading plugin binaries"),
		"pluginDir", pluginDir,
	)

	// Track newly loaded VMs for verification
	var newlyLoadedVMs []ids.ID

	for nodeName, node := range ln.nodes {
		if node.paused {
			continue
		}
		uri := node.GetURL()
		adminCli := admin.NewClient(uri)
		// Use different variable name to avoid shadowing outer context
		apiCtx, cancel := createDefaultCtx(ctx)
		loadedVMs, failedVMs, err := adminCli.LoadVMs(apiCtx)
		cancel()
		if err != nil {
			ln.logger.Error("reloadVMPlugins: failed to load VMs on node",
				"error", err.Error(),
				"nodeName", nodeName,
				"nodeURI", uri,
				"pluginDir", pluginDir,
			)
			return fmt.Errorf("failed to load VMs on node %s (%s): %w (plugin dir: %s)", nodeName, uri, err, pluginDir)
		}
		if len(failedVMs) > 0 {
			// Log detailed information about each failed VM
			for vmID, vmErr := range failedVMs {
				ln.logger.Error("reloadVMPlugins: VM failed to load",
					"vmID", vmID,
					"error", vmErr,
					"nodeName", nodeName,
					"nodeURI", uri,
					"pluginDir", pluginDir,
				)
			}
			return fmt.Errorf("%d VMs failed to load on node %s: %v (plugin dir: %s)", len(failedVMs), nodeName, failedVMs, pluginDir)
		}
		if len(loadedVMs) > 0 {
			ln.logger.Info("reloadVMPlugins: successfully loaded VMs",
				"nodeName", nodeName,
				"loadedVMs", loadedVMs,
				"pluginDir", pluginDir,
			)
			// Collect newly loaded VMs for verification
			for vmID := range loadedVMs {
				newlyLoadedVMs = append(newlyLoadedVMs, vmID)
			}
		}
	}

	// If we loaded new VMs, verify they're actually available on all nodes
	if len(newlyLoadedVMs) > 0 {
		if err := ln.verifyVMsAvailable(ctx, newlyLoadedVMs); err != nil {
			return fmt.Errorf("VMs loaded but not available: %w", err)
		}
	}

	return nil
}

// verifyVMsAvailable checks that all specified VMs are available on all nodes
// with retry logic to handle hot-loading delays
func (ln *localNetwork) verifyVMsAvailable(ctx context.Context, vmIDs []ids.ID) error {
	const (
		// FAIL FAST: Reduced retries from 10 to 5 (2.5s max)
		maxRetries    = 5
		retryInterval = 500 * time.Millisecond
	)

	for nodeName, node := range ln.nodes {
		if node.paused {
			continue
		}
		uri := node.GetURL()
		adminCli := admin.NewClient(uri)

		for _, vmID := range vmIDs {
			var lastErr error
			available := false

			for attempt := 0; attempt < maxRetries; attempt++ {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}

				apiCtx, cancel := createDefaultCtx(ctx)
				registeredVMs, err := adminCli.ListVMs(apiCtx)
				cancel()

				if err != nil {
					lastErr = err
					time.Sleep(retryInterval)
					continue
				}

				// Check if our VM is in the list
				vmIDStr := vmID.String()
				if _, exists := registeredVMs[vmIDStr]; exists {
					available = true
					break
				}

				lastErr = fmt.Errorf("VM %s not found in registered VMs", vmIDStr)
				time.Sleep(retryInterval)
			}

			if !available {
				ln.logger.Error("verifyVMsAvailable: VM not available after retries",
					"vmID", vmID.String(),
					"nodeName", nodeName,
					"nodeURI", uri,
					"maxRetries", maxRetries,
					"lastError", lastErr,
				)
				return fmt.Errorf("VM %s not available on node %s after %d attempts: %v",
					vmID.String(), nodeName, maxRetries, lastErr)
			}

			ln.logger.Debug("verifyVMsAvailable: VM verified available",
				"vmID", vmID.String(),
				"nodeName", nodeName,
			)
		}
	}

	ln.logger.Info("verifyVMsAvailable: all VMs verified available on all nodes",
		"numVMs", len(vmIDs),
	)
	return nil
}

func createBlockchainTxs(
	ctx context.Context,
	chainSpecs []network.ChainSpec,
	w *wallet,
	logger log.Logger,
) ([]*txs.Tx, error) {
	fmt.Println()
	log.Info(log.Green.Wrap("creating tx for each custom chain"))
	blockchainTxs := make([]*txs.Tx, len(chainSpecs))
	for i, chainSpec := range chainSpecs {
		vmName := chainSpec.VMName
		vmID, err := utils.VMID(vmName)
		if err != nil {
			log.Error("createBlockchainTxs: invalid VM name",
				"error", err.Error(),
				"vmName", vmName,
			)
			return nil, fmt.Errorf("invalid VM name %s: %w", vmName, err)
		}
		genesisBytes := chainSpec.Genesis

		// Use BlockchainName if specified, otherwise fall back to VMName
		blockchainName := chainSpec.BlockchainName
		if blockchainName == "" {
			blockchainName = vmName
		}

		log.Info("creating blockchain tx",
			"vm-name", vmName,
			"blockchain-name", blockchainName,
			"vm-ID", vmID.String(),
			"bytes length of genesis", len(genesisBytes),
		)
		// Use different variable name to avoid shadowing outer context
		txCtx, txCancel := createDefaultCtx(ctx)
		chainID, err := ids.FromString(*chainSpec.ChainID)
		if err != nil {
			txCancel()
			log.Error("createBlockchainTxs: invalid chain ID",
				"error", err.Error(),
				"chainID", *chainSpec.ChainID,
				"vmName", vmName,
			)
			return nil, fmt.Errorf("invalid chain ID %s for VM %s: %w", *chainSpec.ChainID, vmName, err)
		}
		tx, err := w.pWallet.IssueCreateChainTx(
			chainID,
			genesisBytes,
			vmID,
			nil,
			blockchainName,
			common.WithContext(txCtx),
			defaultPoll,
		)
		txCancel()
		if err != nil {
			log.Error("createBlockchainTxs: failed to issue create chain transaction",
				"error", err.Error(),
				"vmName", vmName,
				"vmID", vmID.String(),
				"chainID", chainID.String(),
				"blockchainName", blockchainName,
				"genesisLength", len(genesisBytes),
			)
			return nil, fmt.Errorf("failure creating blockchain tx for VM %s (chainID=%s): %w", vmName, chainID.String(), err)
		}

		blockchainTxs[i] = tx
	}

	return blockchainTxs, nil
}

func (ln *localNetwork) setBlockchainConfigFiles(
	ctx context.Context,
	chainSpecs []network.ChainSpec,
	blockchainTxs []*txs.Tx,
	chainIDs []ids.ID,
	participantsSpecs []network.ParticipantsSpec,
	logger log.Logger,
) (set.Set[string], error) {
	fmt.Println()
	log.Info(log.Green.Wrap("creating config files for each custom chain"))
	nodesToRestart := set.Set[string]{}
	for i, chainSpec := range chainSpecs {
		// get chain participants
		participants := []string{}
		chainChainID, err := ids.FromString(*chainSpec.ChainID)
		if err != nil {
			return nil, err
		}
		for j, newChainID := range chainIDs {
			if chainChainID == newChainID {
				// chain is new, use participants from spec
				participants = participantsSpecs[j].Participants
			}
		}
		if len(participants) == 0 {
			// get participants from network
			nodeNames, err := ln.getChainValidatorsNodenames(ctx, chainChainID)
			if err != nil {
				return nil, err
			}
			participants = nodeNames
		}
		chainAlias := blockchainTxs[i].ID().String()

		// For EVM chains, write genesis.json to chainConfigs directory
		// This is required for the EVM to load the chain configuration
		if len(chainSpec.Genesis) > 0 {
			for _, nodeName := range participants {
				node, b := ln.nodes[nodeName]
				if !b {
					return nil, fmt.Errorf("participant node %s is not in network nodes", nodeName)
				}
				// Initialize GenesisConfigFiles map if nil
				if ln.nodes[nodeName].config.GenesisConfigFiles == nil {
					ln.nodes[nodeName].config.GenesisConfigFiles = make(map[string]string)
				}
				ln.nodes[nodeName].config.GenesisConfigFiles[chainAlias] = string(chainSpec.Genesis)

				// Check if node has track-chains=all - if so, skip restart
				// Nodes with track-chains=all auto-discover chains via hot-reload
				trackChainsIntf, hasTrackChains := node.config.Flags[config.TrackChainsKey]
				if hasTrackChains {
					if trackChainsStr, ok := trackChainsIntf.(string); ok && trackChainsStr == "all" {
						// Write genesis file directly to disk instead of waiting for restart
						if err := ln.writeChainGenesisFile(nodeName, chainAlias, string(chainSpec.Genesis)); err != nil {
							return nil, fmt.Errorf("failed to write genesis file for %s: %w", nodeName, err)
						}
						log.Debug("skipping restart for track-chains=all node", "nodeName", nodeName)
						continue
					}
				}
				nodesToRestart.Add(nodeName)
			}
			log.Info("set genesis config for chain",
				"chainAlias", chainAlias,
				"participants", len(participants),
				"genesisLength", len(chainSpec.Genesis),
			)
		}

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

// writeChainGenesisFile writes the chain genesis file directly to disk for a running node.
// This is used when track-chains=all is enabled, allowing chains to be deployed without restart.
func (ln *localNetwork) writeChainGenesisFile(nodeName, chainAlias, genesis string) error {
	node, ok := ln.nodes[nodeName]
	if !ok {
		return fmt.Errorf("node %s not found", nodeName)
	}

	// chainConfigDir = nodeDir/chainConfigs
	chainConfigDir := filepath.Join(node.GetDataDir(), chainConfigSubDir)
	if err := os.MkdirAll(filepath.Join(chainConfigDir, chainAlias), 0o750); err != nil {
		return fmt.Errorf("failed to create chain config dir: %w", err)
	}

	genesisPath := filepath.Join(chainConfigDir, chainAlias, genesisFileName)

	// Genesis is immutable - only write if file doesn't exist
	if _, err := os.Stat(genesisPath); err == nil {
		ln.logger.Info("genesis file already exists, skipping write (immutable)",
			"nodeName", nodeName,
			"chainAlias", chainAlias,
			"path", genesisPath,
		)
		return nil
	}

	if err := os.WriteFile(genesisPath, []byte(genesis), 0o644); err != nil {
		return fmt.Errorf("failed to write genesis file: %w", err)
	}

	ln.logger.Info("wrote genesis file for chain",
		"nodeName", nodeName,
		"chainAlias", chainAlias,
		"path", genesisPath,
	)
	return nil
}

func (ln *localNetwork) setChainConfigFiles(
	chainIDs []ids.ID,
	participantsSpecs []network.ParticipantsSpec,
) error {
	for i, chainID := range chainIDs {
		participants := participantsSpecs[i].Participants
		chainConfig := participantsSpecs[i].ChainConfig
		if chainConfig != nil {
			for _, nodeName := range participants {
				_, b := ln.nodes[nodeName]
				if !b {
					return fmt.Errorf("participant node %s is not in network nodes", nodeName)
				}
				ln.nodes[nodeName].config.ChainConfigFiles[chainID.String()] = string(chainConfig)
			}
		}
	}
	return nil
}

func (ln *localNetwork) createChains(
	ctx context.Context,
	chainSpecs []network.ChainSpec,
	blockchainTxs []*txs.Tx,
	w *wallet,
	logger log.Logger,
) error {
	fmt.Println()
	log.Info(log.Green.Wrap("creating each custom chain"))

	// Get platform client to check tx status
	clientURI, err := ln.getClientURI()
	if err != nil {
		return err
	}
	pClient := platformvm.NewClient(clientURI)

	for i, chainSpec := range chainSpecs {
		vmName := chainSpec.VMName
		vmID, err := utils.VMID(vmName)
		if err != nil {
			return err
		}
		blockchainID := blockchainTxs[i].ID()

		log.Info("creating blockchain",
			"vm-name", vmName,
			"vm-ID", vmID.String(),
			"blockchain-ID", blockchainID.String(),
		)

		// Check if the transaction was already committed (from createBlockchainTxs)
		// This can happen when nodes are restarted between tx creation and this call
		checkCtx, checkCancel := createDefaultCtx(ctx)
		txStatus, err := pClient.GetTxStatus(checkCtx, blockchainID)
		checkCancel()
		if err == nil && txStatus.Status.String() == "Committed" {
			log.Info("blockchain tx already committed, skipping re-issue",
				"vm-name", vmName,
				"blockchain-ID", blockchainID.String(),
			)
			continue
		}

		issueCtx, issueCancel := createDefaultCtx(ctx)
		defer issueCancel()

		err = w.pWallet.IssueTx(
			blockchainTxs[i],
			common.WithContext(issueCtx),
			defaultPoll,
		)
		if err != nil {
			return fmt.Errorf("P-Wallet Tx Error %s %w, blockchainID %s", "IssueCreateBlockchainTx", err, blockchainID.String())
		}

		log.Info("created a new blockchain",
			"vm-name", vmName,
			"vm-ID", vmID.String(),
			"blockchain-ID", blockchainID.String(),
		)
	}

	return nil
}

// createDefaultCtx creates a fresh timeout context for P-Chain API operations.
// It intentionally uses context.Background() instead of deriving from the parent context
// to avoid context cancellation propagation during planned node restarts (e.g., during
// chain creation). The parent context may be canceled by the server's stopCh goroutine
// during restartNodes, but P-Chain API calls should continue to work after nodes restart.
func createDefaultCtx(_ context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), defaultTimeout)
}
