// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package commands

import (
	"github.com/luxfi/log"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/luxfi/netrunner/orchestrator"
	"github.com/spf13/cobra"
)

// NewStackCmd creates the stack management command
func NewStackCmd(logger log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stack",
		Short: "Manage multi-engine stacks",
		Long:  "Deploy and manage complex multi-consensus blockchain stacks",
	}

	cmd.AddCommand(
		newStackDeployCmd(logger),
		newStackDestroyCmd(logger),
		newStackStatusCmd(logger),
		newStackListCmd(logger),
		newStackValidateCmd(logger),
	)

	return cmd
}

func newStackDeployCmd(logger log.Logger) *cobra.Command {
	var (
		manifestPath string
		wait         bool
		timeout      time.Duration
	)

	cmd := &cobra.Command{
		Use:   "deploy [name]",
		Short: "Deploy a multi-engine stack from manifest",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stackName := args[0]
			
			// Load manifest
			manifest, err := orchestrator.LoadManifest(manifestPath)
			if err != nil {
				return fmt.Errorf("failed to load manifest: %w", err)
			}
			
			// Validate manifest
			if err := manifest.Validate(); err != nil {
				return fmt.Errorf("invalid manifest: %w", err)
			}
			
			// Create orchestrator host
			host := orchestrator.NewHost()
			
			ctx := cmd.Context()
			logger.Info("Deploying stack", 
				log.String("name", stackName),
				log.String("manifest", manifestPath),
				log.Int("engines", len(manifest.Engines)))
			
			fmt.Printf("🚀 Deploying stack '%s' with %d engines...\n", stackName, len(manifest.Engines))
			
			// Deploy engines in dependency order
			deployed := make(map[string]bool)
			remaining := make([]orchestrator.EngineConfig, len(manifest.Engines))
			copy(remaining, manifest.Engines)
			
			for len(remaining) > 0 {
				progress := false
				newRemaining := []orchestrator.EngineConfig{}
				
				for _, engineCfg := range remaining {
					// Check if dependencies are satisfied
					ready := true
					for _, dep := range engineCfg.DependsOn {
						if !deployed[dep] {
							ready = false
							break
						}
					}
					
					if !ready {
						newRemaining = append(newRemaining, engineCfg)
						continue
					}
					
					// Deploy this engine
					fmt.Printf("  ⚙️ Starting %s engine '%s'...\n", engineCfg.Type, engineCfg.Name)
					
					if err := host.StartEngine(ctx, engineCfg.Name, engineCfg.Type, &orchestrator.EngineOptions{
						NetworkID:   engineCfg.NetworkID,
						HTTPPort:    engineCfg.HTTPPort,
						WSPort:      engineCfg.WSPort,
						StakingPort: engineCfg.StakingPort,
						DataDir:     engineCfg.DataDir,
						LogLevel:    engineCfg.LogLevel,
						Binary:      engineCfg.Binary,
						Extra:       engineCfg.Extra,
					}); err != nil {
						return fmt.Errorf("failed to start engine %s: %w", engineCfg.Name, err)
					}
					
					// Wait for health if requested
					if engineCfg.WaitHealthy && wait {
						fmt.Printf("    ⏳ Waiting for health check...\n")
						if err := host.WaitForHealth(ctx, engineCfg.Name, 60*time.Second); err != nil {
							return fmt.Errorf("engine %s failed health check: %w", engineCfg.Name, err)
						}
						fmt.Printf("    ✅ Healthy!\n")
					}
					
					deployed[engineCfg.Name] = true
					progress = true
				}
				
				if !progress && len(newRemaining) > 0 {
					return fmt.Errorf("circular dependency detected or unsatisfied dependencies")
				}
				
				remaining = newRemaining
			}
			
			// Deploy bridge if configured
			if manifest.Bridge != nil {
				fmt.Printf("  🌉 Setting up bridge from %s to %s...\n", 
					manifest.Bridge.Source, manifest.Bridge.Destination)
				
				if err := host.SetupBridge(ctx, manifest.Bridge); err != nil {
					return fmt.Errorf("failed to setup bridge: %w", err)
				}
			}
			
			fmt.Printf("\n✅ Stack '%s' deployed successfully!\n", stackName)
			
			// Print endpoints
			fmt.Println("\n📊 Engine Endpoints:")
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ENGINE\tTYPE\tRPC\tWEBSOCKET")
			fmt.Fprintln(w, "------\t----\t---\t---------")
			
			for _, engine := range manifest.Engines {
				rpc := fmt.Sprintf("http://localhost:%d", engine.HTTPPort)
				ws := fmt.Sprintf("ws://localhost:%d", engine.WSPort)
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", engine.Name, engine.Type, rpc, ws)
			}
			w.Flush()
			
			// Save stack info
			stackDir := filepath.Join(os.Getenv("HOME"), ".netrunner", "stacks", stackName)
			if err := os.MkdirAll(stackDir, 0755); err != nil {
				logger.Warn("Failed to create stack directory", log.Err(err))
			} else {
				// Save manifest copy
				manifestCopy := filepath.Join(stackDir, "manifest.yaml")
				if err := manifest.Save(manifestCopy); err != nil {
					logger.Warn("Failed to save manifest copy", log.Err(err))
				}
				
				// Save host state
				stateFile := filepath.Join(stackDir, "state.json")
				if err := host.SaveState(stateFile); err != nil {
					logger.Warn("Failed to save host state", log.Err(err))
				}
			}
			
			return nil
		},
	}
	
	cmd.Flags().StringVarP(&manifestPath, "manifest", "f", "", "Path to stack manifest file")
	cmd.Flags().BoolVarP(&wait, "wait", "w", true, "Wait for engines to be healthy")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "Deployment timeout")
	cmd.MarkFlagRequired("manifest")
	
	return cmd
}

