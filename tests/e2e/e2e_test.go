//go:build e2e
// +build e2e

// Copyright (C) 2021-2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

// e2e implements the e2e tests.
package e2e_test

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"maps"
	"slices"

	luxd_ "github.com/luxfi/constants"
	"github.com/luxfi/p2p/message"
	"github.com/luxfi/sdk/admin"

	"github.com/luxfi/sdk/platformvm"

	"github.com/luxfi/ids"
	log "github.com/luxfi/log"
	"github.com/luxfi/metric"
	"github.com/luxfi/netrunner/client"
	"github.com/luxfi/netrunner/rpcpb"
	"github.com/luxfi/netrunner/server"
	"github.com/luxfi/netrunner/utils"
	"github.com/luxfi/netrunner/utils/constants"
	"github.com/luxfi/netrunner/ux"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

func TestE2e(t *testing.T) {
	if os.Getenv("RUN_E2E") == "" {
		t.Skip("Environment variable RUN_E2E not set; skipping E2E tests")
	}
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "network-runner-example e2e test suites")
}

const clientRootDirPrefix = "client"

var (
	logLevel        string
	logDir          string
	gRPCEp          string
	gRPCGatewayEp   string
	execPath1       string
	execPath2       string
	evmPath         string
	genesisPath     string
	genesisContents string

	newNodeName       = "test-add-node"
	newNodeName2      = "test-add-node2"
	newNode2NodeID    = ""
	pausedNodeURI     = ""
	pausedNodeName    = "node1"
	createdNetworkID  = ""
	elasticAssetID    = ""
	newNetworkID      = ""
	customNodeConfigs = map[string]string{
		"node1": `{"api-admin-enabled":true}`,
		"node2": `{"api-admin-enabled":true}`,
		"node3": `{"api-admin-enabled":true}`,
		"node4": `{"api-admin-enabled":false}`,
		"node5": `{"api-admin-enabled":false}`,
		"node6": `{"api-admin-enabled":false}`,
		"node7": `{"api-admin-enabled":false}`,
	}
	numNodes                       = uint32(3)
	networkParticipants            = []string{"node1", "node2", "node3"}
	newParticipantNode             = "new_participant_node"
	networkParticipants2           = []string{"node1", "node2", newParticipantNode}
	existingNodes                  = []string{"node1", "node2", "node3"}
	disjointNewNetworkParticipants = [][]string{
		{"new_node1", "new_node2"},
		{"new_node3", "new_node4"},
	}
	testElasticChainConfig = rpcpb.ElasticChainSpec{
		ChainId:                  "",
		AssetName:                "BLIZZARD",
		AssetSymbol:              "BRRR",
		InitialSupply:            240000000,
		MaxSupply:                720000000,
		MinConsumptionRate:       100000,
		MaxConsumptionRate:       120000,
		MinValidatorStake:        2000,
		MaxValidatorStake:        3000000,
		MinStakeDuration:         14 * 24,
		MaxStakeDuration:         365 * 24,
		MinDelegationFee:         20000,
		MinDelegatorStake:        25,
		MaxValidatorWeightFactor: 5,
		UptimeRequirement:        0.8 * 1_000_000,
	}

	testValidatorConfig = rpcpb.PermissionlessValidatorSpec{
		StakedTokenAmount: 2000,
		StartTime:         time.Now().Add(1 * time.Hour).UTC().Format(server.TimeParseLayout),
		StakeDuration:     336,
	}
)

func init() {
	flag.StringVar(
		&logLevel,
		"log-level",
		log.Info.String(),
		"log level",
	)
	flag.StringVar(
		&logDir,
		"log-dir",
		"",
		"log directory",
	)
	flag.StringVar(
		&gRPCEp,
		"grpc-endpoint",
		"0.0.0.0:8080",
		"gRPC server endpoint",
	)
	flag.StringVar(
		&gRPCGatewayEp,
		"grpc-gateway-endpoint",
		"0.0.0.0:8081",
		"gRPC gateway endpoint",
	)
	flag.StringVar(
		&execPath1,
		"node-path-1",
		"",
		"node executable path (to upgrade from)",
	)
	flag.StringVar(
		&execPath2,
		"node-path-2",
		"",
		"node executable path (to upgrade to)",
	)
	flag.StringVar(
		&evmPath,
		"evm-path",
		"",
		"path to evm binary",
	)
}

var (
	cli    client.Client
	logger log.Logger
)

