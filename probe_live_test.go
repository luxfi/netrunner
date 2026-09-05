// SPDX-License-Identifier: BSD-3-Clause-Eco
package main

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"
)

// TestLiveLanding asks one endpoint the only question that matters: does a
// transaction sent here end up in a block? Set PROBE_RPC to run it.
func TestLiveLanding(t *testing.T) {
	rpc := os.Getenv("PROBE_RPC")
	if rpc == "" {
		t.Skip("set PROBE_RPC to probe a live endpoint")
	}
	s, err := SignerFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	cid, _ := httpPost(rpc, "eth_chainId", []any{})
	chainID := int64(hexUint(cid["result"]))
	head, _ := httpPost(rpc, "eth_blockNumber", []any{})
	n, _ := httpPost(rpc, "eth_getTransactionCount", []any{s.Address, "pending"})
	nonce := hexUint(n["result"])
	bal, _ := httpPost(rpc, "eth_getBalance", []any{s.Address, "latest"})
	t.Logf("chainId %d head %d nonce %d balance %v", chainID, hexUint(head["result"]), nonce, bal["result"])

	gp := currentGasPrice(rpc)
	var hashes []string
	for i := 0; i < 3; i++ {
		var raw string
		switch i {
		case 1:
			code, _ := hex.DecodeString(sampleContractBytecode)
			raw, err = signEIP155Tx(s, chainID, nonce, "", big.NewInt(0), 150000, gp, code)
		default:
			raw, err = signEIP155Tx(s, chainID, nonce, randomAddress(), big.NewInt(1000), 21000, gp, nil)
		}
		if err != nil {
			t.Fatal(err)
		}
		res, err := httpPost(rpc, "eth_sendRawTransaction", []any{raw})
		t.Logf("submit %d -> %v err=%v", i, res, err)
		if res != nil && res["result"] != nil {
			hashes = append(hashes, fmt.Sprintf("%v", res["result"]))
			nonce++
		}
	}
	for round := 0; round < 15; round++ {
		time.Sleep(2 * time.Second)
		landed := 0
		for _, h := range hashes {
			r, _ := httpPost(rpc, "eth_getTransactionReceipt", []any{h})
			if r != nil && r["result"] != nil {
				landed++
			}
		}
		head, _ = httpPost(rpc, "eth_blockNumber", []any{})
		t.Logf("t+%2ds head %d landed %d/%d", (round+1)*2, hexUint(head["result"]), landed, len(hashes))
		if landed == len(hashes) && landed > 0 {
			return
		}
	}
	t.Log("not all submitted transactions reached a block")
}
