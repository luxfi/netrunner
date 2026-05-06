// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package zapwire defines the ZAP opcodes and message types for the
// netrunner control protocol. It replaces the legacy gRPC/protobuf
// netrunner/rpcpb package with native ZAP types — no protobuf, no .proto
// files, no codegen — every message is a hand-written Go struct that
// encodes/decodes via luxfi/api/zap.Buffer / luxfi/api/zap.Reader.
//
// The control protocol is a request/response RPC that runs on TCP
// between a netrunner client (luxfi/cli, automation tooling, tests) and
// a netrunner server (this repo). Streaming RPCs (StreamStatus) use
// multiple response frames keyed by the same request ID.
package zapwire

import "github.com/luxfi/api/zap"

// DefaultPort is the canonical Lux ZAP TCP port — used by netrunner
// control, KMS ZAP transport, and other in-cluster ZAP services.
// Override only when running multiple ZAP servers on the same host.
const DefaultPort = 9999

// DefaultAddr is the canonical loopback bind address for local
// development and tests. Production deployments bind to a routable
// interface, not loopback.
const DefaultAddr = "127.0.0.1:9999"

// Netrunner control opcodes occupy ZAP message-type range [60, 99].
// Per zap/wire.go layout: bits 0..5 carry the opcode (max 63), bit 6
// is MsgErrorFlag, bit 7 is MsgResponseFlag. Stay below 0x40 so the
// flags remain free.
//
// Reserved ranges (see ~/work/lux/api/zap/wire.go):
//   1..31  — VM service methods
//   40..43 — p2p.Sender methods
//   50..52 — Warp signing
//   60..63 — netrunner control (this file)
//
// Hitting the 0x40 ceiling: keep opcodes <=63. If we need more than 4
// opcodes here, use a sub-opcode byte at the start of the payload
// rather than expanding the opcode range.
const (
	// Lifecycle
	OpPing       zap.MessageType = 60
	OpRPCVersion zap.MessageType = 61
	OpStart      zap.MessageType = 62
	OpStop       zap.MessageType = 63
)

// Sub-opcode dispatcher — encoded as the first byte of payload after the
// 4-byte request ID. This lets us multiplex many RPCs onto a small
// opcode footprint while keeping the wire format simple.
//
//	[hdr][reqID:uint32][subOp:uint8][rpc-specific bytes...]
type SubOpcode uint8

const (
	// Network lifecycle (alongside OpStart/OpStop opcodes above)
	SubHealth         SubOpcode = 1
	SubWaitForHealthy SubOpcode = 2
	SubURIs           SubOpcode = 3
	SubStatus         SubOpcode = 4
	SubStreamStatus   SubOpcode = 5

	// Chain operations
	SubCreateBlockchains          SubOpcode = 10
	SubCreateChains               SubOpcode = 11
	SubTransformElasticChains     SubOpcode = 12
	SubAddPermissionlessValidator SubOpcode = 13
	SubRemoveChainValidator       SubOpcode = 14

	// Node operations
	SubAddNode     SubOpcode = 20
	SubRemoveNode  SubOpcode = 21
	SubRestartNode SubOpcode = 22
	SubPauseNode   SubOpcode = 23
	SubResumeNode  SubOpcode = 24

	// Peer / message operations
	SubAttachPeer          SubOpcode = 30
	SubSendOutboundMessage SubOpcode = 31

	// Snapshot operations
	SubSaveSnapshot     SubOpcode = 40
	SubSaveHotSnapshot  SubOpcode = 41
	SubLoadSnapshot     SubOpcode = 42
	SubRemoveSnapshot   SubOpcode = 43
	SubGetSnapshotNames SubOpcode = 44
)
