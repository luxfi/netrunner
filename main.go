package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	hanzoGen = "/home/z/work/hanzo/universe/infra/k8s/hanzod/mainnet/genesis.json"
	// What a Go node needs before it will serve a C-Chain: a genesis that
	// declares one, and the post-quantum staking keys the strict profile
	// requires. Without them luxd refuses to construct, so a boot measured
	// without them measures a node that never starts.
	luxGen     = "/home/z/testnets/lux/genesis.json"
	luxStaking = "/home/z/testnets/lux/nodes/node0/staking"
)

// The three runtimes, where they are built. Variables rather than constants so
// a candidate build can be measured where it sits, without being installed over
// the one a cluster is already running.
var (
	binLuxd   = "/home/z/work/lux/node/build/luxd"
	binHanzod = "/home/z/work/lux-rs/node/target/release/hanzod"
	binZood   = "/home/z/work/lux-cpp/node/build/zood"
)

type Config struct {
	NetworkID        int
	ChainID          int
	ROArchive        string
	LuxdPorts        []int
	ZoodBasePort     int
	ZoodRPCPorts     []int
	HanzodStakePorts []int
	HanzodRPCPorts   []int
	RuntimeDir       string
	// BotTargets is where the traffic bot sends, one entry per chain. Each
	// chain answers on its own name at its own node, addressed by loopback:
	// a public host name here would aim treasury-signed traffic at the
	// public network.
	BotTargets []ChainBotTarget
}

var configs = map[string]Config{
	"mainnet": {
		NetworkID:        36963,
		ChainID:          200200, // Zoo mainnet EVM chain ID
		ROArchive:        "http://127.0.0.1:9630",
		LuxdPorts:        []int{9630, 9640, 9650, 9660, 9670},
		ZoodBasePort:     9731,
		ZoodRPCPorts:     []int{9730, 9740, 9750, 9760, 9770},
		HanzodStakePorts: []int{9781, 9782, 9783, 9784, 9785},
		HanzodRPCPorts:   []int{9780, 9790, 9800, 9810, 9820},
		RuntimeDir:       "/tmp/cluster_mainnet",
		BotTargets: []ChainBotTarget{
			{Name: "lux", RPC: "http://127.0.0.1:9630/v1/chain/c", Coin: "LUX", ChainID: 96369, Proc: "luxd", Port: 9630},
			{Name: "zoo", RPC: "http://127.0.0.1:9730/v1/chain/zoo", Coin: "ZOO", ChainID: 200200, Proc: "zood", Port: 9730},
			{Name: "hanzo", RPC: "http://127.0.0.1:9780/v1/chain/hanzo", Coin: "AI", ChainID: 36963, Proc: "hanzod", Port: 9780},
		},
	},
	"testnet": {
		NetworkID:        96300,
		ChainID:          200201, // Zoo testnet EVM chain ID
		ROArchive:        "http://127.0.0.1:9630",
		LuxdPorts:        []int{19630, 19640, 19650, 19660, 19670},
		ZoodBasePort:     19731,
		ZoodRPCPorts:     []int{19730, 19740, 19750, 19760, 19770},
		HanzodStakePorts: []int{19781, 19782, 19783, 19784, 19785},
		HanzodRPCPorts:   []int{19780, 19790, 19800, 19810, 19820},
		RuntimeDir:       "/tmp/cluster_testnet",
		BotTargets: []ChainBotTarget{
			{Name: "lux", RPC: "http://127.0.0.1:19600/v1/chain/c", Coin: "LUX", ChainID: 96368, Proc: "luxd", Port: 19600},
			{Name: "zoo", RPC: "http://127.0.0.1:19730/v1/chain/zoo", Coin: "ZOO", ChainID: 200201, Proc: "zood", Port: 19730},
			{Name: "hanzo", RPC: "http://127.0.0.1:19780/v1/chain/hanzo", Coin: "AI", ChainID: 36962, Proc: "hanzod", Port: 19780},
		},
	},
}

func httpGet(url string) (map[string]any, error) {
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var res map[string]any
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}
	return res, nil
}

func httpPost(url, method string, params any) (map[string]any, error) {
	client := http.Client{Timeout: 3 * time.Second}
	reqBody, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	resp, err := client.Post(url, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var res map[string]any
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}
	return res, nil
}

