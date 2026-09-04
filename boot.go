package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"syscall"
	"time"
)

// How long a runtime is given to get to a served call before the run is judged
// a failure rather than a slow success.
const bootDeadline = 90 * time.Second

// bootTarget is one runtime, and how to start a single node of it in isolation.
//
// Isolation is the point: boot is measured on ports and directories nothing
// else uses, so a measurement never disturbs a cluster that is already running
// and a cluster already running never flatters a measurement.
type bootTarget struct {
	name string
	bin  string
	rpc  int
	// args builds the command line for a fresh node in dir, answering on rpc.
	args func(dir string, rpc int) []string
	// prepare does whatever has to exist before the node is started. Its cost
	// is NOT counted: it is the operator's work, not the node's.
	prepare func(dir string) error
}

// bootTime is what one cold start cost.
type bootTime struct {
	listen time.Duration // exec -> the RPC port accepts a connection
	served time.Duration // exec -> a call is answered
}

// timeBoot starts one node and measures it, from exec to a served eth_chainId.
//
// The two numbers are taken from ONE process on ONE clock: listening is when
// the socket first accepts, served is when a call first comes back with a
// result. The gap between them is the node's own work — a port that is open
// before the chain is is not a node that is up, and reporting only the first
// number is how a runtime comes to look fast.
func timeBoot(t bootTarget, dir string) (bootTime, error) {
	var out bootTime
	if err := os.RemoveAll(dir); err != nil {
		return out, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return out, err
	}
	if t.prepare != nil {
		if err := t.prepare(dir); err != nil {
			return out, fmt.Errorf("prepare: %w", err)
		}
	}

	logFile, err := os.Create(filepath.Join(dir, "boot.log"))
	if err != nil {
		return out, err
	}
	defer logFile.Close()

	cmd := exec.Command(t.bin, t.args(dir, t.rpc)...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	// Its own process group, so the node is killed without reaching anything
	// that was already running.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return out, err
	}
	defer func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
	}()

	addr := fmt.Sprintf("127.0.0.1:%d", t.rpc)
	deadline := start.Add(bootDeadline)
	for time.Now().Before(deadline) {
		if out.listen == 0 {
			conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
			if err != nil {
				time.Sleep(2 * time.Millisecond)
				continue
			}
			conn.Close()
			out.listen = time.Since(start)
		}
		// The useful number: not a port, an answer.
		res, err := httpPost("http://"+addr+"/v1/chain/c", "eth_chainId", []any{})
		if err == nil && res != nil && res["result"] != nil {
			out.served = time.Since(start)
			return out, nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return out, fmt.Errorf("no answer within %s", bootDeadline)
}

// runBoot measures cold start for every runtime and prints one table.
func runBoot(cfg Config, runs int) {
	dir := filepath.Join(os.TempDir(), "cluster_boot")
	archive := cfg.ROArchive

	targets := []bootTarget{
		{
			name: "zood (C++)",
			bin:  binZood,
			rpc:  21730,
			args: func(_ string, rpc int) []string {
				return []string{
					"--index", "0", "--n", "1",
					"--base-port", "21731",
					"--rpc-port", fmt.Sprintf("%d", rpc),
					"--deadline-ms", "10000",
					"--chain-id", fmt.Sprintf("%d", cfg.ChainID),
					"--archive-rpc", archive,
					"--light",
				}
			},
		},
		{
			name: "hanzod (Rust)",
			bin:  binHanzod,
			rpc:  21780,
			prepare: func(dir string) error {
				// A committee of one: its own key, published into a file the
				// node then reads. Not part of the measurement.
				data := filepath.Join(dir, "n0")
				if err := os.MkdirAll(data, 0o755); err != nil {
					return err
				}
				args := []string{"--data", data, "--publish"}
				if _, err := os.Stat(hanzoGen); err == nil {
					args = append(args, "--genesis", hanzoGen)
				}
				key, err := exec.Command(binHanzod, args...).Output()
				if err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(dir, "committee.txt"), key, 0o644)
			},
			args: func(dir string, rpc int) []string {
				args := []string{
					"--data", filepath.Join(dir, "n0"),
					"--committee", filepath.Join(dir, "committee.txt"),
					"--rpc", fmt.Sprintf("127.0.0.1:%d", rpc),
					"--stake", "127.0.0.1:21781",
					"--peers", "127.0.0.1:21781",
					"--archive-rpc", archive,
					"--light",
				}
				if _, err := os.Stat(hanzoGen); err == nil {
					return append(args, "--genesis", hanzoGen)
				}
				return append(args, "--network", "mainnet")
			},
		},
		{
			name: "luxd (Go)",
			bin:  binLuxd,
			rpc:  21630,
			prepare: func(dir string) error {
				// The post-quantum staking keys, in place before the clock
				// starts. Generating them is the operator's cost, not the
				// node's, and without them luxd refuses to construct at all.
				if _, err := os.Stat(luxStaking); err != nil {
					return nil
				}
				return exec.Command("cp", "-r", luxStaking, filepath.Join(dir, "staking")).Run()
			},
			args: func(dir string, rpc int) []string {
				args := []string{
					"--network-id=1337",
					"--data-dir=" + dir,
					"--db-type=zapdb",
					fmt.Sprintf("--http-port=%d", rpc),
					fmt.Sprintf("--staking-port=%d", rpc+1),
					"--sybil-protection-enabled=false",
					"--bootstrap-ips=",
					"--bootstrap-ids=",
				}
				if _, err := os.Stat(luxGen); err == nil {
					args = append(args, "--genesis-file="+luxGen)
				}
				if plugins := filepath.Join(filepath.Dir(luxStaking), "plugins"); dirExists(plugins) {
					args = append(args, "--plugin-dir="+plugins)
				}
				return args
			},
		},
	}

	fmt.Printf("\n  COLD START — exec to first served eth_chainId (%d runs each)\n", runs)
	fmt.Printf("  Ports 216xx/217xx and %s, untouched by any running cluster.\n\n", dir)
	fmt.Printf("  %-16s %12s %12s %12s\n", "runtime", "listening", "served", "chain work")
	fmt.Printf("  %-16s %12s %12s %12s\n", "----------------", "------------", "------------", "------------")

	for _, t := range targets {
		if _, err := os.Stat(t.bin); err != nil {
			fmt.Printf("  %-16s %12s  %s\n", t.name, "-", "not built: "+t.bin)
			continue
		}
		var listen, served []time.Duration
		var lastErr error
		for i := 0; i < runs; i++ {
			got, err := timeBoot(t, filepath.Join(dir, t.name))
			if err != nil {
				lastErr = err
				continue
			}
			listen = append(listen, got.listen)
			served = append(served, got.served)
		}
		if len(served) == 0 {
			fmt.Printf("  %-16s %12s  %v\n", t.name, "-", lastErr)
			continue
		}
		l, s := median(listen), median(served)
		fmt.Printf("  %-16s %12s %12s %12s\n", t.name, round(l), round(s), round(s-l))
	}
	fmt.Printf("\n  listening = the RPC port accepts. served = a call is answered.\n")
	fmt.Printf("  chain work = the difference, which is the number an open port hides.\n\n")
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func median(d []time.Duration) time.Duration {
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	return d[len(d)/2]
}

func round(d time.Duration) string {
	return d.Round(time.Millisecond).String()
}