var _ = ginkgo.BeforeSuite(func() {
	var err error
	if logDir == "" {
		anrRootDir := filepath.Join(os.TempDir(), constants.RootDirPrefix)
		err = os.MkdirAll(anrRootDir, os.ModePerm)
		gomega.Ω(err).Should(gomega.BeNil())
		clientRootDir := filepath.Join(anrRootDir, clientRootDirPrefix)
		logDir, err = utils.MkDirWithTimestamp(clientRootDir)
		gomega.Ω(err).Should(gomega.BeNil())
	}
	lvl, err := log.ToLevel(logLevel)
	gomega.Ω(err).Should(gomega.BeNil())
	logFactory := log.NewFactory(log.Config{
		RotatingWriterConfig: log.RotatingWriterConfig{
			Directory: logDir,
		},
		DisplayLevel: lvl,
		LogLevel:     lvl,
	})
	logger, err = logFactory.Make(constants.LogNameTest)
	gomega.Ω(err).Should(gomega.BeNil())

	cli, err = client.New(client.Config{
		Endpoint:    gRPCEp,
		DialTimeout: 10 * time.Second,
	}, logger)
	gomega.Ω(err).Should(gomega.BeNil())

	genesisPath = "tests/e2e/evm-genesis.json"
	genesisByteContents, err := os.ReadFile(genesisPath)
	gomega.Ω(err).Should(gomega.BeNil())
	genesisContents = string(genesisByteContents)
})

var _ = ginkgo.AfterSuite(func() {
	ux.Print(logger, log.Red.Wrap("shutting down cluster"))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	_, err := cli.Stop(ctx)
	cancel()
	gomega.Ω(err).Should(gomega.BeNil())

	ux.Print(logger, log.Red.Wrap("shutting down client"))
	err = cli.Close()
	gomega.Ω(err).Should(gomega.BeNil())
})