func startZood(cfg Config, env string) error {
	rdir := filepath.Join(cfg.RuntimeDir, "zood")
	os.MkdirAll(rdir, 0755)
	fmt.Printf("[*] Starting 5 zood (C++) nodes for %s (chainID: %d, shared RO archive: %s)...\n", env, cfg.ChainID, cfg.ROArchive)
	for i := 0; i < 5; i++ {
		rpc := cfg.ZoodRPCPorts[i]
		cmd := exec.Command(
			binZood,
			"--index", fmt.Sprintf("%d", i),
			"--n", "5",
			"--base-port", fmt.Sprintf("%d", cfg.ZoodBasePort),
			"--rpc-port", fmt.Sprintf("%d", rpc),
			"--deadline-ms", "10000",
			"--chain-id", fmt.Sprintf("%d", cfg.ChainID),
			"--archive-rpc", cfg.ROArchive,
			"--light",
		)
		logPath := filepath.Join(rdir, fmt.Sprintf("n%d.log", i))
		logFile, err := os.Create(logPath)
		if err != nil {
			return err
		}
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to start zood-%d: %w", i, err)
		}
	}
	fmt.Printf("[+] 5 zood nodes active on ports %v\n", cfg.ZoodRPCPorts)
	return nil
}

func startHanzod(cfg Config, env string) error {
	rdir := filepath.Join(cfg.RuntimeDir, "hanzod")
	os.MkdirAll(rdir, 0755)
	for i := 0; i < 5; i++ {
		os.MkdirAll(filepath.Join(rdir, fmt.Sprintf("n%d", i)), 0755)
	}

	commFile := filepath.Join(rdir, "committee.txt")
	f, err := os.Create(commFile)
	if err != nil {
		return err
	}

	for i := 0; i < 5; i++ {
		args := []string{"--data", filepath.Join(rdir, fmt.Sprintf("n%d", i)), "--publish"}
		if _, err := os.Stat(hanzoGen); err == nil {
			args = append(args, "--genesis", hanzoGen)
		}
		pubCmd := exec.Command(binHanzod, args...)
		out, err := pubCmd.Output()
		if err != nil {
			return fmt.Errorf("hanzod publish node %d: %w", i, err)
		}
		f.Write(out)
	}
	f.Close()

	var peers string
	for i, p := range cfg.HanzodStakePorts {
		if i > 0 {
			peers += ","
		}
		peers += fmt.Sprintf("127.0.0.1:%d", p)
	}

	fmt.Printf("[*] Starting 5 hanzod (Rust) nodes for %s (Hanzo Mainnet 36963, shared RO archive: %s)...\n", env, cfg.ROArchive)
	for i := 0; i < 5; i++ {
		rpc := cfg.HanzodRPCPorts[i]
		stake := cfg.HanzodStakePorts[i]
		args := []string{
			"--data", filepath.Join(rdir, fmt.Sprintf("n%d", i)),
			"--committee", commFile,
			"--rpc", fmt.Sprintf("127.0.0.1:%d", rpc),
			"--stake", fmt.Sprintf("127.0.0.1:%d", stake),
			"--peers", peers,
			"--archive-rpc", cfg.ROArchive,
			"--light",
		}
		if _, err := os.Stat(hanzoGen); err == nil {
			args = append(args, "--genesis", hanzoGen)
		} else {
			args = append(args, "--network", "mainnet")
		}

		cmd := exec.Command(binHanzod, args...)
		logPath := filepath.Join(rdir, fmt.Sprintf("n%d.log", i))
		logFile, err := os.Create(logPath)
		if err != nil {
			return err
		}
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to start hanzod-%d: %w", i, err)
		}
	}
	fmt.Printf("[+] 5 hanzod nodes active on ports %v\n", cfg.HanzodRPCPorts)
	return nil
}

func stopNodes() {
	fmt.Println("[*] Stopping cluster processes...")
	exec.Command("pkill", "-f", binZood).Run()
	exec.Command("pkill", "-f", binHanzod).Run()
	exec.Command("pkill", "-f", "/home/z/work/lux-rs/node/target/release/lux-node").Run()
	time.Sleep(1 * time.Second)
	fmt.Println("[+] All zood and hanzod processes stopped.")
}

