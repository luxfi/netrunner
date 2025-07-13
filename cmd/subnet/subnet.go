// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnet

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

var (
	dataDir      string
	chainID      string
	port         string
	automine     bool
	mineInterval string
)

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "subnet",
		Short: "Run a single subnet node without P-chain registration",
		Long: `Run a LuxFi subnet in standalone mode, bypassing P-chain registration.
This is useful for development and testing with existing chain data.`,
		RunE: runSubnet,
	}

	cmd.PersistentFlags().StringVar(
		&dataDir,
		"data-dir",
		os.ExpandEnv("$HOME/.luxd"),
		"data directory for chain data",
	)
	cmd.PersistentFlags().StringVar(
		&chainID,
		"chain-id",
		"dnmzhuf6poM6PUNQCe7MWWfBdTJEnddhHRNXz2x7H6qSmyBEJ",
		"chain ID to run",
	)
	cmd.PersistentFlags().StringVar(
		&port,
		"port",
		"9650",
		"RPC port to listen on",
	)
	cmd.PersistentFlags().BoolVar(
		&automine,
		"automine",
		true,
		"enable automining",
	)
	cmd.PersistentFlags().StringVar(
		&mineInterval,
		"mine-interval",
		"2s",
		"mining interval when automine is enabled",
	)

	return cmd
}

func runSubnet(cmd *cobra.Command, args []string) error {
	fmt.Println("=== LuxFi Subnet Runner ===")
	fmt.Println("Running subnet in standalone mode")
	fmt.Println()

	// Setup paths
	chainDataPath := filepath.Join(dataDir, "chainData", chainID)
	vmID := "srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy"
	subnetEVMBin := filepath.Join(dataDir, "plugins", vmID)

	// Check chain data
	if _, err := os.Stat(chainDataPath); err != nil {
		return fmt.Errorf("chain data not found at %s: %w", chainDataPath, err)
	}

	// Get chain data size
	output, _ := exec.Command("du", "-sh", chainDataPath).Output()
	size := strings.Fields(string(output))[0]
	fmt.Printf("✓ Found chain data: %s\n", size)

	// Check subnet-evm binary
	if _, err := os.Stat(subnetEVMBin); err != nil {
		// Try to find it in common locations
		locations := []string{
			"/home/z/node/build/plugins/" + vmID,
			"/home/z/.avalanche-cli/bin/subnet-evm/subnet-evm",
			"/home/z/.avalanche-cli.current/bin/subnet-evm/subnet-evm-v0.6.12/subnet-evm",
		}

		found := false
		for _, loc := range locations {
			if _, err := os.Stat(loc); err == nil {
				fmt.Printf("Found subnet-evm at: %s\n", loc)
				if err := copyFile(loc, subnetEVMBin); err != nil {
					return fmt.Errorf("failed to copy subnet-evm: %w", err)
				}
				os.Chmod(subnetEVMBin, 0755)
				found = true
				break
			}
		}

		if !found {
			return fmt.Errorf("subnet-evm binary not found")
		}
	}

	// Create config directory
	configDir := filepath.Join(dataDir, "subnet-runner-config")
	os.MkdirAll(configDir, 0755)

	// Create config
	config := map[string]interface{}{
		"snowman-api-enabled": false,
		"admin-api-enabled":   true,
		"eth-apis": []string{
			"eth", "eth-filter", "net", "web3",
			"internal-eth", "internal-blockchain",
			"internal-transaction-pool", "internal-debug",
			"debug-tracer", "trace", "admin", "personal",
			"db", "txpool",
		},
		"pruning-enabled":               false,
		"local-txs-enabled":             true,
		"allow-unfinalized-queries":     true,
		"allow-unprotected-txs":         true,
		"preimages-enabled":             true,
		"tx-lookup-limit":               0,
		"skip-tx-indexing":              false,
		"accepted-cache-size":           32,
		"rpc-gas-cap":                   50000000,
		"rpc-tx-fee-cap":                100,
		"api-max-duration":              0,
		"api-max-blocks-per-request":    0,
		"remote-tx-gossip-only-enabled": false,
		"log-level":                     "info",
		"offline-pruning-enabled":       false,
		"snapshot-async":                true,
		"snapshot-verification-enabled": false,
		"metrics-enabled":               false,
		"chain-data-dir":                chainDataPath,
	}

	// Add automine config
	if automine {
		config["enable-auto-mine"] = true
		config["auto-mine-tx-interval"] = mineInterval
	}

	configPath := filepath.Join(configDir, "config.json")
	configData, _ := json.MarshalIndent(config, "", "  ")
	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	// Create chain config
	chainConfigDir := filepath.Join(configDir, chainID)
	os.MkdirAll(chainConfigDir, 0755)

	chainConfig := map[string]interface{}{
		"chainId":           96369,
		"homesteadBlock":    0,
		"eip150Block":       0,
		"eip155Block":       0,
		"eip158Block":       0,
		"byzantiumBlock":    0,
		"constantinopleBlock": 0,
		"petersburgBlock":   0,
		"istanbulBlock":     0,
		"muirGlacierBlock":  0,
		"subnetEVMTimestamp": 0,
		"feeConfig": map[string]interface{}{
			"gasLimit":                 15000000,
			"minBaseFee":               1000000000,
			"targetGas":                15000000,
			"baseFeeChangeDenominator": 48,
			"minBlockGasCost":          0,
			"maxBlockGasCost":          10000000,
			"targetBlockRate":          2,
			"blockGasCostStep":         500000,
		},
	}

	chainConfigPath := filepath.Join(chainConfigDir, "config.json")
	chainConfigData, _ := json.MarshalIndent(chainConfig, "", "  ")
	if err := os.WriteFile(chainConfigPath, chainConfigData, 0644); err != nil {
		return fmt.Errorf("failed to write chain config: %w", err)
	}

	// Kill any existing processes
	exec.Command("pkill", "-f", "subnet-evm").Run()
	time.Sleep(2 * time.Second)

	// Start subnet-evm
	logFile := filepath.Join(dataDir, "subnet-runner.log")
	log, err := os.Create(logFile)
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}
	defer log.Close()

	cmd = exec.Command(
		subnetEVMBin,
		"--config-file", configPath,
		"--chain-config-dir", configDir,
		"--http-host", "0.0.0.0",
		"--http-port", port,
		"--http-allowed-origins", "*",
		"--log-level", "info",
	)

	cmd.Stdout = log
	cmd.Stderr = log

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start subnet-evm: %w", err)
	}

	fmt.Printf("\nsubnet-evm started with PID: %d\n", cmd.Process.Pid)
	fmt.Printf("Logs at: %s\n", logFile)

	// Wait for RPC to be ready
	fmt.Print("\nWaiting for RPC to be ready")
	ready := false
	for i := 0; i < 30; i++ {
		if testRPC(port) {
			ready = true
			break
		}
		fmt.Print(".")
		time.Sleep(1 * time.Second)
	}

	if !ready {
		cmd.Process.Kill()
		return fmt.Errorf("RPC failed to start")
	}

	fmt.Println("\n✓ RPC is ready!")

	// Display status
	displayStatus(port)

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	fmt.Println("\nSubnet is running. Press Ctrl+C to stop.")

	select {
	case <-sigChan:
		fmt.Println("\nShutting down...")
		cmd.Process.Kill()
	case err := <-waitForProcess(cmd):
		if err != nil {
			return fmt.Errorf("subnet-evm exited: %w", err)
		}
	}

	return nil
}