var _ = ginkgo.Describe("[Start/Remove/Restart/Add/Stop]", func() {
	ginkgo.It("can create blockhains", func() {
		existingChainID := ""
		createdBlockchainID := ""
		createdBlockchainID2 := ""
		ginkgo.By("start with blockchain specs", func() {
			ux.Print(logger, log.Green.Wrap("sending 'start' with the valid binary path: %s"), execPath1)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			resp, err := cli.Start(ctx, execPath1,
				client.WithPluginDir(filepath.Join(filepath.Dir(execPath1), "plugins")),
				client.WithChainSpecs([]*rpcpb.BlockchainSpec{
					{
						VmName:  "evm",
						Genesis: genesisPath, // test genesis path usage
					},
				}),
			)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(len(resp.ChainIds)).Should(gomega.Equal(1))
			createdBlockchainID = resp.ChainIds[0]
			ux.Print(logger, log.Green.Wrap("successfully started, node-names: %s"), resp.ClusterInfo.NodeNames)
		})

		ginkgo.By("can create a blockchain with a new network id", func() {
			ux.Print(logger, log.Blue.Wrap("can create a blockchain in a new network"))
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			resp, err := cli.CreateChains(ctx,
				[]*rpcpb.BlockchainSpec{
					{
						VmName:  "evm",
						Genesis: genesisPath, // test genesis path usage
					},
				},
			)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(len(resp.ChainIds)).Should(gomega.Equal(1))
		})

		ginkgo.By("get network ID", func() {
			cctx, ccancel := context.WithTimeout(context.Background(), 15*time.Second)
			status, err := cli.Status(cctx)
			ccancel()
			gomega.Ω(err).Should(gomega.BeNil())
			customChains := status.ClusterInfo.GetCustomChains()
			_, ok := customChains[createdBlockchainID]
			gomega.Ω(ok).Should(gomega.Equal(true))
			existingNetworkID = customChains[createdBlockchainID].PchainId
			gomega.Ω(existingNetworkID).Should(gomega.Not(gomega.BeNil()))
		})

		ginkgo.By("verify the network also has all existing nodes as participants", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			status, err := cli.Status(ctx)
			gomega.Ω(err).Should(gomega.BeNil())
			networkIDs := slices.Collect(maps.Keys(status.ClusterInfo.Chains))
			sort.Strings(networkIDs)
			createdNetworkIDString := networkIDs[0]
			networkHasCorrectParticipants := utils.VerifyNetworkHasCorrectParticipants(logger, existingNodes, status.ClusterInfo, createdNetworkIDString)
			gomega.Ω(networkHasCorrectParticipants).Should(gomega.Equal(true))
		})

		ginkgo.By("can create a blockchain with an existing network id", func() {
			ux.Print(logger, log.Blue.Wrap("can create a blockchain in an existing network"))
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			resp, err := cli.CreateChains(ctx,
				[]*rpcpb.BlockchainSpec{
					{
						VmName:  "evm",
						Genesis: genesisContents,
						ChainId: &existingNetworkID,
					},
				},
			)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(len(resp.ChainIds)).Should(gomega.Equal(1))
		})

		ginkgo.By("can save snapshot", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			_, err := cli.SaveSnapshot(ctx, "test")
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
		})

		ginkgo.By("can load snapshot", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			_, err := cli.LoadSnapshot(ctx, "test")
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
		})

		// need to remove the snapshot otherwise it fails later in the 2nd part of snapshot tests
		// (testing for no snapshots)
		ginkgo.By("can remove snapshot", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			_, err := cli.RemoveSnapshot(ctx, "test")
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
		})

		ginkgo.By("can create a blockchain with an existing network id loaded from snapshot", func() {
			ux.Print(logger, log.Blue.Wrap("can create a blockchain in an existing network"))
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			_, err := cli.CreateChains(ctx,
				[]*rpcpb.BlockchainSpec{
					{
						VmName:  "evm",
						Genesis: genesisContents,
						ChainId: &existingNetworkID,
					},
				},
			)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
		})

		ginkgo.By("can create a blockchain with new network id with some of existing participating nodes", func() {
			ux.Print(logger, log.Blue.Wrap("can create a blockchain with new network id with some of existing participating nodes"))
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			resp, err := cli.CreateChains(ctx,
				[]*rpcpb.BlockchainSpec{
					{
						VmName:           "evm",
						Genesis:          genesisContents,
						ParticipantsSpec: &rpcpb.ParticipantsSpec{Participants: networkParticipants},
					},
				},
			)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(len(resp.ChainIds)).Should(gomega.Equal(1))
			createdBlockchainID = resp.ChainIds[0]
		})

		ginkgo.By("verify network has correct participants", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			status, err := cli.Status(ctx)
			gomega.Ω(err).Should(gomega.BeNil())
			customChains := status.ClusterInfo.GetCustomChains()
			createdNetworkIDString := customChains[createdBlockchainID].PchainId
			networkHasCorrectParticipants := utils.VerifyNetworkHasCorrectParticipants(logger, networkParticipants, status.ClusterInfo, createdNetworkIDString)
			gomega.Ω(networkHasCorrectParticipants).Should(gomega.Equal(true))
			// verify that no new nodes is added to cluster
			gomega.Ω(len(status.ClusterInfo.NodeNames)).Should(gomega.Equal(5))
		})

		ginkgo.By("can create a blockchain with new network id with some of existing participating nodes and a new node", func() {
			ux.Print(logger, log.Blue.Wrap("can create a blockchain with new network id with some of existing participating nodes and a new node"))
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			resp, err := cli.CreateChains(ctx,
				[]*rpcpb.BlockchainSpec{
					{
						VmName:           "evm",
						Genesis:          genesisContents,
						ParticipantsSpec: &rpcpb.ParticipantsSpec{Participants: networkParticipants2},
					},
				},
			)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			// verify that a new node is added to cluster
			gomega.Ω(len(resp.ClusterInfo.NodeNames)).Should(gomega.Equal(6))
			_, ok := resp.ClusterInfo.NodeInfos[newParticipantNode]
			gomega.Ω(ok).Should(gomega.Equal(true))
			gomega.Ω(len(resp.ChainIds)).Should(gomega.Equal(1))
			createdBlockchainID = resp.ChainIds[0]
		})

		ginkgo.By("verify the newer network also has correct participants", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			status, err := cli.Status(ctx)
			gomega.Ω(err).Should(gomega.BeNil())
			customChains := status.ClusterInfo.GetCustomChains()
			createdNetworkIDString := customChains[createdBlockchainID].PchainId
			networkHasCorrectParticipants := utils.VerifyNetworkHasCorrectParticipants(log, networkParticipants2, status.ClusterInfo, createdNetworkIDString)
			gomega.Ω(networkHasCorrectParticipants).Should(gomega.Equal(true))
		})

		ginkgo.By("can create two blockchains at a time", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			resp, err := cli.CreateChains(ctx,
				[]*rpcpb.BlockchainSpec{
					{
						VmName:  "evm",
						Genesis: genesisContents,
						ChainId: &existingNetworkID,
					},
					{
						VmName:  "evm",
						Genesis: genesisContents,
						ChainId: &existingNetworkID,
					},
				},
			)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(len(resp.ChainIds)).Should(gomega.Equal(2))
		})

		ginkgo.By("can create two blockchains in two new disjoint networks with bls validators", func() {
			// get prev status
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			prevStatus, err := cli.Status(ctx)
			gomega.Ω(err).Should(gomega.BeNil())
			cancel()
			// create blockchains
			ctx, cancel = context.WithTimeout(context.Background(), 3*time.Minute)
			resp, err := cli.CreateChains(ctx,
				[]*rpcpb.BlockchainSpec{
					{
						VmName:           "evm",
						Genesis:          genesisContents,
						ParticipantsSpec: &rpcpb.ParticipantsSpec{Participants: disjointNewNetworkParticipants[0]},
					},
					{
						VmName:           "evm",
						Genesis:          genesisContents,
						ParticipantsSpec: &rpcpb.ParticipantsSpec{Participants: disjointNewNetworkParticipants[1]},
					},
				},
			)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			// check new nodes
			allNewParticipants := append(disjointNewNetworkParticipants[0], disjointNewNetworkParticipants[1]...)
			expectedLen := len(prevStatus.ClusterInfo.NodeNames) + len(allNewParticipants)
			gomega.Ω(len(resp.ClusterInfo.NodeNames)).Should(gomega.Equal(expectedLen))
			for _, nodeName := range allNewParticipants {
				_, ok := resp.ClusterInfo.NodeInfos[nodeName]
				gomega.Ω(ok).Should(gomega.Equal(true))
			}
			gomega.Ω(len(resp.ChainIds)).Should(gomega.Equal(2))
			createdBlockchainID = resp.ChainIds[0]
			createdBlockchainID2 = resp.ChainIds[1]
		})

		ginkgo.By("verify the new networks also has correct participants", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			status, err := cli.Status(ctx)
			gomega.Ω(err).Should(gomega.BeNil())
			customChains := status.ClusterInfo.GetCustomChains()
			createdNetworkIDString := customChains[createdBlockchainID].PchainId
			networkHasCorrectParticipants := utils.VerifyNetworkHasCorrectParticipants(log, disjointNewNetworkParticipants[0], status.ClusterInfo, createdNetworkIDString)
			gomega.Ω(networkHasCorrectParticipants).Should(gomega.Equal(true))
			createdNetworkID2String := customChains[createdBlockchainID2].PchainId
			network2HasCorrectParticipants := utils.VerifyNetworkHasCorrectParticipants(log, disjointNewNetworkParticipants[1], status.ClusterInfo, createdNetworkID2String)
			gomega.Ω(network2HasCorrectParticipants).Should(gomega.Equal(true))
		})

		ginkgo.By("verify that new validators have BLS Keys", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			clientURIs, err := cli.URIs(ctx)
			gomega.Ω(err).Should(gomega.BeNil())
			var clientURI string
			for _, uri := range clientURIs {
				clientURI = uri
				break
			}
			platformCli := platformvm.NewClient(clientURI)
			vdrs, err := platformCli.GetCurrentValidators(ctx, ids.Empty, nil)
			gomega.Ω(err).Should(gomega.BeNil())
			status, err := cli.Status(ctx)
			gomega.Ω(err).Should(gomega.BeNil())
			allNewParticipants := append(disjointNewNetworkParticipants[0], disjointNewNetworkParticipants[1]...)
			for _, nodeName := range allNewParticipants {
				nodeInfo, ok := status.ClusterInfo.NodeInfos[nodeName]
				gomega.Ω(ok).Should(gomega.Equal(true))
				found := false
				for _, v := range vdrs {
					if v.NodeID.String() == nodeInfo.Id {
						gomega.Ω(v.Signer).Should(gomega.Not(gomega.BeNil()))
						found = true
					}
				}
				gomega.Ω(found).Should(gomega.Equal(true))
			}
			cancel()
		})

		ginkgo.By("stop the network", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_, err := cli.Stop(ctx)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
		})
	})

	ginkgo.It("can start", func() {
		ginkgo.By("start request with invalid exec path should fail", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			_, err := cli.Start(ctx, "")
			cancel()
			gomega.Ω(err.Error()).Should(gomega.ContainSubstring(utils.ErrInvalidExecPath.Error()))
		})

		ginkgo.By("start request with invalid exec path should fail", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			_, err := cli.Start(ctx, "invalid")
			cancel()
			gomega.Ω(err.Error()).Should(gomega.ContainSubstring(utils.ErrNotExists.Error()))
		})

		ginkgo.By("start request with invalid custom VM path should fail", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			_, err := cli.Start(ctx, execPath1,
				client.WithPluginDir(os.TempDir()),
				client.WithChainSpecs([]*rpcpb.BlockchainSpec{
					{
						VmName: "invalid",
					},
				}),
			)
			cancel()
			gomega.Ω(err.Error()).Should(gomega.ContainSubstring(utils.ErrNotExistsPlugin.Error()))
		})

		ginkgo.By("start request with invalid custom VM name format should fail", func() {
			f, err := os.CreateTemp(os.TempDir(), strings.Repeat("a", 33))
			gomega.Ω(err).Should(gomega.BeNil())
			filePath := f.Name()
			gomega.Ω(f.Close()).Should(gomega.BeNil())

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			_, err = cli.Start(ctx, execPath1,
				client.WithPluginDir(filepath.Dir(filePath)),
				client.WithChainSpecs([]*rpcpb.BlockchainSpec{
					{
						VmName: filepath.Base(filePath),
					},
				}),
			)
			cancel()
			gomega.Ω(err.Error()).Should(gomega.ContainSubstring(server.ErrInvalidVMName.Error()))

			os.RemoveAll(filePath)
		})

		ginkgo.By("calling start API with the valid binary path", func() {
			ux.Print(logger, log.Green.Wrap("sending 'start' with the valid binary path: %s"), execPath1)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			resp, err := cli.Start(ctx, execPath1)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			ux.Print(logger, log.Green.Wrap("successfully started, node-names: %s"), resp.ClusterInfo.NodeNames)
		})
	})

	ginkgo.It("can wait for health", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		_, err := cli.Health(ctx)
		cancel()
		gomega.Ω(err).Should(gomega.BeNil())
	})

	ginkgo.It("can get URIs", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		uris, err := cli.URIs(ctx)
		cancel()
		gomega.Ω(err).Should(gomega.BeNil())
		ux.Print(logger, log.Blue.Wrap("URIs: %s"), uris)
	})

	ginkgo.It("can fetch status", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		resp, err := cli.Status(ctx)
		cancel()
		gomega.Ω(err).Should(gomega.BeNil())
		// get paused node URI for pause node test later
		_, ok := resp.ClusterInfo.NodeInfos[pausedNodeName]
		gomega.Ω(ok).Should(gomega.Equal(true))
		pausedNodeURI = resp.ClusterInfo.NodeInfos[pausedNodeName].Uri
	})

	ginkgo.It("can poll status", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		ch, err := cli.StreamStatus(ctx, 5*time.Second)
		gomega.Ω(err).Should(gomega.BeNil())
		for info := range ch {
			ux.Print(logger, log.Green.Wrap("fetched info, node-names: %s"), info.NodeNames)
			if info.Healthy {
				break
			}
		}
	})

	ginkgo.It("can remove", func() {
		ginkgo.By("calling remove API with the first binary", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			resp, err := cli.RemoveNode(ctx, "node5")
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			ux.Print(logger, log.Green.Wrap("successfully removed, node-names: %s"), resp.ClusterInfo.NodeNames)
		})
	})

	ginkgo.It("can restart", func() {
		ginkgo.By("calling restart API with the second binary", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			resp, err := cli.RestartNode(ctx, "node4", client.WithExecPath(execPath2))
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			ux.Print(logger, log.Green.Wrap("successfully restarted, node-names: %s"), resp.ClusterInfo.NodeNames)
		})
	})

	ginkgo.It("can attach a peer", func() {
		ginkgo.By("calling attach peer API", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			resp, err := cli.AttachPeer(ctx, "node1")
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())

			v, ok := resp.ClusterInfo.AttachedPeerInfos["node1"]
			gomega.Ω(ok).Should(gomega.BeTrue())
			ux.Print(logger, log.Green.Wrap("successfully attached peer, peers: %+v"), v.Peers)

			mc, err := message.NewCreator(
				log.NoLog{},
				metric.NewRegistry(),
				"",
				luxd_constants.DefaultNetworkCompressionType,
				10*time.Second,
			)
			gomega.Ω(err).Should(gomega.BeNil())

			containerIDs := []ids.ID{
				ids.GenerateTestID(),
				ids.GenerateTestID(),
				ids.GenerateTestID(),
			}
			requestID := uint32(42)
			chainID := luxd_constants.PlatformChainID
			msg, err := mc.Chits(chainID, requestID, []ids.ID{}, containerIDs)
			gomega.Ω(err).Should(gomega.BeNil())

			ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
			sresp, err := cli.SendOutboundMessage(ctx, "node1", v.Peers[0].Id, uint32(msg.Op()), msg.Bytes())
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(sresp.Sent).Should(gomega.BeTrue())
		})
	})

	ginkgo.It("can add a node", func() {
		ginkgo.By("calling AddNode", func() {
			ux.Print(logger, log.Green.Wrap("calling 'add-node' with the valid binary path: %s"), execPath1)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			resp, err := cli.AddNode(ctx, newNodeName, execPath1)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			ux.Print(logger, log.Green.Wrap("successfully started, node-names: %s"), resp.ClusterInfo.NodeNames)
		})

		ginkgo.By("calling AddNode with existing node name, should fail", func() {
			ux.Print(logger, log.Green.Wrap("calling 'add-node' with the valid binary path: %s"), execPath1)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			resp, err := cli.AddNode(ctx, newNodeName, execPath1)
			cancel()
			gomega.Ω(err.Error()).Should(gomega.ContainSubstring("repeated node name"))
			gomega.Ω(resp).Should(gomega.BeNil())
			ux.Print(logger, log.Green.Wrap("'add-node' failed as expected"))
		})
	})

	ginkgo.It("can start with custom config", func() {
		ginkgo.By("stopping network first", func() {
			ux.Print(logger, log.Red.Wrap("shutting down cluster"))
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			_, err := cli.Stop(ctx)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())

			ux.Print(logger, log.Red.Wrap("shutting down client"))
			gomega.Ω(err).Should(gomega.BeNil())
		})
		ginkgo.By("calling start API with custom config", func() {
			ux.Print(logger, log.Green.Wrap("sending 'start' with the valid binary path: %s"), execPath1)
			opts := []client.OpOption{
				client.WithNumNodes(numNodes),
				client.WithCustomNodeConfigs(customNodeConfigs),
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			resp, err := cli.Start(ctx, execPath1, opts...)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			ux.Print(logger, log.Green.Wrap("successfully started, node-names: %s"), resp.ClusterInfo.NodeNames)
		})
		ginkgo.By("can wait for health", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			_, err := cli.Health(ctx)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
		})
		ginkgo.By("overrides num-nodes", func() {
			ux.Print(logger, log.Green.Wrap("checking that given num-nodes %d have been overridden by custom configs: %d"), numNodes, len(customNodeConfigs))
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			uris, err := cli.URIs(ctx)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(uris).Should(gomega.HaveLen(len(customNodeConfigs)))
			ux.Print(logger, log.Green.Wrap("expected number of nodes up: %d"), len(customNodeConfigs))

			ux.Print(logger, log.Green.Wrap("checking correct admin APIs are enabled resp. disabled"))
			// we have 7 nodes, 3 have the admin API enabled, the other 4 disabled
			// therefore we expect exactly 4 calls to fail and exactly 3 to succeed.
			ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
			errCnt := 0
			for i := 0; i < len(uris); i++ {
				cli := admin.NewClient(uris[i])
				err := cli.LockProfile(ctx)
				if err != nil {
					errCnt++
				}
			}
			cancel()
			gomega.Ω(errCnt).Should(gomega.Equal(4))
		})
	})

	ginkgo.It("start with default network, for subsequent steps", func() {
		ginkgo.By("stopping network first", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			_, err := cli.Stop(ctx)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
		})
		ginkgo.By("starting", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			_, err := cli.Start(ctx, execPath1)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
		})
		ginkgo.By("wait for health", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			_, err := cli.Health(ctx)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
		})
	})
	ginkgo.It("can pause a node", func() {
		ginkgo.By("calling PauseNode", func() {
			ux.Print(logger, log.Green.Wrap("calling 'pause-node'"))
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			resp, err := cli.PauseNode(ctx, pausedNodeName)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			ux.Print(logger, log.Green.Wrap("successfully paused, node-names: %s"), resp.ClusterInfo.NodeNames)
		})
		ginkgo.By("can wait for health", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			_, err := cli.Health(ctx)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
		})
		ginkgo.By("API Call using paused node URI will fail", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			platformCli := platformvm.NewClient(pausedNodeURI)
			_, err := platformCli.GetCurrentValidators(ctx, ids.Empty, nil)
			gomega.Ω(err).Should(gomega.HaveOccurred())
		})
		ginkgo.By("calling resumeNode API", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			resp, err := cli.ResumeNode(ctx, pausedNodeName)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			ux.Print(logger, log.Green.Wrap("successfully resumed %s, cluster node-names: %s"), "node1", resp.ClusterInfo.NodeNames)
		})
		ginkgo.By("can wait for health", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			_, err := cli.Health(ctx)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
		})
		ginkgo.By("API Call using resumed node URI", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			platformCli := platformvm.NewClient(pausedNodeURI)
			_, err := platformCli.GetCurrentValidators(ctx, ids.Empty, nil)
			gomega.Ω(err).Should(gomega.BeNil())
		})
	})

	ginkgo.It("can add primary validator with BLS Keys", func() {
		ginkgo.By("calling AddNode", func() {
			ux.Print(logger, log.Green.Wrap("calling 'add-node' with the valid binary path: %s"), execPath1)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			resp, err := cli.AddNode(ctx, newNodeName2, execPath1)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			for nodeName, nodeInfo := range resp.ClusterInfo.NodeInfos {
				if nodeName == newNodeName2 {
					newNode2NodeID = nodeInfo.Id
				}
			}
			ux.Print(logger, log.Green.Wrap("successfully started, node-names: %s"), resp.ClusterInfo.NodeNames)
		})
		ginkgo.By("can wait for health", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			_, err := cli.Health(ctx)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
		})
		ginkgo.By("add 1 network", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			resp, err := cli.CreateParticipantGroups(ctx, []*rpcpb.ParticipantsSpec{{}})
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(len(resp.ChainIds)).Should(gomega.Equal(1))
		})
		ginkgo.By("verify that new validator has BLS Keys", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			clientURIs, err := cli.URIs(ctx)
			gomega.Ω(err).Should(gomega.BeNil())
			var clientURI string
			for _, uri := range clientURIs {
				clientURI = uri
				break
			}
			platformCli := platformvm.NewClient(clientURI)
			vdrs, err := platformCli.GetCurrentValidators(ctx, ids.Empty, nil)

			gomega.Ω(err).Should(gomega.BeNil())
			for _, v := range vdrs {
				if v.NodeID.String() == newNode2NodeID {
					gomega.Ω(v.Signer).Should(gomega.Not(gomega.BeNil()))
				}
			}
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
		})
	})

	ginkgo.It("network creation", func() {
		ginkgo.By("add 1 network", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			_, err := cli.CreateParticipantGroups(ctx, []*rpcpb.ParticipantsSpec{{}})
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
		})
		ginkgo.By("check network number is 2", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			status, err := cli.Status(ctx)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			numNetworks := len(status.ClusterInfo.Chains)
			gomega.Ω(numNetworks).Should(gomega.Equal(2))
		})
		ginkgo.By("add 1 network with participants", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			response, err := cli.CreateParticipantGroups(ctx, []*rpcpb.ParticipantsSpec{{Participants: networkParticipants}})
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(len(response.ChainIds)).Should(gomega.Equal(1))
			newNetworkID = response.ChainIds[0]
		})
		ginkgo.By("verify network has correct participants", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			status, err := cli.Status(ctx)
			gomega.Ω(err).Should(gomega.BeNil())
			networkHasCorrectParticipants := utils.VerifyNetworkHasCorrectParticipants(log, networkParticipants, status.ClusterInfo, newNetworkID)
			gomega.Ω(networkHasCorrectParticipants).Should(gomega.Equal(true))
		})
		ginkgo.By("add 1 network with node not currently added", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			response, err := cli.CreateParticipantGroups(ctx, []*rpcpb.ParticipantsSpec{{Participants: networkParticipants2}})
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			// verify that a new node is added to cluster
			gomega.Ω(len(response.ClusterInfo.NodeNames)).Should(gomega.Equal(7))
			_, ok := response.ClusterInfo.NodeInfos[newParticipantNode]
			gomega.Ω(ok).Should(gomega.Equal(true))
			gomega.Ω(len(response.ChainIds)).Should(gomega.Equal(1))
			newNetworkID = response.ChainIds[0]
		})
		ginkgo.By("calling AddNode with existing node name, should fail", func() {
			ux.Print(logger, log.Green.Wrap("calling 'add-node' with the valid binary path: %s"), execPath1)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			resp, err := cli.AddNode(ctx, newParticipantNode, execPath1)
			cancel()
			gomega.Ω(err.Error()).Should(gomega.ContainSubstring("repeated node name"))
			gomega.Ω(resp).Should(gomega.BeNil())
			ux.Print(logger, log.Green.Wrap("'add-node' failed as expected"))
		})
		ginkgo.By("verify the newer network also has correct participants", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			status, err := cli.Status(ctx)
			gomega.Ω(err).Should(gomega.BeNil())
			networkHasCorrectParticipants := utils.VerifyNetworkHasCorrectParticipants(logger, networkParticipants2, status.ClusterInfo, newNetworkID)
			gomega.Ω(networkHasCorrectParticipants).Should(gomega.Equal(true))
		})
	})

	ginkgo.It("can remove network validator", func() {
		ginkgo.By("removing a network validator", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			testRemoveNetworkValidatorConfig := rpcpb.RemoveChainValidatorSpec{
				NodeNames: []string{"node2"},
				ChainId:   newNetworkID,
			}
			_, err := cli.RemoveChainValidator(ctx, []*rpcpb.RemoveChainValidatorSpec{&testRemoveNetworkValidatorConfig})
			gomega.Ω(err).Should(gomega.BeNil())
		})
		ginkgo.By("verify there are only two validators left for network", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			clientURIs, err := cli.URIs(ctx)
			gomega.Ω(err).Should(gomega.BeNil())
			var clientURI string
			for _, uri := range clientURIs {
				clientURI = uri
				break
			}
			networkID, err := ids.FromString(newNetworkID)
			gomega.Ω(err).Should(gomega.BeNil())
			platformCli := platformvm.NewClient(clientURI)
			vdrs, err := platformCli.GetCurrentValidators(ctx, networkID, nil)
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(len(vdrs)).Should(gomega.Equal(2))
		})
	})

	ginkgo.It("transform network to elastic networks", func() {
		var elasticNetworkID string
		ginkgo.By("add 1 network", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			resp, err := cli.CreateParticipantGroups(ctx, []*rpcpb.ParticipantsSpec{{}})
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(len(resp.ChainIds)).Should(gomega.Equal(1))
			gomega.Ω(len(resp.ClusterInfo.Chains)).Should(gomega.Equal(5))
			createdNetworkID = resp.ChainIds[0]
		})
		ginkgo.By("check expected non elastic status", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			status, err := cli.Status(ctx)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			networkInfo := status.ClusterInfo.Chains[createdNetworkID]
			gomega.Ω(networkInfo.IsElastic).Should(gomega.Equal(false))
			gomega.Ω(networkInfo.ElasticChainId).Should(gomega.Equal(ids.Empty.String()))
		})
		ginkgo.By("transform 1 network to elastic network", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			testElasticChainConfig.ChainId = createdNetworkID
			response, err := cli.TransformElasticChains(ctx, []*rpcpb.ElasticChainSpec{&testElasticChainConfig})
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(len(response.TxIds)).Should(gomega.Equal(1))
			gomega.Ω(len(response.AssetIds)).Should(gomega.Equal(1))
			elasticAssetID = response.AssetIds[0]
		})
		ginkgo.By("check expected elastic status", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			status, err := cli.Status(ctx)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			networkInfo := status.ClusterInfo.Chains[createdNetworkID]
			gomega.Ω(networkInfo.IsElastic).Should(gomega.Equal(true))
			gomega.Ω(networkInfo.ElasticChainId).ShouldNot(gomega.Equal(ids.Empty.String()))
			elasticNetworkID = networkInfo.ElasticChainId
		})
		ginkgo.By("transforming a network with same networkID to elastic network will fail", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			testElasticChainConfig.ChainId = createdNetworkID
			_, err := cli.TransformElasticChains(ctx, []*rpcpb.ElasticChainSpec{&testElasticChainConfig})
			gomega.Ω(err).Should(gomega.HaveOccurred())
		})
		ginkgo.By("save snapshot with elastic info", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			_, err := cli.SaveSnapshot(ctx, "elastic_snapshot")
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
		})
		ginkgo.By("load snapshot with elastic info", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			_, err := cli.LoadSnapshot(ctx, "elastic_snapshot")
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
		})
		ginkgo.By("check expected elastic status", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			status, err := cli.Status(ctx)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			networkInfo := status.ClusterInfo.Chains[createdNetworkID]
			gomega.Ω(networkInfo.IsElastic).Should(gomega.Equal(true))
			gomega.Ω(networkInfo.ElasticChainId).Should(gomega.Equal(elasticNetworkID))
		})
		ginkgo.By("remove snapshot with elastic info", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			_, err := cli.RemoveSnapshot(ctx, "elastic_snapshot")
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
		})
	})

	ginkgo.It("add permissionless validator to elastic networks", func() {
		ginkgo.By("adding a permissionless validator to elastic network", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			testValidatorConfig.ChainId = createdNetworkID
			testValidatorConfig.AssetId = elasticAssetID
			testValidatorConfig.NodeName = "permissionlessNode"
			_, err := cli.AddPermissionlessValidator(ctx, []*rpcpb.PermissionlessValidatorSpec{&testValidatorConfig})
			gomega.Ω(err).Should(gomega.BeNil())
		})
	})

	ginkgo.It("snapshots + blockchain creation", func() {
		var originalUris []string
		var originalNetworks []string
		ginkgo.By("get original URIs", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			var err error
			originalUris, err = cli.URIs(ctx)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(len(originalUris)).Should(gomega.Equal(8))
		})
		ginkgo.By("get original networks", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			status, err := cli.Status(ctx)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			numNetworks := len(status.ClusterInfo.Chains)
			gomega.Ω(numNetworks).Should(gomega.Equal(5))
			originalNetworks = slices.Collect(maps.Keys(status.ClusterInfo.Chains))
		})
		ginkgo.By("check there are no snapshots", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			snapshotNames, err := cli.GetSnapshotNames(ctx)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(snapshotNames).Should(gomega.Equal([]string(nil)))
		})
		ginkgo.By("can save snapshot", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			_, err := cli.SaveSnapshot(ctx, "pepe")
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
		})
		ginkgo.By("wait fail for stopped network", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			_, err := cli.Health(ctx)
			cancel()
			gomega.Ω(err.Error()).Should(gomega.ContainSubstring(server.ErrNotBootstrapped.Error()))
		})
		ginkgo.By("load fail for unknown snapshot", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			_, err := cli.LoadSnapshot(ctx, "papa")
			cancel()
			gomega.Ω(err.Error()).Should(gomega.ContainSubstring("snapshot not found"))
		})
		ginkgo.By("can load snapshot", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			_, err := cli.LoadSnapshot(ctx, "pepe")
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
		})
		ginkgo.By("check URIs", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			var err error
			uris, err := cli.URIs(ctx)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(uris).Should(gomega.Equal(originalUris))
		})
		ginkgo.By("check networks", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			status, err := cli.Status(ctx)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			networkIDs := slices.Collect(maps.Keys(status.ClusterInfo.Chains))
			sort.Strings(networkIDs)
			sort.Strings(originalNetworks)
			gomega.Ω(networkIDs).Should(gomega.Equal(originalNetworks))
		})
		ginkgo.By("save fail for already saved snapshot", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			_, err := cli.SaveSnapshot(ctx, "pepe")
			cancel()
			gomega.Ω(err.Error()).Should(gomega.ContainSubstring("snapshot \"pepe\" already exists"))
		})
		ginkgo.By("check there is a snapshot", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			snapshotNames, err := cli.GetSnapshotNames(ctx)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(snapshotNames).Should(gomega.Equal([]string{"pepe"}))
		})
		ginkgo.By("can remove snapshot", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			_, err := cli.RemoveSnapshot(ctx, "pepe")
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
		})
		ginkgo.By("check there are no snapshots", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			snapshotNames, err := cli.GetSnapshotNames(ctx)
			cancel()
			gomega.Ω(err).Should(gomega.BeNil())
			gomega.Ω(snapshotNames).Should(gomega.Equal([]string(nil)))
		})
		ginkgo.By("remove fail for unknown snapshot", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			_, err := cli.RemoveSnapshot(ctx, "pepe")
			cancel()
			gomega.Ω(err.Error()).Should(gomega.ContainSubstring("snapshot not found"))
		})
	})
})