func newStackDestroyCmd(logger log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "destroy [name]",
		Short: "Destroy a deployed stack",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stackName := args[0]
			
			// Load stack state
			stackDir := filepath.Join(os.Getenv("HOME"), ".netrunner", "stacks", stackName)
			stateFile := filepath.Join(stackDir, "state.json")
			
			host := orchestrator.NewHost()
			if err := host.LoadState(stateFile); err != nil {
				return fmt.Errorf("failed to load stack state: %w", err)
			}
			
			ctx := cmd.Context()
			fmt.Printf("🛑 Destroying stack '%s'...\n", stackName)
			
			// Stop all engines
			if err := host.StopAll(ctx); err != nil {
				return fmt.Errorf("failed to stop engines: %w", err)
			}
			
			// Clean up stack directory
			if err := os.RemoveAll(stackDir); err != nil {
				logger.Warn("Failed to remove stack directory", log.Err(err))
			}
			
			fmt.Printf("✅ Stack '%s' destroyed\n", stackName)
			return nil
		},
	}
}

func newStackStatusCmd(logger log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "status [name]",
		Short: "Get status of a deployed stack",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stackName := args[0]
			
			// Load stack state
			stackDir := filepath.Join(os.Getenv("HOME"), ".netrunner", "stacks", stackName)
			stateFile := filepath.Join(stackDir, "state.json")
			
			host := orchestrator.NewHost()
			if err := host.LoadState(stateFile); err != nil {
				return fmt.Errorf("failed to load stack state: %w", err)
			}
			
			ctx := cmd.Context()
			fmt.Printf("📊 Status of stack '%s':\n\n", stackName)
			
			// Get status of all engines
			statuses, err := host.GetAllStatus(ctx)
			if err != nil {
				return fmt.Errorf("failed to get status: %w", err)
			}
			
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ENGINE\tTYPE\tSTATUS\tBLOCK HEIGHT\tPEERS\tVERSION")
			fmt.Fprintln(w, "------\t----\t------\t------------\t-----\t-------")
			
			for name, status := range statuses {
				health := "❌ Unhealthy"
				if status.Health.Healthy {
					health = "✅ Healthy"
				}
				
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\n",
					name,
					status.Type,
					health,
					status.Health.BlockHeight,
					status.Health.PeerCount,
					status.Health.Version)
			}
			w.Flush()
			
			// Show metrics
			fmt.Println("\n📈 Metrics:")
			metrics := host.GetMetrics()
			for engine, m := range metrics {
				fmt.Printf("  %s:\n", engine)
				for k, v := range m.Values {
					fmt.Printf("    %s: %v\n", k, v)
				}
			}
			
			return nil
		},
	}
}

