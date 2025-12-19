//go:build integration
// +build integration

// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package orchestrator_test

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/luxfi/netrunner/engines"
	_ "github.com/luxfi/netrunner/engines/lux" // Register lux engine
	"github.com/luxfi/netrunner/orchestrator"
	"github.com/stretchr/testify/require"
)

func TestMultiEngineOrchestration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	// Skip if luxd binary is not available
	if _, err := exec.LookPath("luxd"); err != nil {
		t.Skip("luxd binary not found, skipping integration test")
	}

	ctx := context.Background()
	host := orchestrator.NewHost()

	// Test starting Lux engine
	t.Run("StartLuxEngine", func(t *testing.T) {
		options := &orchestrator.EngineOptions{
			NetworkID:   96369,
			HTTPPort:    19630,
			StakingPort: 19631,
			LogLevel:    "info",
			Extra: map[string]interface{}{
				"dev-mode":                         true,
				"sybil-protection-disabled-weight": 100,
				"health-check-frequency":           "2s",
			},
		}

		err := host.StartEngine(ctx, "test-lux", string(engines.EngineLux), options)
		require.NoError(t, err)

		// Wait for health
		time.Sleep(5 * time.Second)

		// Check engine is listed
		engines := host.ListEngines()
		require.Len(t, engines, 1)
		require.Equal(t, "test-lux", engines[0].Name)
		require.Equal(t, "lux", engines[0].Type)

		// Stop engine
		err = host.StopEngine(ctx, "test-lux")
		require.NoError(t, err)
	})

	// Test starting multiple engines
	t.Run("MultipleEngines", func(t *testing.T) {
		// Start Lux
		luxOptions := &orchestrator.EngineOptions{
			NetworkID:   96369,
			HTTPPort:    19640,
			StakingPort: 19641,
			LogLevel:    "info",
		}
		err := host.StartEngine(ctx, "lux-1", string(engines.EngineLux), luxOptions)
		require.NoError(t, err)

		// Start Lux
		avaOptions := &orchestrator.EngineOptions{
			NetworkID:   43114,
			HTTPPort:    19650,
			StakingPort: 19651,
			LogLevel:    "info",
		}
		err = host.StartEngine(ctx, "ava-1", string(engines.EngineLux), avaOptions)
		require.NoError(t, err)

		// Check both are running
		engines := host.ListEngines()
		require.Len(t, engines, 2)

		// Clean up
		host.StopEngine(ctx, "lux-1")
		host.StopEngine(ctx, "ava-1")
	})
}

func TestStackManifest(t *testing.T) {
	manifest := &orchestrator.StackManifest{
		Name:        "test-stack",
		Version:     "1.0.0",
		Description: "Test stack",
		Engines: []orchestrator.EngineConfig{
			{
				Name:        "lux-l1",
				Type:        "lux",
				NetworkID:   96369,
				HTTPPort:    9630,
				StakingPort: 9631,
				WaitHealthy: true,
			},
			{
				Name:      "lux-l2",
				Type:      "op",
				NetworkID: 200200,
				HTTPPort:  8545,
				DependsOn: []string{"lux-l1"},
			},
		},
	}

	t.Run("Validate", func(t *testing.T) {
		err := manifest.Validate()
		require.NoError(t, err)
	})

	t.Run("SaveAndLoad", func(t *testing.T) {
		// Save as YAML
		yamlPath := "/tmp/test-stack.yaml"
		err := manifest.Save(yamlPath)
		require.NoError(t, err)

		// Load back
		loaded, err := orchestrator.LoadManifest(yamlPath)
		require.NoError(t, err)
		require.Equal(t, manifest.Name, loaded.Name)
		require.Len(t, loaded.Engines, 2)

		// Save as JSON
		jsonPath := "/tmp/test-stack.json"
		err = manifest.Save(jsonPath)
		require.NoError(t, err)

		// Load JSON
		loaded, err = orchestrator.LoadManifest(jsonPath)
		require.NoError(t, err)
		require.Equal(t, manifest.Name, loaded.Name)
	})
}

func TestPortManager(t *testing.T) {
	pm := orchestrator.NewPortManager()

	t.Run("AllocatePorts", func(t *testing.T) {
		// Allocate HTTP ports
		port1 := pm.AllocateHTTP()
		port2 := pm.AllocateHTTP()
		require.NotEqual(t, port1, port2)

		// Allocate P2P ports
		p2p1 := pm.AllocateP2P()
		p2p2 := pm.AllocateP2P()
		require.NotEqual(t, p2p1, p2p2)

		// Release and reallocate
		pm.Release(port1)
		port3 := pm.AllocateHTTP()
		// May or may not reuse port1 depending on system state
		require.NotEqual(t, port3, port2)
	})

	t.Run("ReservePorts", func(t *testing.T) {
		pm := orchestrator.NewPortManager()
		
		// Reserve specific ports
		err := pm.Reserve(20000, 20001, 20002)
		require.NoError(t, err)

		// Try to reserve again - should fail
		err = pm.Reserve(20001)
		require.Error(t, err)
	})
}

func TestMetricsCollector(t *testing.T) {
	mc := orchestrator.NewMetricsCollector()

	t.Run("RecordAndRetrieve", func(t *testing.T) {
		// Record metrics
		mc.Record("engine1", map[string]interface{}{
			"uptime_seconds": 100.0,
			"running":        true,
			"peers":          5,
		})

		// Retrieve metrics
		metrics := mc.Get("engine1")
		require.NotNil(t, metrics)
		require.Equal(t, "engine1", metrics.Name)
		require.Equal(t, 100.0, metrics.Values["uptime_seconds"])

		// Record more for history
		for i := 0; i < 5; i++ {
			mc.Record("engine1", map[string]interface{}{
				"uptime_seconds": float64(100 + i*10),
				"running":        true,
			})
			time.Sleep(10 * time.Millisecond)
		}

		metrics = mc.Get("engine1")
		require.Len(t, metrics.History, 6) // Initial + 5 more
	})

	t.Run("Summary", func(t *testing.T) {
		mc := orchestrator.NewMetricsCollector()

		mc.Record("engine1", map[string]interface{}{
			"uptime_seconds": 100.0,
			"running":        true,
		})
		mc.Record("engine2", map[string]interface{}{
			"uptime_seconds": 200.0,
			"running":        true,
		})
		mc.Record("engine3", map[string]interface{}{
			"uptime_seconds": 50.0,
			"running":        false,
		})

		summary := mc.Summary()
		require.Equal(t, 3, summary["engines"])
		require.Equal(t, 350.0, summary["total_uptime_seconds"])
		require.Equal(t, 2, summary["healthy_engines"])
	})
}