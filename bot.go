// SPDX-License-Identifier: BSD-3-Clause-Eco
package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ChainBotTarget is one chain the bot works against: where to send, and how to
// find the process serving it so its memory can be watched under the load.
type ChainBotTarget struct {
	Name    string
	RPC     string
	Coin    string
	ChainID int64
	Proc    string // binary name, for the memory sample
	Port    int    // the port in that process's command line
}

// kind is the sort of EVM work one transaction does.
type kind int

const (
	transfer kind = iota
	deploy
	call
)

func (k kind) String() string {
	switch k {
	case deploy:
		return "deploy"
	case call:
		return "call"
	default:
		return "transfer"
	}
}

// sent is a transaction the chain accepted but has not yet been seen in a block.
type sent struct {
	kind kind
	at   time.Time
}

// tally is one chain's answer: what was submitted, and what actually landed.
type tally struct {
	mu sync.Mutex

	chainID                 int64
	heightBefore            uint64
	heightAfter             uint64
	submitted               int
	landed                  int
	reverted                int
	landedBy                map[kind]int
	rejects                 map[string]int // submission errors, by reason
	outstanding             map[string]sent
	blocker                 string
	balance                 *big.Int
	memory                  []float64
	firstLandedBlock, lastB uint64
}

func newTally() *tally {
	return &tally{
		landedBy:    map[kind]int{},
		rejects:     map[string]int{},
		outstanding: map[string]sent{},
	}
}

func randomAddress() string {
	b := make([]byte, 20)
	rand.Read(b)
	return "0x" + hex.EncodeToString(b)
}

func hexUint(v any) uint64 {
	s, ok := v.(string)
	if !ok {
		return 0
	}
	n, _ := strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 64)
	return n
}

// rss reads the resident set of the process serving a chain, in MB. It matches
// on the port in the command line, because several builds of the same binary
// run side by side and only the port tells them apart.
func rss(proc string, port int) float64 {
	out, err := exec.Command("sh", "-c",
		fmt.Sprintf("ps -eo rss,cmd | grep -F %s | grep -F -- %d | grep -v grep | awk '{s+=$1} END {print s}'", proc, port)).Output()
	if err != nil {
		return 0
	}
	kb, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	return kb / 1024.0
}

// rejectReason collapses a node's error text to a short, groupable phrase, so
// a thousand failures read as one line rather than a thousand.
func rejectReason(msg string) string {
	m := strings.ToLower(msg)
	for _, known := range []string{
		"insufficient funds", "nonce too low", "nonce too high", "already known",
		"replacement transaction underpriced", "transaction underpriced",
		"intrinsic gas too low", "gas limit", "exceeds block gas limit",
		"invalid sender", "not supported", "method not found", "no such chain",
		"mempool full", "future nonce", "txpool is full",
	} {
		if strings.Contains(m, known) {
			return known
		}
	}
	if len(msg) > 60 {
		msg = msg[:60]
	}
	return msg
}

func runTrafficBot(targets []ChainBotTarget, s *Signer, tps float64, duration time.Duration) {
	fmt.Println("================================================================================")
	fmt.Printf("  MULTI-CHAIN EVM TRAFFIC — %.1f tx/s per chain for %v\n", tps, duration)
	fmt.Printf("  Sender: %s\n", s.Address)
	fmt.Println("  Work per chain: value transfer, contract deploy, contract call")
	fmt.Println("================================================================================")

	interval := time.Duration(float64(time.Second) / tps)
	stop := make(chan struct{})
	time.AfterFunc(duration, func() { close(stop) })

	results := make([]*tally, len(targets))
	var wg sync.WaitGroup
	for i, t := range targets {
		results[i] = newTally()
		wg.Add(1)
		go func(t ChainBotTarget, r *tally) {
			defer wg.Done()
			work(t, s, r, interval, stop)
		}(t, results[i])
	}

	// Progress, and the memory series the run is meant to expose.
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(10 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				var line []string
				for i, t := range targets {
					r := results[i]
					r.mu.Lock()
					mb := rss(t.Proc, t.Port)
					r.memory = append(r.memory, mb)
					line = append(line, fmt.Sprintf("%s %d/%d sent/landed %.0fMB", t.Name, r.submitted, r.landed, mb))
					r.mu.Unlock()
				}
				fmt.Printf("[%s] %s\n", time.Now().UTC().Format("15:04:05"), strings.Join(line, " | "))
			}
		}
	}()

	wg.Wait()
	<-done
	report(targets, results)
}