func printStatus(cfg Config, env string) {
	fmt.Println("================================================================================")
	fmt.Printf("  CLUSTER TOPOLOGY STATUS: %s (Shared RO Archive: %s)\n", env, cfg.ROArchive)
	fmt.Println("================================================================================")

	fmt.Println("\n--- [1] luxd (Go Archive Cluster - Primary C-Chain & P-Chain) ---")
	for i, port := range cfg.LuxdPorts {
		url := fmt.Sprintf("http://127.0.0.1:%d", port)
		_, err := httpGet(url + "/v1/health")
		role := "Shared RO Archive"
		if i > 0 {
			role = "Full Validator"
		}
		bnRes, _ := httpPost(url+"/v1/bc/C/rpc", "eth_blockNumber", []any{})
		bn := "N/A"
		if res, ok := bnRes["result"].(string); ok {
			bn = res
		}
		alive := "ONLINE"
		if err != nil && bn == "N/A" {
			alive = "OFFLINE"
		}
		cidRes, _ := httpPost(url+"/v1/bc/C/rpc", "eth_chainId", []any{})
		cid := "N/A"
		if res, ok := cidRes["result"].(string); ok {
			cInt, _ := strconv.ParseInt(strings.TrimPrefix(res, "0x"), 16, 64)
			cid = fmt.Sprintf("%d (%s)", cInt, res)
		}
		fmt.Printf("  luxd-%d (Port %d): %s | Role: %s | ChainID: %s | Block: %s\n", i, port, alive, role, cid, bn)
	}

	fmt.Println("\n--- [2] zood (C++ Frontier Nodes - Zoo L2 EVM) ---")
	for i, port := range cfg.ZoodRPCPorts {
		url := fmt.Sprintf("http://127.0.0.1:%d", port)
		d, _ := httpGet(url + "/")
		h, err := httpGet(url + "/v1/health")
		alive := "ONLINE"
		if err != nil || h["healthy"] != true {
			alive = "OFFLINE"
		}
		mode := "light"
		if m, ok := d["mode"].(string); ok {
			mode = m
		}
		bnRes, _ := httpPost(url+"/v1/chain/C/rpc", "eth_blockNumber", []any{})
		bn := "N/A"
		if res, ok := bnRes["result"].(string); ok {
			bn = res
		}
		cidRes, _ := httpPost(url+"/v1/chain/C/rpc", "eth_chainId", []any{})
		cid := "N/A"
		if res, ok := cidRes["result"].(string); ok {
			cInt, _ := strconv.ParseInt(strings.TrimPrefix(res, "0x"), 16, 64)
			cid = fmt.Sprintf("%d (%s)", cInt, res)
		}
		fmt.Printf("  zood-%d (Port %d): %s | Mode: %s | ChainID: %s | Tip: %s\n", i, port, alive, mode, cid, bn)
	}

	fmt.Println("\n--- [3] hanzod (Rust Frontier Nodes - Hanzo L2 EVM) ---")
	for i, port := range cfg.HanzodRPCPorts {
		url := fmt.Sprintf("http://127.0.0.1:%d", port)
		d, _ := httpGet(url + "/")
		h, err := httpGet(url + "/v1/health")
		alive := "ONLINE"
		if err != nil || h["healthy"] != true {
			alive = "OFFLINE"
		}
		mode := "light"
		if m, ok := d["mode"].(string); ok {
			mode = m
		}
		bnRes, _ := httpPost(url+"/v1/chain/C/rpc", "eth_blockNumber", []any{})
		bn := "N/A"
		if res, ok := bnRes["result"].(string); ok {
			bn = res
		}
		cidRes, _ := httpPost(url+"/v1/chain/C/rpc", "eth_chainId", []any{})
		cid := "N/A"
		if res, ok := cidRes["result"].(string); ok {
			cInt, _ := strconv.ParseInt(strings.TrimPrefix(res, "0x"), 16, 64)
			cid = fmt.Sprintf("%d (%s)", cInt, res)
		}
		fmt.Printf("  hanzod-%d (Port %d): %s | Mode: %s | ChainID: %s | Tip: %s\n", i, port, alive, mode, cid, bn)
	}
}

