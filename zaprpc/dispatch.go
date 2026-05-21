// Copyright (C) 2021-2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package zaprpc

import (
	"context"
	"fmt"

	"github.com/luxfi/zap"
)

// Dispatcher routes incoming ZAP messages by MsgType to typed handlers.
type Dispatcher struct {
	handlers map[MsgType]zap.Handler
}

// NewDispatcher returns an empty dispatcher.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{handlers: make(map[MsgType]zap.Handler)}
}

// Bind registers a typed handler for the given MsgType. The handler signature
// is `func(ctx, *Req) (*Resp, error)`; JSON encode/decode and ZAP framing
// happen here, so handlers stay business-logic-only.
func Bind[Req, Resp any](d *Dispatcher, msgType MsgType, h func(context.Context, *Req) (*Resp, error)) {
	d.handlers[msgType] = func(ctx context.Context, from string, msg *zap.Message) (*zap.Message, error) {
		var req Req
		if err := Decode(msg, &req); err != nil {
			return errorResponse(msgType, fmt.Errorf("decode %s request: %w", msgType, err))
		}
		resp, err := h(ctx, &req)
		var (
			body   any
			errStr string
		)
		if err != nil {
			errStr = err.Error()
		} else {
			body = resp
		}
		raw, encErr := Encode(msgType, body, errStr)
		if encErr != nil {
			return nil, encErr
		}
		return zap.Parse(raw)
	}
}

// HandlerFor returns the ZAP handler for the given MsgType, or nil if unbound.
// The returned function plugs straight into zap.Node.Handle.
func (d *Dispatcher) HandlerFor(msgType MsgType) zap.Handler {
	return d.handlers[msgType]
}

// AttachTo registers every bound handler onto the given ZAP node.
func (d *Dispatcher) AttachTo(n *zap.Node) {
	for mt, h := range d.handlers {
		n.Handle(uint16(mt), h)
	}
}

// MsgTypes returns the set of method IDs bound on this dispatcher.
func (d *Dispatcher) MsgTypes() []MsgType {
	out := make([]MsgType, 0, len(d.handlers))
	for mt := range d.handlers {
		out = append(out, mt)
	}
	return out
}

// errorResponse builds a ZAP message carrying just an RPC-level error.
// Returned to the client as the response to a request that failed before
// the handler could produce a typed body.
func errorResponse(msgType MsgType, err error) (*zap.Message, error) {
	raw, encErr := Encode(msgType, nil, err.Error())
	if encErr != nil {
		return nil, encErr
	}
	return zap.Parse(raw)
}
