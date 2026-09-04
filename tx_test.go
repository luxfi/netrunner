package main

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"
)


func TestComputeContractAddress(t *testing.T) {

	addr := computeContractAddress(TreasuryAddress, 0)
	t.Logf("Computed contract address for nonce 0: %s", addr)
	if len(addr) != 42 {
		t.Fatalf("unexpected addr len: %s", addr)
	}
}

func TestSignTx(t *testing.T) {
	raw, err := signEIP155Tx(TreasuryPrivateKey, 36963, 0, TreasuryAddress, big.NewInt(1000), 21000, big.NewInt(25000000000), nil)
	if err != nil {
		t.Fatalf("sign failed: %v", err)
	}
	t.Logf("Signed raw tx: %s", raw)
	if len(raw) < 10 {
		t.Fatalf("raw tx too short")
	}
}

func TestEVMContractLifecycle(t *testing.T) {
	// Let's test full EVM contract deployment on Hanzo EVM first
	rpcURL := "http://127.0.0.1:9780/v1/chain/C/rpc"
	chainID := int64(36963)

	nonceRes, err := httpPost(rpcURL, "eth_getTransactionCount", []any{TreasuryAddress, "latest"})
	if err != nil || nonceRes["result"] == nil {
		t.Fatalf("get nonce failed: %v", err)
	}
	var nonce uint64
	fmt.Sscanf(fmt.Sprintf("%v", nonceRes["result"]), "0x%x", &nonce)
	t.Logf("Initial Nonce: %d", nonce)

	deployBytecode, _ := hex.DecodeString(sampleContractBytecode)
	contractAddr := computeContractAddress(TreasuryAddress, nonce)
	t.Logf("Expected Contract Address: %s", contractAddr)

	// 1. Deploy Contract
	gasPrice := big.NewInt(50000000000)
	rawDeploy, err := signEIP155Tx(TreasuryPrivateKey, chainID, nonce, "", big.NewInt(0), 100000, gasPrice, deployBytecode)
	if err != nil {
		t.Fatalf("sign deploy err: %v", err)
	}
	deployRes, err := httpPost(rpcURL, "eth_sendRawTransaction", []any{rawDeploy})
	t.Logf("Deploy Response: %v, err: %v", deployRes, err)
	if err != nil || deployRes["result"] == nil {
		t.Fatalf("deploy failed")
	}
	deployTxHash := fmt.Sprintf("%v", deployRes["result"])
	t.Logf("Deploy TxHash: %s", deployTxHash)

	// Check code at contract address
	codeRes, err := httpPost(rpcURL, "eth_getCode", []any{contractAddr, "latest"})
	t.Logf("Contract Code: %v", codeRes)

	// 2. State Mutation: call set(42) [selector 0x60fe47b1]
	nonce++
	setDataHex := "60fe47b1000000000000000000000000000000000000000000000000000000000000002a"
	setData, _ := hex.DecodeString(setDataHex)
	rawSet, err := signEIP155Tx(TreasuryPrivateKey, chainID, nonce, contractAddr, big.NewInt(0), 100000, gasPrice, setData)
	if err != nil {
		t.Fatalf("sign set err: %v", err)
	}
	setRes, err := httpPost(rpcURL, "eth_sendRawTransaction", []any{rawSet})
	t.Logf("Set Response: %v", setRes)

	// 3. State Query: eth_getStorageAt slot 0
	storageRes, err := httpPost(rpcURL, "eth_getStorageAt", []any{contractAddr, "0x0", "latest"})
	t.Logf("Storage slot 0: %v", storageRes)

	// 4. eth_call: get() [selector 0x6d4ce63c]
	callRes, err := httpPost(rpcURL, "eth_call", []any{
		map[string]any{"to": contractAddr, "data": "0x6d4ce63c"},
		"latest",
	})
	t.Logf("eth_call get(): %v", callRes)

	// 5. Destructuring: destroy() [selector 0x83197ef0]
	nonce++
	destroyData, _ := hex.DecodeString("83197ef0")
	rawDestroy, err := signEIP155Tx(TreasuryPrivateKey, chainID, nonce, contractAddr, big.NewInt(0), 100000, gasPrice, destroyData)
	if err != nil {
		t.Fatalf("sign destroy err: %v", err)
	}
	destroyRes, err := httpPost(rpcURL, "eth_sendRawTransaction", []any{rawDestroy})
	t.Logf("Destroy Response: %v", destroyRes)

	// Receipt query
	receiptRes, _ := httpPost(rpcURL, "eth_getTransactionReceipt", []any{deployTxHash})
	t.Logf("Deploy Receipt: %v", receiptRes)
}







