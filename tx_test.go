// SPDX-License-Identifier: BSD-3-Clause-Eco
package main

import (
	"math/big"
	"strings"
	"testing"
)

// The published BIP-39 test phrase and the address it yields at the Ethereum
// account path. Checking against a value the whole world agrees on is what
// makes the derivation trustworthy without ever naming a real account.
const (
	testPhrase  = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	testAddress = "0x9858effd232b4033e47d90003d41ec34ecaeda94"
)

func testSigner(t *testing.T) *Signer {
	t.Helper()
	t.Setenv("LUX_MNEMONIC", testPhrase)
	s, err := SignerFromEnv()
	if err != nil {
		t.Fatalf("SignerFromEnv: %v", err)
	}
	return s
}

func TestSignerDerivesTheStandardAddress(t *testing.T) {
	s := testSigner(t)
	if !strings.EqualFold(s.Address, testAddress) {
		t.Fatalf("m/44'/60'/0'/0/0 gave %s, want %s", s.Address, testAddress)
	}
}

func TestSignerRefusesAnAbsentPhrase(t *testing.T) {
	t.Setenv("LUX_MNEMONIC", "")
	if _, err := SignerFromEnv(); err == nil {
		t.Fatal("an absent LUX_MNEMONIC must be an error, not a default account")
	}
	t.Setenv("LUX_MNEMONIC", "only three words")
	if _, err := SignerFromEnv(); err == nil {
		t.Fatal("a phrase of the wrong length must be an error")
	}
}

func TestComputeContractAddress(t *testing.T) {
	addr := computeContractAddress(testAddress, 0)
	if len(addr) != 42 || !strings.HasPrefix(addr, "0x") {
		t.Fatalf("unexpected contract address: %s", addr)
	}
}

func TestSignTx(t *testing.T) {
	s := testSigner(t)
	raw, err := signEIP155Tx(s, 96368, 0, testAddress, big.NewInt(1000), 21000, big.NewInt(25000000000), nil)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	// A signed legacy transfer is an RLP list of nine items; anything much
	// shorter than that is not a transaction.
	if !strings.HasPrefix(raw, "0xf8") || len(raw) < 200 {
		t.Fatalf("signed transaction looks wrong: %s", raw)
	}
}

func TestRejectReasonGroups(t *testing.T) {
	if got := rejectReason("err: insufficient funds for gas * price + value"); got != "insufficient funds" {
		t.Fatalf("got %q", got)
	}
	if got := rejectReason("nonce too low: address 0x..., tx: 3 state: 5"); got != "nonce too low" {
		t.Fatalf("got %q", got)
	}
}