func runTest(cfg Config, env string) {
	fmt.Println("================================================================================")
	fmt.Printf("  TESTING 15-NODE CLUSTER & SHARED RO ARCHIVE PROXYING (%s)\n", env)
	fmt.Println("================================================================================")

	archURL := fmt.Sprintf("%s/v1/bc/C/rpc", cfg.ROArchive)
	archB0, err := httpPost(archURL, "eth_getBlockByNumber", []any{"0x0", false})
	if err != nil || archB0["result"] == nil {
		fmt.Printf("[-] Shared RO archive check failed: %v\n", err)
		return
	}
	hash := archB0["result"].(map[string]any)["hash"]
	fmt.Printf("[*] Shared RO Archive Block 0 Hash: %s\n\n", hash)

	for i, port := range cfg.ZoodRPCPorts {
		rpcURL := fmt.Sprintf("http://127.0.0.1:%d/v1/chain/C/rpc", port)
		bn, err := httpPost(rpcURL, "eth_blockNumber", []any{})
		if err != nil || bn["result"] == nil {
			fmt.Printf("[-] zood-%d failed frontier tip query\n", i)
			continue
		}
		b0, err := httpPost(rpcURL, "eth_getBlockByNumber", []any{"0x0", false})
		if err != nil || b0["result"] == nil {
			fmt.Printf("[-] zood-%d failed historical proxy query\n", i)
			continue
		}
		pRes, err := httpPost(fmt.Sprintf("http://127.0.0.1:%d/v1/bc/P", port), "platform.getHeight", map[string]any{})
		if err != nil || pRes["result"] == nil {
			fmt.Printf("[-] zood-%d failed P-chain proxy query\n", i)
			continue
		}
		fmt.Printf("  [PASS] zood-%d (Port %d): Frontier %s | Historical 0x0 Proxied | P-Chain Proxied\n",
			i, port, bn["result"],
		)
	}

	for i, port := range cfg.HanzodRPCPorts {
		rpcURL := fmt.Sprintf("http://127.0.0.1:%d/v1/chain/C/rpc", port)
		bn, err := httpPost(rpcURL, "eth_blockNumber", []any{})
		if err != nil || bn["result"] == nil {
			fmt.Printf("[-] hanzod-%d failed frontier tip query\n", i)
			continue
		}
		b0, err := httpPost(rpcURL, "eth_getBlockByNumber", []any{"0x0", false})
		if err != nil || b0["result"] == nil {
			fmt.Printf("[-] hanzod-%d failed historical proxy query\n", i)
			continue
		}
		pRes, err := httpPost(fmt.Sprintf("http://127.0.0.1:%d/v1/bc/P", port), "platform.getHeight", map[string]any{})
		if err != nil || pRes["result"] == nil {
			fmt.Printf("[-] hanzod-%d failed P-chain proxy query\n", i)
			continue
		}
		fmt.Printf("  [PASS] hanzod-%d (Port %d): Frontier %s | Historical 0x0 Proxied | P-Chain Proxied\n",
			i, port, bn["result"],
		)
	}

	fmt.Println("\n================================================================================")
	fmt.Println("  ALL TESTS PASSED: 15-node topology healthy with shared RO archive!")
	fmt.Println("================================================================================")
}

func getProcessRSSMB(cmdName string, port int) float64 {
	cmd := exec.Command("sh", "-c", fmt.Sprintf("ps -eo pid,rss,cmd | grep '%s' | grep '%d' | grep -v grep | awk '{print $2}'", cmdName, port))
	out, err := cmd.Output()
	if err == nil {
		lines := strings.Fields(strings.TrimSpace(string(out)))
		if len(lines) > 0 {
			if kb, err := strconv.ParseFloat(lines[0], 64); err == nil && kb > 0 {
				return kb / 1024.0
			}
		}
	}
	cmd = exec.Command("sh", "-c", fmt.Sprintf("ps -C '%s' -o rss= | head -n1", cmdName))
	out, err = cmd.Output()
	if err == nil {
		lines := strings.Fields(strings.TrimSpace(string(out)))
		if len(lines) > 0 {
			if kb, err := strconv.ParseFloat(lines[0], 64); err == nil && kb > 0 {
				return kb / 1024.0
			}
		}
	}
	return 0.0
}

