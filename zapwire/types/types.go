// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package types defines the netrunner control RPC message types as
// hand-written Go structs. These replace the legacy rpcpb (protobuf-
// generated) types — there is no .proto, no codegen, no protobuf
// dependency. Each type owns its own ZAP encode/decode pair.
//
// Style: every struct has no embedded interfaces, no pointers to
// primitives unless null-distinct semantics matter, and every field is
// JSON-serialisable so test fixtures can be written by hand.
package types

import (
	"github.com/luxfi/api/zap"
)

// Encoder is implemented by every netrunner control message that goes
// over the wire. Encode appends the message bytes to the provided
// buffer. The 4-byte request ID is written by the transport layer
// before Encode is called; do not write it from inside Encode.
type Encoder interface {
	Encode(b *zap.Buffer)
}

// Decoder is implemented by every netrunner control message that comes
// off the wire. Decode reads the message from the reader and populates
// the receiver. The reader is positioned past the request ID and any
// sub-opcode byte before Decode is called.
type Decoder interface {
	Decode(r *zap.Reader) error
}

// PingRequest is empty; the wire payload is just the request ID.
type PingRequest struct{}

func (*PingRequest) Encode(b *zap.Buffer)        {}
func (*PingRequest) Decode(r *zap.Reader) error  { return nil }

// PingResponse carries the netrunner server's PID, used by clients to
// detect server identity changes (e.g. the daemon was restarted).
type PingResponse struct {
	PID int32
}

func (p *PingResponse) Encode(b *zap.Buffer) {
	b.WriteInt32(p.PID)
}

func (p *PingResponse) Decode(r *zap.Reader) error {
	pid, err := r.ReadInt32()
	if err != nil {
		return err
	}
	p.PID = pid
	return nil
}

// RPCVersionRequest is empty.
type RPCVersionRequest struct{}

func (*RPCVersionRequest) Encode(b *zap.Buffer)       {}
func (*RPCVersionRequest) Decode(r *zap.Reader) error { return nil }

// RPCVersionResponse carries the wire-protocol version. Bumped on
// breaking changes to the netrunner ZAP control protocol.
type RPCVersionResponse struct {
	Version uint32
}

func (r *RPCVersionResponse) Encode(b *zap.Buffer) {
	b.WriteUint32(r.Version)
}

func (r *RPCVersionResponse) Decode(rd *zap.Reader) error {
	v, err := rd.ReadUint32()
	if err != nil {
		return err
	}
	r.Version = v
	return nil
}

// HealthRequest is empty.
type HealthRequest struct{}

func (*HealthRequest) Encode(b *zap.Buffer)       {}
func (*HealthRequest) Decode(r *zap.Reader) error { return nil }

// HealthResponse carries a snapshot of the cluster's health.
type HealthResponse struct {
	ClusterInfo *ClusterInfo // nil if no cluster yet
}

func (h *HealthResponse) Encode(b *zap.Buffer) {
	if h.ClusterInfo == nil {
		b.WriteUint8(0)
		return
	}
	b.WriteUint8(1)
	h.ClusterInfo.Encode(b)
}

func (h *HealthResponse) Decode(r *zap.Reader) error {
	present, err := r.ReadUint8()
	if err != nil {
		return err
	}
	if present == 0 {
		h.ClusterInfo = nil
		return nil
	}
	h.ClusterInfo = &ClusterInfo{}
	return h.ClusterInfo.Decode(r)
}

// URIsRequest is empty.
type URIsRequest struct{}

func (*URIsRequest) Encode(b *zap.Buffer)       {}
func (*URIsRequest) Decode(r *zap.Reader) error { return nil }

// URIsResponse carries the public RPC endpoints of every node in the
// running cluster.
type URIsResponse struct {
	URIs []string
}

func (u *URIsResponse) Encode(b *zap.Buffer) {
	b.WriteUint32(uint32(len(u.URIs)))
	for _, uri := range u.URIs {
		b.WriteString(uri)
	}
}

func (u *URIsResponse) Decode(r *zap.Reader) error {
	n, err := r.ReadUint32()
	if err != nil {
		return err
	}
	u.URIs = make([]string, n)
	for i := uint32(0); i < n; i++ {
		s, err := r.ReadString()
		if err != nil {
			return err
		}
		u.URIs[i] = s
	}
	return nil
}

// StatusRequest is empty.
type StatusRequest struct{}

func (*StatusRequest) Encode(b *zap.Buffer)       {}
func (*StatusRequest) Decode(r *zap.Reader) error { return nil }

// StatusResponse carries the full cluster snapshot.
type StatusResponse struct {
	ClusterInfo *ClusterInfo
}

func (s *StatusResponse) Encode(b *zap.Buffer) {
	if s.ClusterInfo == nil {
		b.WriteUint8(0)
		return
	}
	b.WriteUint8(1)
	s.ClusterInfo.Encode(b)
}

func (s *StatusResponse) Decode(r *zap.Reader) error {
	present, err := r.ReadUint8()
	if err != nil {
		return err
	}
	if present == 0 {
		s.ClusterInfo = nil
		return nil
	}
	s.ClusterInfo = &ClusterInfo{}
	return s.ClusterInfo.Decode(r)
}

// ---- Composite types ----

// NodeInfo is the per-node payload inside ClusterInfo.
type NodeInfo struct {
	Name               string
	ExecPath           string
	URI                string
	ID                 string
	LogDir             string
	DBDir              string
	Config             []byte
	PluginDir          string
	WhitelistedSubnets string // legacy field name preserved for migration; rename pending
	Paused             bool
}

