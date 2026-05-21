// Copyright (C) 2021-2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package server

import (
	"github.com/luxfi/netrunner/rpcpb"
	"github.com/luxfi/netrunner/zaprpc"
)

// bindZAP installs every netrunner control-plane method onto a zaprpc
// Dispatcher. One entry per MsgType — the canonical map of "what does this
// server expose". Stream methods are handled separately at Run() time.
func bindZAP(s *server) *zaprpc.Dispatcher {
	d := zaprpc.NewDispatcher()

	zaprpc.Bind[rpcpb.PingRequest, rpcpb.PingResponse](d, zaprpc.MsgPing, s.Ping)
	zaprpc.Bind[rpcpb.RPCVersionRequest, rpcpb.RPCVersionResponse](d, zaprpc.MsgRPCVersion, s.RPCVersion)
	zaprpc.Bind[rpcpb.StartRequest, rpcpb.StartResponse](d, zaprpc.MsgStart, s.Start)
	zaprpc.Bind[rpcpb.CreateBlockchainsRequest, rpcpb.CreateBlockchainsResponse](d, zaprpc.MsgCreateBlockchains, s.CreateBlockchains)
	zaprpc.Bind[rpcpb.TransformElasticChainsRequest, rpcpb.TransformElasticChainsResponse](d, zaprpc.MsgTransformElasticChains, s.TransformElasticChains)
	zaprpc.Bind[rpcpb.AddPermissionlessValidatorRequest, rpcpb.AddPermissionlessValidatorResponse](d, zaprpc.MsgAddPermissionlessValidator, s.AddPermissionlessValidator)
	zaprpc.Bind[rpcpb.RemoveChainValidatorRequest, rpcpb.RemoveChainValidatorResponse](d, zaprpc.MsgRemoveChainValidator, s.RemoveChainValidator)
	zaprpc.Bind[rpcpb.CreateChainsRequest, rpcpb.CreateChainsResponse](d, zaprpc.MsgCreateChains, s.CreateChains)
	zaprpc.Bind[rpcpb.HealthRequest, rpcpb.HealthResponse](d, zaprpc.MsgHealth, s.Health)
	zaprpc.Bind[rpcpb.URIsRequest, rpcpb.URIsResponse](d, zaprpc.MsgURIs, s.URIs)
	zaprpc.Bind[rpcpb.WaitForHealthyRequest, rpcpb.WaitForHealthyResponse](d, zaprpc.MsgWaitForHealthy, s.WaitForHealthy)
	zaprpc.Bind[rpcpb.StatusRequest, rpcpb.StatusResponse](d, zaprpc.MsgStatus, s.Status)
	zaprpc.Bind[rpcpb.RemoveNodeRequest, rpcpb.RemoveNodeResponse](d, zaprpc.MsgRemoveNode, s.RemoveNode)
	zaprpc.Bind[rpcpb.AddNodeRequest, rpcpb.AddNodeResponse](d, zaprpc.MsgAddNode, s.AddNode)
	zaprpc.Bind[rpcpb.RestartNodeRequest, rpcpb.RestartNodeResponse](d, zaprpc.MsgRestartNode, s.RestartNode)
	zaprpc.Bind[rpcpb.PauseNodeRequest, rpcpb.PauseNodeResponse](d, zaprpc.MsgPauseNode, s.PauseNode)
	zaprpc.Bind[rpcpb.ResumeNodeRequest, rpcpb.ResumeNodeResponse](d, zaprpc.MsgResumeNode, s.ResumeNode)
	zaprpc.Bind[rpcpb.StopRequest, rpcpb.StopResponse](d, zaprpc.MsgStop, s.Stop)
	zaprpc.Bind[rpcpb.AttachPeerRequest, rpcpb.AttachPeerResponse](d, zaprpc.MsgAttachPeer, s.AttachPeer)
	zaprpc.Bind[rpcpb.SendOutboundMessageRequest, rpcpb.SendOutboundMessageResponse](d, zaprpc.MsgSendOutboundMessage, s.SendOutboundMessage)
	zaprpc.Bind[rpcpb.SaveSnapshotRequest, rpcpb.SaveSnapshotResponse](d, zaprpc.MsgSaveSnapshot, s.SaveSnapshot)
	zaprpc.Bind[rpcpb.SaveSnapshotRequest, rpcpb.SaveSnapshotResponse](d, zaprpc.MsgSaveHotSnapshot, s.SaveHotSnapshot)
	zaprpc.Bind[rpcpb.LoadSnapshotRequest, rpcpb.LoadSnapshotResponse](d, zaprpc.MsgLoadSnapshot, s.LoadSnapshot)
	zaprpc.Bind[rpcpb.RemoveSnapshotRequest, rpcpb.RemoveSnapshotResponse](d, zaprpc.MsgRemoveSnapshot, s.RemoveSnapshot)
	zaprpc.Bind[rpcpb.GetSnapshotNamesRequest, rpcpb.GetSnapshotNamesResponse](d, zaprpc.MsgGetSnapshotNames, s.GetSnapshotNames)

	// MsgStreamStatus is delivered via server-push on Status subscription —
	// see (*server).pushStatusLoop for the ticker. Clients install an
	// inbound handler on this MsgType to receive ClusterInfo updates.

	return d
}