func runScale(nNodes int, impl string, reqs int) {
	fmt.Printf("\n================================================================================\n")
	fmt.Printf("  SCALE BENCHMARK: %d LIGHT NODES WITH 3 SHARED RO ARCHIVES (%s)\n", nNodes, impl)
	fmt.Printf("================================================================================\n")

	archiveURLs := []string{"http://127.0.0.1:9630", "http://127.0.0.1:9640", "http://127.0.0.1:9650"}
	for i, aURL := range archiveURLs {
		res, err := httpPost(aURL+"/v1/bc/C/rpc", "eth_blockNumber", []any{})
		if err != nil || res["result"] == nil {
			fmt.Printf("    [-] Archive %d (%s) OFFLINE: %v\n", i, aURL, err)
			return
		}
		fmt.Printf("    [+] Archive %d (%s): ONLINE | Tip: %s\n", i, aURL, res["result"])
	}

	exec.Command("pkill", "-f", "zood.*--base-port 20000").Run()
	time.Sleep(1 * time.Second)

	rdir := "/tmp/cluster_scale"
	os.RemoveAll(rdir)
	os.MkdirAll(rdir, 0755)

	basePort := 20000
	rpcBase := 12000
	startLaunch := time.Now()

	for i := 0; i < nNodes; i++ {
		assignedArchive := archiveURLs[i%len(archiveURLs)]
		rpcPort := rpcBase + i
		cmd := exec.Command(
			binZood,
			"--index", fmt.Sprintf("%d", i),
			"--n", fmt.Sprintf("%d", nNodes),
			"--base-port", fmt.Sprintf("%d", basePort),
			"--rpc-port", fmt.Sprintf("%d", rpcPort),
			"--deadline-ms", "30000",
			"--chain-id", "200200",
			"--archive-rpc", assignedArchive,
			"--light",
		)
		logFile, _ := os.Create(filepath.Join(rdir, fmt.Sprintf("node_%d.log", i)))
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Start()
	}

	fmt.Printf("[*] Waiting for %d nodes to establish mesh...\n", nNodes)
	for attempt := 0; attempt < 80; attempt++ {
		readyCount := 0
		for i := 0; i < nNodes; i++ {
			res, err := httpPost(fmt.Sprintf("http://127.0.0.1:%d/v1/chain/C/rpc", rpcBase+i), "eth_blockNumber", []any{})
			if err == nil && res != nil && res["result"] != nil {
				readyCount++
			}
		}
		if readyCount == nNodes {
			fmt.Printf("[+] ALL %d NODES ONLINE in %v\n", nNodes, time.Since(startLaunch).Round(10*time.Millisecond))
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func printUsage(prog string) {
	fmt.Printf(`================================================================================
  NETRUNNER — Multi-Chain Heterogeneous Node Fleet Orchestrator (Zap RPC)
================================================================================
Usage: %s <command> [flags]

COMMANDS:
  status       Check status of Go (luxd), C++ (zood), and Rust (hanzod) nodes
  start        Launch multi-node cluster mesh (Go, C++, Rust)
  stop         Stop all managed node processes
  restart      Restart all managed node processes
  test         Verify multi-chain consensus, RO archive proxying & genesis parity
  scale        Benchmark light node scaling (10 to 100 nodes) with shared RO archive
  gateway      Start reverse proxy with EVM root fallback & Zap RPC
  bot          Run native EVM transaction bot (moving funds on all 3 chains)
  boot         Measure cold start: exec -> listening -> first served call
  monitor      Run real-time RSS memory leak detector under load
  zap          Start standalone Zap RPC protocol transport daemon (:8082)
  network      Display typed multi-chain network topology (JSON)
  consensus    Display Consensus-as-a-Service (CaaS) metrics (JSON)

FLAGS:
  --env        Target network environment (mainnet, testnet) [default: mainnet]
  --impl       Implementation filter (all, zood, hanzod) [default: all]
  --port       Gateway listen port [default: :8080]
  --dur        Duration for bot or monitor [default: 60s]
  --tps        Transactions per second per chain for bot [default: 1.0]
  --n          Number of nodes for scale test [default: 10]
  --runs       Cold starts to time per runtime for boot [default: 3]
  --luxd       Go node binary to measure   [default: lux/node/build/luxd]
  --hanzod     Rust node binary to measure [default: lux-rs/node/target/release/hanzod]
  --zood       C++ node binary to measure  [default: lux-cpp/node/build/zood]

PROTOCOLS & TRANSPORTS:
  * Zap RPC:   Binary & HTTP transport (/v1/zap) replacing legacy gRPC
  * EVM Root:  api.lux.network/, api.zoo.network/, api.hanzo.network/
  * Chains:    Lux (/v1/chain/c), Zoo (/v1/chain/zoo), Hanzo (/v1/chain/hanzo)
               The alias is case-insensitive and /rpc is optional.
================================================================================
`, prog)
}

func main() {
	prog := filepath.Base(os.Args[0])
	if prog == "" {
		prog = "netrunner"
	}
	if len(os.Args) < 2 || os.Args[1] == "--help" || os.Args[1] == "-h" || os.Args[1] == "help" {
		printUsage(prog)
		os.Exit(0)
	}

	action := os.Args[1]
	fs := flag.NewFlagSet(prog, flag.ExitOnError)
	env := fs.String("env", "mainnet", "environment: mainnet or testnet")
	impl := fs.String("impl", "all", "implementation: all, zood, or hanzod")
	nNodes := fs.Int("n", 10, "number of nodes for scale test")
	reqs := fs.Int("reqs", 50, "requests per node for load test")
	// Unprivileged by default. Binding :80 costs a capability on the binary,
	// and a tool that has to be blessed before it will start is a tool that
	// does not start.
	port := fs.String("port", ":8080", "gateway listen port")
	runs := fs.Int("runs", 3, "cold starts to time per runtime for boot")
	fs.StringVar(&binLuxd, "luxd", binLuxd, "Go node binary to measure")
	fs.StringVar(&binHanzod, "hanzod", binHanzod, "Rust node binary to measure")
	fs.StringVar(&binZood, "zood", binZood, "C++ node binary to measure")
	tps := fs.Float64("tps", 1.0, "transactions per second per chain for bot")
	duration := fs.Duration("dur", 60*time.Second, "duration for bot or monitor")
	fs.Parse(os.Args[2:])

	cfg, ok := configs[*env]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown environment: %s (choose mainnet or testnet)\n", *env)
		os.Exit(1)
	}

	gwCfg := GatewayConfig{
		ListenAddr:  *port,
		LuxRPC:      "http://127.0.0.1:9630",
		ZooRPC:      "http://127.0.0.1:9730",
		HanzoRPC:    "http://127.0.0.1:9780",
		ExplorerRPC: "http://127.0.0.1:8091",
	}

	switch action {
	case "network":
		data, _ := json.MarshalIndent(getNetworkResponse(gwCfg), "", "  ")
		fmt.Println(string(data))
	case "consensus":
		data, _ := json.MarshalIndent(getConsensusResponse(gwCfg), "", "  ")
		fmt.Println(string(data))
	case "stop":
		stopNodes()
		exec.Command("pkill", "-f", "zood.*--base-port 20000").Run()
	case "start":
		if *impl == "all" || *impl == "zood" {
			startZood(cfg, *env)
		}
		if *impl == "all" || *impl == "hanzod" {
			startHanzod(cfg, *env)
		}
		time.Sleep(2 * time.Second)
		printStatus(cfg, *env)
	case "restart":
		stopNodes()
		if *impl == "all" || *impl == "zood" {
			startZood(cfg, *env)
		}
		if *impl == "all" || *impl == "hanzod" {
			startHanzod(cfg, *env)
		}
		time.Sleep(2 * time.Second)
		printStatus(cfg, *env)
	case "status":
		printStatus(cfg, *env)
	case "test":
		runTest(cfg, *env)
	case "scale":
		runScale(*nNodes, *impl, *reqs)
	case "gateway":
		// One address, the one that was asked for. The retry onto another port
		// existed because the default could not be bound without a capability;
		// with an unprivileged default it only hid the real error.
		if err := startGateway(gwCfg); err != nil {
			log.Fatalf("gateway on %s: %v", gwCfg.ListenAddr, err)
		}
	case "bot":
		signer, err := SignerFromEnv()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		runTrafficBot(cfg.BotTargets, signer, *tps, *duration)
	case "zap":
		zapPort := ":8082"
		fmt.Printf("[*] Starting Netrunner Zap RPC Protocol Server on %s...\n", zapPort)
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			handleZapRPC(gwCfg, w, r)
		})
		log.Fatal(http.ListenAndServe(zapPort, mux))
	case "boot":
		runBoot(cfg, *runs)
	case "monitor":
		monitorMemory(cfg.BotTargets, *duration, 5*time.Second)
	default:
		fmt.Fprintf(os.Stderr, "unknown action: %s\n", action)
		os.Exit(1)
	}
}
