// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package engines

import (
	"context"
	"testing"
	"time"

	"github.com/luxfi/ids"
)

func TestEngineTypes(t *testing.T) {
	// Test that engine types are defined correctly
	engines := []EngineType{
		EngineLux,
		EngineLux,
		EngineGeth,
		EngineOP,
		EngineEth2,
	}

	for _, e := range engines {
		if e == "" {
			t.Errorf("Engine type should not be empty")
		}
	}
}

func TestEngineRegistry(t *testing.T) {
	// Test that we can register and retrieve factories
	testFactory := func(name string, binary string) (Engine, error) {
		return &mockEngine{name: name}, nil
	}

	// Register a test engine
	Register("test", testFactory)

	// Try to create it
	engine, err := New("test", "test-engine", "")
	if err != nil {
		t.Fatalf("Failed to create test engine: %v", err)
	}

	if engine.Name() != "test-engine" {
		t.Errorf("Expected engine name 'test-engine', got '%s'", engine.Name())
	}
}

// mockEngine implements Engine interface for testing
type mockEngine struct {
	name string
}

func (m *mockEngine) Name() string                                 { return m.name }
func (m *mockEngine) Type() EngineType                             { return "test" }
func (m *mockEngine) NetworkID() uint32                            { return 1337 }
func (m *mockEngine) ChainID() ids.ID                              { return ids.Empty }
func (m *mockEngine) ParentChain() *ChainInfo                      { return nil }
func (m *mockEngine) Start(_ context.Context, _ *NodeConfig) error { return nil }
func (m *mockEngine) Stop(_ context.Context) error                 { return nil }
func (m *mockEngine) Restart(_ context.Context) error              { return nil }
func (m *mockEngine) Health(_ context.Context) (*HealthStatus, error) {
	return &HealthStatus{Healthy: true}, nil
}
func (m *mockEngine) IsRunning() bool                 { return true }
func (m *mockEngine) Uptime() time.Duration           { return 0 }
func (m *mockEngine) RPCEndpoint() string             { return "http://localhost:8545" }
func (m *mockEngine) WSEndpoint() string              { return "ws://localhost:8546" }
func (m *mockEngine) P2PEndpoint() string             { return "localhost:30303" }
func (m *mockEngine) Metrics() map[string]interface{} { return map[string]interface{}{} }
