// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package orchestrator

import (
	"fmt"
	"net"
	"sync"
)

// PortManager manages port allocation for engines
type PortManager struct {
	mu         sync.Mutex
	allocated  map[uint16]bool
	httpBase   uint16
	p2pBase    uint16
}

// NewPortManager creates a new port manager
func NewPortManager() *PortManager {
	return &PortManager{
		allocated: make(map[uint16]bool),
		httpBase:  9630,
		p2pBase:   9631,
	}
}

// AllocateHTTP allocates an HTTP port
func (pm *PortManager) AllocateHTTP() uint16 {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	
	port := pm.httpBase
	for pm.isInUse(port) || pm.allocated[port] {
		port += 10 // Skip by 10 to avoid conflicts with P2P ports
	}
	
	pm.allocated[port] = true
	return port
}

// AllocateP2P allocates a P2P port
func (pm *PortManager) AllocateP2P() uint16 {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	
	port := pm.p2pBase
	for pm.isInUse(port) || pm.allocated[port] {
		port += 10
	}
	
	pm.allocated[port] = true
	return port
}

// Release releases a port
func (pm *PortManager) Release(port uint16) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.allocated, port)
}

// isInUse checks if a port is in use
func (pm *PortManager) isInUse(port uint16) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return true // Port is in use
	}
	ln.Close()
	return false
}

// Reserve reserves specific ports
func (pm *PortManager) Reserve(ports ...uint16) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	
	// Check all ports are available first
	for _, port := range ports {
		if pm.allocated[port] {
			return fmt.Errorf("port %d already allocated", port)
		}
		if pm.isInUse(port) {
			return fmt.Errorf("port %d already in use", port)
		}
	}
	
	// Reserve all ports
	for _, port := range ports {
		pm.allocated[port] = true
	}
	
	return nil
}