func (n *NodeInfo) Encode(b *zap.Buffer) {
	b.WriteString(n.Name)
	b.WriteString(n.ExecPath)
	b.WriteString(n.URI)
	b.WriteString(n.ID)
	b.WriteString(n.LogDir)
	b.WriteString(n.DBDir)
	b.WriteBytes(n.Config)
	b.WriteString(n.PluginDir)
	b.WriteString(n.WhitelistedSubnets)
	b.WriteBool(n.Paused)
}

func (n *NodeInfo) Decode(r *zap.Reader) error {
	var err error
	if n.Name, err = r.ReadString(); err != nil {
		return err
	}
	if n.ExecPath, err = r.ReadString(); err != nil {
		return err
	}
	if n.URI, err = r.ReadString(); err != nil {
		return err
	}
	if n.ID, err = r.ReadString(); err != nil {
		return err
	}
	if n.LogDir, err = r.ReadString(); err != nil {
		return err
	}
	if n.DBDir, err = r.ReadString(); err != nil {
		return err
	}
	cfg, err := r.ReadBytes()
	if err != nil {
		return err
	}
	// ReadBytes is zero-copy; copy because NodeInfo outlives the frame.
	n.Config = append([]byte(nil), cfg...)
	if n.PluginDir, err = r.ReadString(); err != nil {
		return err
	}
	if n.WhitelistedSubnets, err = r.ReadString(); err != nil {
		return err
	}
	if n.Paused, err = r.ReadBool(); err != nil {
		return err
	}
	return nil
}

// ChainInfo is the per-chain payload inside ClusterInfo.
type ChainInfo struct {
	ChainName string
	VMID      string
	VMName    string
	ChainID   string
	SubnetID  string // P-Chain validator-set identifier — distinct from ChainID
	Genesis   []byte
}

func (c *ChainInfo) Encode(b *zap.Buffer) {
	b.WriteString(c.ChainName)
	b.WriteString(c.VMID)
	b.WriteString(c.VMName)
	b.WriteString(c.ChainID)
	b.WriteString(c.SubnetID)
	b.WriteBytes(c.Genesis)
}

func (c *ChainInfo) Decode(r *zap.Reader) error {
	var err error
	if c.ChainName, err = r.ReadString(); err != nil {
		return err
	}
	if c.VMID, err = r.ReadString(); err != nil {
		return err
	}
	if c.VMName, err = r.ReadString(); err != nil {
		return err
	}
	if c.ChainID, err = r.ReadString(); err != nil {
		return err
	}
	if c.SubnetID, err = r.ReadString(); err != nil {
		return err
	}
	gen, err := r.ReadBytes()
	if err != nil {
		return err
	}
	c.Genesis = append([]byte(nil), gen...)
	return nil
}

// ClusterInfo is the top-level cluster snapshot returned by Health,
// Status, and StreamStatus.
type ClusterInfo struct {
	NodeNames    []string
	NodeInfos    map[string]*NodeInfo // keyed by node name
	PID          int32
	RootDataDir  string
	Healthy      bool
	CustomChains map[string]*ChainInfo // keyed by chain ID
	Subnets      []string              // P-Chain validator sets
	NetworkID    uint32
}

func (c *ClusterInfo) Encode(b *zap.Buffer) {
	b.WriteUint32(uint32(len(c.NodeNames)))
	for _, n := range c.NodeNames {
		b.WriteString(n)
	}
	b.WriteUint32(uint32(len(c.NodeInfos)))
	for _, ni := range c.NodeInfos {
		ni.Encode(b)
	}
	b.WriteInt32(c.PID)
	b.WriteString(c.RootDataDir)
	b.WriteBool(c.Healthy)
	b.WriteUint32(uint32(len(c.CustomChains)))
	for _, ch := range c.CustomChains {
		ch.Encode(b)
	}
	b.WriteUint32(uint32(len(c.Subnets)))
	for _, s := range c.Subnets {
		b.WriteString(s)
	}
	b.WriteUint32(c.NetworkID)
}

func (c *ClusterInfo) Decode(r *zap.Reader) error {
	n, err := r.ReadUint32()
	if err != nil {
		return err
	}
	c.NodeNames = make([]string, n)
	for i := uint32(0); i < n; i++ {
		if c.NodeNames[i], err = r.ReadString(); err != nil {
			return err
		}
	}
	n, err = r.ReadUint32()
	if err != nil {
		return err
	}
	c.NodeInfos = make(map[string]*NodeInfo, n)
	for i := uint32(0); i < n; i++ {
		ni := &NodeInfo{}
		if err := ni.Decode(r); err != nil {
			return err
		}
		c.NodeInfos[ni.Name] = ni
	}
	if c.PID, err = r.ReadInt32(); err != nil {
		return err
	}
	if c.RootDataDir, err = r.ReadString(); err != nil {
		return err
	}
	if c.Healthy, err = r.ReadBool(); err != nil {
		return err
	}
	n, err = r.ReadUint32()
	if err != nil {
		return err
	}
	c.CustomChains = make(map[string]*ChainInfo, n)
	for i := uint32(0); i < n; i++ {
		ch := &ChainInfo{}
		if err := ch.Decode(r); err != nil {
			return err
		}
		c.CustomChains[ch.ChainID] = ch
	}
	n, err = r.ReadUint32()
	if err != nil {
		return err
	}
	c.Subnets = make([]string, n)
	for i := uint32(0); i < n; i++ {
		if c.Subnets[i], err = r.ReadString(); err != nil {
			return err
		}
	}
	if c.NetworkID, err = r.ReadUint32(); err != nil {
		return err
	}
	return nil
}