func newStackListCmd(logger log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all deployed stacks",
		RunE: func(cmd *cobra.Command, args []string) error {
			stacksDir := filepath.Join(os.Getenv("HOME"), ".netrunner", "stacks")
			
			// Ensure directory exists
			if _, err := os.Stat(stacksDir); os.IsNotExist(err) {
				fmt.Println("No stacks deployed")
				return nil
			}
			
			// List stack directories
			entries, err := os.ReadDir(stacksDir)
			if err != nil {
				return fmt.Errorf("failed to list stacks: %w", err)
			}
			
			if len(entries) == 0 {
				fmt.Println("No stacks deployed")
				return nil
			}
			
			fmt.Println("Deployed stacks:")
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tCREATED\tENGINES")
			fmt.Fprintln(w, "----\t-------\t-------")
			
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				
				info, _ := entry.Info()
				
				// Try to load manifest to get engine count
				manifestPath := filepath.Join(stacksDir, entry.Name(), "manifest.yaml")
				engineCount := "?"
				if manifest, err := orchestrator.LoadManifest(manifestPath); err == nil {
					engineCount = fmt.Sprintf("%d", len(manifest.Engines))
				}
				
				fmt.Fprintf(w, "%s\t%s\t%s\n", 
					entry.Name(),
					info.ModTime().Format("2006-01-02 15:04"),
					engineCount)
			}
			w.Flush()
			
			return nil
		},
	}
}

func newStackValidateCmd(logger log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "validate [manifest]",
		Short: "Validate a stack manifest",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manifestPath := args[0]
			
			// Load manifest
			manifest, err := orchestrator.LoadManifest(manifestPath)
			if err != nil {
				return fmt.Errorf("failed to load manifest: %w", err)
			}
			
			// Validate
			if err := manifest.Validate(); err != nil {
				fmt.Printf("❌ Manifest validation failed: %v\n", err)
				return err
			}
			
			fmt.Printf("✅ Manifest is valid\n")
			fmt.Printf("\n📋 Summary:\n")
			fmt.Printf("  Name: %s\n", manifest.Name)
			fmt.Printf("  Version: %s\n", manifest.Version)
			fmt.Printf("  Engines: %d\n", len(manifest.Engines))
			
			if manifest.Bridge != nil {
				fmt.Printf("  Bridge: %s → %s\n", manifest.Bridge.Source, manifest.Bridge.Destination)
			}
			
			// Check for dependency cycles
			fmt.Printf("\n🔍 Checking dependencies...\n")
			visited := make(map[string]bool)
			recStack := make(map[string]bool)
			
			var hasCycle func(string) bool
			hasCycle = func(name string) bool {
				visited[name] = true
				recStack[name] = true
				
				// Find engine config
				var engine *orchestrator.EngineConfig
				for i := range manifest.Engines {
					if manifest.Engines[i].Name == name {
						engine = &manifest.Engines[i]
						break
					}
				}
				
				if engine != nil {
					for _, dep := range engine.DependsOn {
						if !visited[dep] {
							if hasCycle(dep) {
								return true
							}
						} else if recStack[dep] {
							return true
						}
					}
				}
				
				recStack[name] = false
				return false
			}
			
			for _, engine := range manifest.Engines {
				if !visited[engine.Name] {
					if hasCycle(engine.Name) {
						fmt.Printf("  ❌ Circular dependency detected involving %s\n", engine.Name)
						return fmt.Errorf("circular dependency detected")
					}
				}
			}
			
			fmt.Printf("  ✅ No circular dependencies\n")
			
			return nil
		},
	}
}