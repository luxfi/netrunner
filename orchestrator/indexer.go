// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package orchestrator

import (
	"sync"
	"time"
)

// Indexer provides cross-engine blockchain indexing
type Indexer struct {
	mu    sync.RWMutex
	data  map[string]*IndexData
}

// IndexData contains indexed blockchain data
type IndexData struct {
	Engine       string
	LastUpdate   time.Time
	BlockHeight  uint64
	Transactions uint64
	Accounts     uint64
	Extra        map[string]interface{}
}

// NewIndexer creates a new indexer
func NewIndexer() *Indexer {
	return &Indexer{
		data: make(map[string]*IndexData),
	}
}

// Update updates index data for an engine
func (i *Indexer) Update(engine string, data *IndexData) {
	i.mu.Lock()
	defer i.mu.Unlock()
	
	data.Engine = engine
	data.LastUpdate = time.Now()
	i.data[engine] = data
}

// Get retrieves index data for an engine
func (i *Indexer) Get(engine string) *IndexData {
	i.mu.RLock()
	defer i.mu.RUnlock()
	
	return i.data[engine]
}

// GetAll retrieves all index data
func (i *Indexer) GetAll() map[string]*IndexData {
	i.mu.RLock()
	defer i.mu.RUnlock()
	
	result := make(map[string]*IndexData)
	for k, v := range i.data {
		result[k] = v
	}
	return result
}

// Clear clears index data for an engine
func (i *Indexer) Clear(engine string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	
	delete(i.data, engine)
}