func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	return err
}

func testRPC(port string) bool {
	client := &http.Client{Timeout: 1 * time.Second}
	
	reqBody := `{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}`
	resp, err := client.Post(
		fmt.Sprintf("http://localhost:%s", port),
		"application/json",
		strings.NewReader(reqBody),
	)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == 200
}

func displayStatus(port string) {
	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("    LUX SUBNET RUNNING")
	fmt.Println(strings.Repeat("=", 50))

	client := &http.Client{Timeout: 2 * time.Second}

	// Get chain ID
	chainIDResp := rpcCall(client, port, "eth_chainId", []interface{}{})
	if chainID, ok := chainIDResp["result"].(string); ok {
		fmt.Printf("\nChain ID: %s (%d)\n", chainID, hexToInt(chainID))
	}

	// Get block number
	blockResp := rpcCall(client, port, "eth_blockNumber", []interface{}{})
	if blockNum, ok := blockResp["result"].(string); ok {
		fmt.Printf("Block Number: %s (%d)\n", blockNum, hexToInt(blockNum))
	}

	// Get balance
	balanceResp := rpcCall(client, port, "eth_getBalance", []interface{}{
		"0x9011E888251AB053B7bD1cdB598Db4f9DEd94714",
		"latest",
	})
	if balance, ok := balanceResp["result"].(string); ok {
		fmt.Printf("Test Account Balance: %s\n", balance)
	}

	fmt.Printf("\nRPC Endpoint: http://localhost:%s\n", port)
	fmt.Println("\nTest Commands:")
	fmt.Printf("  # Get block number\n")
	fmt.Printf("  curl -X POST -H 'Content-Type: application/json' \\\n")
	fmt.Printf("    -d '{\"jsonrpc\":\"2.0\",\"method\":\"eth_blockNumber\",\"params\":[],\"id\":1}' \\\n")
	fmt.Printf("    http://localhost:%s\n", port)
	fmt.Println()
}

func rpcCall(client *http.Client, port string, method string, params interface{}) map[string]interface{} {
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	}

	body, _ := json.Marshal(reqBody)
	resp, err := client.Post(
		fmt.Sprintf("http://localhost:%s", port),
		"application/json",
		strings.NewReader(string(body)),
	)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return result
}

func hexToInt(hex string) int64 {
	if strings.HasPrefix(hex, "0x") {
		hex = hex[2:]
	}
	var result int64
	fmt.Sscanf(hex, "%x", &result)
	return result
}

func waitForProcess(cmd *exec.Cmd) <-chan error {
	ch := make(chan error, 1)
	go func() {
		ch <- cmd.Wait()
	}()
	return ch
}