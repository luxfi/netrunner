// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zapwire

import (
	"context"
	"errors"
	"fmt"

	"github.com/luxfi/api/zap"
	"github.com/luxfi/netrunner/zapwire/types"
)

// Backend is the interface a netrunner server implementation provides
// to the ZAP wire layer. Each method maps 1:1 to an RPC opcode in
// opcodes.go. The wire layer is fully decoupled from the underlying
// network/orchestrator/local-cluster impl — just plug in any Backend.
type Backend interface {
	Ping(ctx context.Context) (*types.PingResponse, error)
	RPCVersion(ctx context.Context) (*types.RPCVersionResponse, error)
	Health(ctx context.Context) (*types.HealthResponse, error)
	URIs(ctx context.Context) (*types.URIsResponse, error)
	Status(ctx context.Context) (*types.StatusResponse, error)
	Stop(ctx context.Context) (*types.StopResponse, error)
	AddNode(ctx context.Context, req *types.AddNodeRequest) (*types.AddNodeResponse, error)
	RemoveNode(ctx context.Context, req *types.RemoveNodeRequest) (*types.RemoveNodeResponse, error)
	RestartNode(ctx context.Context, req *types.RestartNodeRequest) (*types.RestartNodeResponse, error)
	PauseNode(ctx context.Context, req *types.PauseNodeRequest) (*types.PauseNodeResponse, error)
}

// Server hosts a netrunner control RPC over ZAP.
type Server struct {
	be       Backend
	listener *zap.Listener
	srv      *zap.Server
}

// NewServer creates a netrunner ZAP server listening on addr.
func NewServer(addr string, be Backend) (*Server, error) {
	if be == nil {
		return nil, errors.New("zapwire: nil backend")
	}
	listener, err := zap.Listen(addr, nil)
	if err != nil {
		return nil, fmt.Errorf("zapwire: listen %s: %w", addr, err)
	}
	s := &Server{be: be, listener: listener}
	s.srv = zap.NewServer(listener, zap.HandlerFunc(s.handle))
	return s, nil
}

// Addr returns the actual listen address (useful for ":0" tests).
func (s *Server) Addr() string {
	return s.listener.Addr().String()
}

// Serve runs the server until ctx is cancelled or Close is called.
func (s *Server) Serve(ctx context.Context) error {
	return s.srv.Serve(ctx)
}

// Close shuts down the server.
func (s *Server) Close() error {
	if s.srv != nil {
		s.srv.Close()
	}
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

// handle is the single ZAP message handler. It dispatches to the
// backend based on the message-type opcode (and sub-opcode for
// fan-out opcodes).
//
// luxfi/api/zap.ServerConn.Read() already strips the request ID off
// the payload AND the response writer prepends it on the way back —
// so handlers see only the application bytes both inbound and out.
func (s *Server) handle(
	ctx context.Context,
	msgType zap.MessageType,
	payload []byte,
) (zap.MessageType, []byte, error) {
	switch msgType {
	case OpPing:
		resp, err := s.be.Ping(ctx)
		if err != nil {
			return 0, nil, err
		}
		return msgType, encodeResp(resp), nil

	case OpRPCVersion:
		resp, err := s.be.RPCVersion(ctx)
		if err != nil {
			return 0, nil, err
		}
		return msgType, encodeResp(resp), nil

	case OpStart:
		// Fan-out opcode: peel the sub-opcode byte and dispatch.
		sub, body, err := peelSubOpcode(payload)
		if err != nil {
			return 0, nil, err
		}
		return s.handleStartSub(ctx, msgType, sub, body)

	case OpStop:
		resp, err := s.be.Stop(ctx)
		if err != nil {
			return 0, nil, err
		}
		return msgType, encodeResp(resp), nil

	default:
		return 0, nil, fmt.Errorf("zapwire: unknown opcode 0x%02x", byte(msgType))
	}
}

func (s *Server) handleStartSub(
	ctx context.Context,
	msgType zap.MessageType,
	sub SubOpcode,
	body []byte,
) (zap.MessageType, []byte, error) {
	switch sub {
	case SubHealth:
		resp, err := s.be.Health(ctx)
		if err != nil {
			return 0, nil, err
		}
		return msgType, encodeResp(resp), nil
	case SubURIs:
		resp, err := s.be.URIs(ctx)
		if err != nil {
			return 0, nil, err
		}
		return msgType, encodeResp(resp), nil
	case SubStatus:
		resp, err := s.be.Status(ctx)
		if err != nil {
			return 0, nil, err
		}
		return msgType, encodeResp(resp), nil
	case SubAddNode:
		req := &types.AddNodeRequest{}
		if err := req.Decode(zap.NewReader(body)); err != nil {
			return 0, nil, err
		}
		resp, err := s.be.AddNode(ctx, req)
		if err != nil {
			return 0, nil, err
		}
		return msgType, encodeResp(resp), nil
	case SubRemoveNode:
		req := &types.RemoveNodeRequest{}
		if err := req.Decode(zap.NewReader(body)); err != nil {
			return 0, nil, err
		}
		resp, err := s.be.RemoveNode(ctx, req)
		if err != nil {
			return 0, nil, err
		}
		return msgType, encodeResp(resp), nil
	case SubRestartNode:
		req := &types.RestartNodeRequest{}
		if err := req.Decode(zap.NewReader(body)); err != nil {
			return 0, nil, err
		}
		resp, err := s.be.RestartNode(ctx, req)
		if err != nil {
			return 0, nil, err
		}
		return msgType, encodeResp(resp), nil
	case SubPauseNode:
		req := &types.PauseNodeRequest{}
		if err := req.Decode(zap.NewReader(body)); err != nil {
			return 0, nil, err
		}
		resp, err := s.be.PauseNode(ctx, req)
		if err != nil {
			return 0, nil, err
		}
		return msgType, encodeResp(resp), nil
	default:
		return 0, nil, fmt.Errorf("zapwire: unknown SubOp 0x%02x for OpStart", uint8(sub))
	}
}

// encodeResp serialises the response body. The transport prepends the
// request ID and applies MsgResponseFlag automatically.
func encodeResp(resp types.Encoder) []byte {
	buf := zap.GetBuffer()
	defer zap.PutBuffer(buf)
	resp.Encode(buf)
	return append([]byte(nil), buf.Bytes()...)
}
