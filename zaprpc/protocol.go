// Copyright (C) 2021-2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

// Package zaprpc carries the netrunner control-plane RPC over luxfi/zap.
//
// One MsgType per RPC method; payloads are JSON inside a ZAP envelope:
//
//	envelope := zap.NewBuilder(...)
//	root    := envelope.StartObject(envelopeSize)
//	root.SetBytes(FieldPayload, payloadJSON) // request or response body
//	root.SetText(FieldError, errMsg)         // empty on success
//	root.FinishAsRoot()
//	wire := envelope.FinishWithFlags(MsgType << 8)
//
// JSON is the body format so any rpcpb.* struct round-trips via its existing
// `json:` tags — no extra schema to maintain and no `google.golang.org/protobuf`
// on the wire (per the project rule: ZAP internal, ZIP edge).
package zaprpc

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/luxfi/zap"
)

// MsgType identifies a netrunner RPC method on the ZAP wire.
//
// Stable wire IDs: never renumber, append-only. Bumping changes the wire
// protocol and forces clients to upgrade in lockstep with the server.
//
// Constraint: ZAP carries the method tag in the upper 8 bits of its 16-bit
// flags header, so MsgType values must fit in uint8 (0..255). 1..26 are
// the current netrunner control-plane methods; 27..255 are free for future
// additions; 0 is reserved (unused).
type MsgType uint16

const (
	MsgPing                       MsgType = 1
	MsgRPCVersion                 MsgType = 2
	MsgStart                      MsgType = 3
	MsgCreateBlockchains          MsgType = 4
	MsgTransformElasticChains     MsgType = 5
	MsgAddPermissionlessValidator MsgType = 6
	MsgRemoveChainValidator       MsgType = 7
	MsgCreateChains               MsgType = 8
	MsgHealth                     MsgType = 9
	MsgURIs                       MsgType = 10
	MsgWaitForHealthy             MsgType = 11
	MsgStatus                     MsgType = 12
	MsgRemoveNode                 MsgType = 13
	MsgAddNode                    MsgType = 14
	MsgRestartNode                MsgType = 15
	MsgPauseNode                  MsgType = 16
	MsgResumeNode                 MsgType = 17
	MsgStop                       MsgType = 18
	MsgAttachPeer                 MsgType = 19
	MsgSendOutboundMessage        MsgType = 20
	MsgSaveSnapshot               MsgType = 21
	MsgSaveHotSnapshot            MsgType = 22
	MsgLoadSnapshot               MsgType = 23
	MsgRemoveSnapshot             MsgType = 24
	MsgGetSnapshotNames           MsgType = 25
	MsgStreamStatus               MsgType = 26 // server→client status push, one msg per tick
)

// Envelope field layout — keep the size 16 (multiple of 8 for alignment).
const (
	envelopeSize       = 16
	FieldPayload   int = 0 // bytes — JSON-encoded request or response body
	FieldError     int = 8 // text  — non-empty on RPC-level error
)

// Encode wraps a JSON-marshalable value in a ZAP envelope tagged with msgType.
// Empty errStr means "success".
func Encode(msgType MsgType, body any, errStr string) ([]byte, error) {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("zaprpc encode: %w", err)
		}
	}

	cap := envelopeSize + 64 + len(payload) + len(errStr)
	b := zap.NewBuilder(cap)
	root := b.StartObject(envelopeSize)
	root.SetBytes(FieldPayload, payload)
	root.SetText(FieldError, errStr)
	root.FinishAsRoot()
	return b.FinishWithFlags(uint16(msgType) << 8), nil
}

// Decode reads a ZAP envelope. dest may be nil for void responses.
// Returns the RPC-level error from the envelope (non-nil if errStr was set).
func Decode(msg *zap.Message, dest any) error {
	root := msg.Root()
	if errStr := root.Text(FieldError); errStr != "" {
		return errors.New(errStr)
	}
	payload := root.Bytes(FieldPayload)
	if dest == nil || len(payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, dest); err != nil {
		return fmt.Errorf("zaprpc decode: %w", err)
	}
	return nil
}

// MsgTypeOf extracts the MsgType from a ZAP message's flags.
func MsgTypeOf(msg *zap.Message) MsgType {
	return MsgType(msg.Flags() >> 8)
}

// String renders the canonical name for telemetry/logging.
func (m MsgType) String() string {
	switch m {
	case MsgPing:
		return "Ping"
	case MsgRPCVersion:
		return "RPCVersion"
	case MsgStart:
		return "Start"
	case MsgCreateBlockchains:
		return "CreateBlockchains"
	case MsgTransformElasticChains:
		return "TransformElasticChains"
	case MsgAddPermissionlessValidator:
		return "AddPermissionlessValidator"
	case MsgRemoveChainValidator:
		return "RemoveChainValidator"
	case MsgCreateChains:
		return "CreateChains"
	case MsgHealth:
		return "Health"
	case MsgURIs:
		return "URIs"
	case MsgWaitForHealthy:
		return "WaitForHealthy"
	case MsgStatus:
		return "Status"
	case MsgRemoveNode:
		return "RemoveNode"
	case MsgAddNode:
		return "AddNode"
	case MsgRestartNode:
		return "RestartNode"
	case MsgPauseNode:
		return "PauseNode"
	case MsgResumeNode:
		return "ResumeNode"
	case MsgStop:
		return "Stop"
	case MsgAttachPeer:
		return "AttachPeer"
	case MsgSendOutboundMessage:
		return "SendOutboundMessage"
	case MsgSaveSnapshot:
		return "SaveSnapshot"
	case MsgSaveHotSnapshot:
		return "SaveHotSnapshot"
	case MsgLoadSnapshot:
		return "LoadSnapshot"
	case MsgRemoveSnapshot:
		return "RemoveSnapshot"
	case MsgGetSnapshotNames:
		return "GetSnapshotNames"
	case MsgStreamStatus:
		return "StreamStatus"
	default:
		return fmt.Sprintf("Msg(%d)", uint16(m))
	}
}