// work drives one chain: submit on a fixed cadence, and reap receipts as they
// appear. The nonce is kept locally because a chain that has not yet mined the
// last transaction still reports the old count.
func work(t ChainBotTarget, s *Signer, r *tally, interval time.Duration, stop chan struct{}) {
	cid, err := httpPost(t.RPC, "eth_chainId", []any{})
	if err != nil || cid["result"] == nil {
		r.blocker = fmt.Sprintf("no chainId from %s: %v", t.RPC, err)
		return
	}
	r.chainID = int64(hexUint(cid["result"]))

	head, _ := httpPost(t.RPC, "eth_blockNumber", []any{})
	r.heightBefore = hexUint(head["result"])

	bal, _ := httpPost(t.RPC, "eth_getBalance", []any{s.Address, "latest"})
	r.balance = new(big.Int)
	if bal != nil && bal["result"] != nil {
		hexStr := strings.TrimPrefix(fmt.Sprintf("%v", bal["result"]), "0x")
		r.balance.SetString(hexStr, 16)
	}

	gasPrice := currentGasPrice(t.RPC)
	if r.balance.Sign() == 0 && gasPrice.Sign() > 0 {
		r.blocker = "sender holds no balance on this chain and gas is priced above zero — nothing can land"
	}

	nonceRes, _ := httpPost(t.RPC, "eth_getTransactionCount", []any{s.Address, "pending"})
	nonce := hexUint(nonceRes["result"])

	reap := make(chan struct{})
	go reapReceipts(t, r, stop, reap)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	step := 0
	var contract string

	for {
		select {
		case <-stop:
			<-reap
			finish(t, r)
			return
		case <-ticker.C:
			step++
			if step%20 == 0 {
				gasPrice = currentGasPrice(t.RPC)
			}

			k := kind(step % 3)
			if k == call && contract == "" {
				k = transfer
			}
			var to string
			var value *big.Int
			var gas uint64
			var data []byte
			switch k {
			case deploy:
				to, value, gas = "", big.NewInt(0), 150000
				data, _ = hex.DecodeString(sampleContractBytecode)
			case call:
				to, value, gas = contract, big.NewInt(0), 100000
				data, _ = hex.DecodeString(fmt.Sprintf("60fe47b1%064x", step))
			default:
				to, value, gas = randomAddress(), big.NewInt(int64(1000+step%97)), 21000
			}

			raw, err := signEIP155Tx(s, r.chainID, nonce, to, value, gas, gasPrice, data)
			if err != nil {
				r.mu.Lock()
				r.rejects["sign: "+err.Error()]++
				r.mu.Unlock()
				continue
			}

			res, err := httpPost(t.RPC, "eth_sendRawTransaction", []any{raw})
			r.mu.Lock()
			r.submitted++
			switch {
			case err != nil:
				r.rejects[rejectReason(err.Error())]++
			case res["error"] != nil:
				msg := fmt.Sprintf("%v", res["error"])
				if m, ok := res["error"].(map[string]any); ok {
					msg = fmt.Sprintf("%v", m["message"])
				}
				reason := rejectReason(msg)
				r.rejects[reason]++
				// A rejected nonce means the local count and the chain's have
				// parted; take the chain's answer rather than wedging here.
				if strings.HasPrefix(reason, "nonce") || reason == "already known" {
					r.mu.Unlock()
					if n, _ := httpPost(t.RPC, "eth_getTransactionCount", []any{s.Address, "pending"}); n["result"] != nil {
						nonce = hexUint(n["result"])
					}
					continue
				}
			case res["result"] != nil:
				h := fmt.Sprintf("%v", res["result"])
				r.outstanding[h] = sent{kind: k, at: time.Now()}
				if k == deploy {
					contract = computeContractAddress(s.Address, nonce)
				}
				nonce++
			default:
				r.rejects["empty response"]++
			}
			r.mu.Unlock()
		}
	}
}

// reapReceipts asks the chain which of the accepted transactions are in a
// block. Submission is not landing: only a receipt says a transaction is real.
func reapReceipts(t ChainBotTarget, r *tally, stop chan struct{}, done chan struct{}) {
	defer close(done)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			// A last pass, so transactions accepted at the buzzer get a fair
			// chance to be mined before they are called failures.
			deadline := time.Now().Add(30 * time.Second)
			for time.Now().Before(deadline) {
				if sweep(t, r) == 0 {
					return
				}
				time.Sleep(2 * time.Second)
			}
			return
		case <-tick.C:
			sweep(t, r)
		}
	}
}

// sweep checks every outstanding hash once and returns how many are still open.
func sweep(t ChainBotTarget, r *tally) int {
	r.mu.Lock()
	pending := make([]string, 0, len(r.outstanding))
	for h := range r.outstanding {
		pending = append(pending, h)
	}
	r.mu.Unlock()

	for _, h := range pending {
		res, err := httpPost(t.RPC, "eth_getTransactionReceipt", []any{h})
		if err != nil || res == nil || res["result"] == nil {
			continue
		}
		rcpt, ok := res["result"].(map[string]any)
		if !ok {
			continue
		}
		r.mu.Lock()
		s := r.outstanding[h]
		delete(r.outstanding, h)
		blk := hexUint(rcpt["blockNumber"])
		if hexUint(rcpt["status"]) == 1 {
			r.landed++
			r.landedBy[s.kind]++
		} else {
			r.reverted++
		}
		if r.firstLandedBlock == 0 || blk < r.firstLandedBlock {
			r.firstLandedBlock = blk
		}
		if blk > r.lastB {
			r.lastB = blk
		}
		r.mu.Unlock()
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.outstanding)
}

func finish(t ChainBotTarget, r *tally) {
	head, _ := httpPost(t.RPC, "eth_blockNumber", []any{})
	r.mu.Lock()
	r.heightAfter = hexUint(head["result"])
	r.memory = append(r.memory, rss(t.Proc, t.Port))
	r.mu.Unlock()
}

// currentGasPrice takes the chain's own price and adds headroom, except on a
// chain that prices gas at zero, where zero is the right answer.
func currentGasPrice(rpc string) *big.Int {
	res, err := httpPost(rpc, "eth_gasPrice", []any{})
	if err != nil || res["result"] == nil {
		return big.NewInt(25000000000)
	}
	p := new(big.Int).SetUint64(hexUint(res["result"]))
	if p.Sign() == 0 {
		return p
	}
	return p.Mul(p, big.NewInt(2))
}

func report(targets []ChainBotTarget, results []*tally) {
	fmt.Println("\n================================================================================")
	fmt.Println("  RESULT — submitted is a claim, landed is a fact")
	fmt.Println("================================================================================")
	for i, t := range targets {
		r := results[i]
		r.mu.Lock()
		fmt.Printf("\n%s  (chainId %d, %s)\n", t.Name, r.chainID, t.RPC)
		fmt.Printf("  height           %d -> %d  (+%d)\n", r.heightBefore, r.heightAfter, r.heightAfter-r.heightBefore)
		fmt.Printf("  submitted        %d\n", r.submitted)
		fmt.Printf("  landed           %d   (transfer %d, deploy %d, call %d)\n",
			r.landed, r.landedBy[transfer], r.landedBy[deploy], r.landedBy[call])
		fmt.Printf("  reverted         %d\n", r.reverted)
		fmt.Printf("  never mined      %d\n", len(r.outstanding))
		if r.landed > 0 {
			fmt.Printf("  landed in blocks %d..%d\n", r.firstLandedBlock, r.lastB)
		}
		if len(r.rejects) > 0 {
			reasons := make([]string, 0, len(r.rejects))
			for k := range r.rejects {
				reasons = append(reasons, k)
			}
			sort.Slice(reasons, func(a, b int) bool { return r.rejects[reasons[a]] > r.rejects[reasons[b]] })
			fmt.Println("  refused at submission:")
			for _, k := range reasons {
				fmt.Printf("    %-42s %d\n", k, r.rejects[k])
			}
		}
		if len(r.memory) > 0 {
			first, last := r.memory[0], r.memory[len(r.memory)-1]
			peak := first
			for _, m := range r.memory {
				if m > peak {
					peak = m
				}
			}
			fmt.Printf("  %s RSS         %.0f -> %.0f MB (peak %.0f, %d samples)\n", t.Proc, first, last, peak, len(r.memory))
		}
		if r.blocker != "" {
			fmt.Printf("  BLOCKER          %s\n", r.blocker)
		}
		r.mu.Unlock()
	}
	fmt.Println("\n================================================================================")
}

// monitorMemory samples the same processes the bot loads, on its own, so a run
// can be watched from a second terminal.
func monitorMemory(targets []ChainBotTarget, duration, interval time.Duration) {
	fmt.Printf("  RSS every %v for %v\n", interval, duration)
	labels := make([]string, len(targets))
	first := make([]float64, len(targets))
	for i, t := range targets {
		labels[i] = t.Name
	}
	fmt.Printf("%-10s", "elapsed")
	for _, l := range labels {
		fmt.Printf("%-22s", l)
	}
	fmt.Println()

	start := time.Now()
	for n := 0; ; n++ {
		fmt.Printf("%-10.0fs", time.Since(start).Seconds())
		for i, t := range targets {
			mb := rss(t.Proc, t.Port)
			if n == 0 {
				first[i] = mb
			}
			fmt.Printf("%-22s", fmt.Sprintf("%.0f MB (%+.0f)", mb, mb-first[i]))
		}
		fmt.Println()
		if time.Since(start) >= duration {
			return
		}
		time.Sleep(interval)
	}
}
