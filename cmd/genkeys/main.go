// genkeys generates mainnet validator keys from a BIP-39 mnemonic.
//
// Mnemonic source (priority order, handled inside the shared luxfi/kms
// keys.LoadMnemonic loader so every Lux-derived tool resolves keys
// the same way):
//
//	1. MNEMONIC env var                 — local dev + CI test seam
//	2. KMS_ADDR + KMS_ENV +             — native ZAP from Liquid KMS
//	   KMS_MNEMONIC_PATH
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/luxfi/keys"
)

type ValidatorBackup struct {
	Index        int    `json:"index"`
	Name         string `json:"name"`
	NodeID       string `json:"nodeID"`
	PChainAddr   string `json:"pChainAddr"`
	CChainAddr   string `json:"cChainAddr"`
	BLSPublicKey string `json:"blsPublicKey"`
	BLSPoP       string `json:"blsPoP"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	identity, err := bootstrapIdentity("netrunner/genkeys")
	if err != nil {
		fmt.Printf("ERROR: derive KMS dial identity: %v\n", err)
		fmt.Println("Set MNEMONIC or KMS_BOOTSTRAP_MNEMONIC for consensus-auth dial.")
		os.Exit(1)
	}
	defer identity.Wipe()
	mnemonic, err := keys.LoadMnemonic(ctx,
		os.Getenv("KMS_ADDR"),
		os.Getenv("KMS_ENV"),
		envOr("KMS_MNEMONIC_PATH", "/mnemonic"),
		identity)
	if err != nil {
		fmt.Printf("ERROR: load mnemonic: %v\n", err)
		fmt.Println("Set MNEMONIC env var, or KMS_ADDR + KMS_ENV + KMS_MNEMONIC_PATH for native ZAP.")
		os.Exit(1)
	}

	// Use KEYS_DIR if set, otherwise default
	keysDir := os.Getenv("KEYS_DIR")
	if keysDir == "" {
		home, _ := os.UserHomeDir()
		keysDir = filepath.Join(home, ".lux", "keys")
	}

	fmt.Printf("=== Generating 5 Mainnet Validators from MNEMONIC ===\n")
	fmt.Printf("Keys Directory: %s\n\n", keysDir)

	ks := keys.NewKeyStore(keysDir)

	// Generate 5 validators
	validators, err := keys.DeriveValidatorsFromMnemonic(mnemonic, 5)
	if err != nil {
		fmt.Printf("ERROR: Failed to derive validators: %v\n", err)
		os.Exit(1)
	}

	backups := make([]ValidatorBackup, 5)

	for i, vk := range validators {
		name := fmt.Sprintf("node%d", i)

		fmt.Printf("--- Validator %d (%s) ---\n", i, name)
		fmt.Printf("  NodeID:       %s\n", vk.NodeID.String())
		fmt.Printf("  P-Chain Addr: %s\n", vk.PChainAddr.String())
		fmt.Printf("  C-Chain Addr: %s\n", vk.CChainAddrHex())
		fmt.Printf("  BLS PubKey:   %s\n", vk.BLSPublicKeyHex())
		fmt.Printf("  BLS PoP:      %s\n", vk.BLSPoPHex())

		// Save to disk
		if err := ks.Save(name, vk); err != nil {
			fmt.Printf("  ERROR saving: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("  Saved to:     %s/%s/\n\n", keysDir, name)

		// Build backup struct
		backups[i] = ValidatorBackup{
			Index:        i,
			Name:         name,
			NodeID:       vk.NodeID.String(),
			PChainAddr:   vk.PChainAddr.String(),
			CChainAddr:   vk.CChainAddrHex(),
			BLSPublicKey: vk.BLSPublicKeyHex(),
			BLSPoP:       vk.BLSPoPHex(),
		}
	}

	// Write backup JSON
	backupJSON, _ := json.MarshalIndent(backups, "", "  ")
	backupPath := filepath.Join(keysDir, "mainnet_validators.json")
	if err := os.WriteFile(backupPath, backupJSON, 0600); err != nil {
		fmt.Printf("ERROR: Failed to write backup file: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("=== Backup Summary Written ===\n")
	fmt.Printf("  File: %s\n\n", backupPath)

	// Show EC private keys (SENSITIVE - for secure backup only)
	fmt.Println("=== EC Private Keys (SECURE BACKUP ONLY) ===")
	for i, vk := range validators {
		fmt.Printf("  node%d: %s\n", i, hex.EncodeToString(vk.ECPrivateKey))
	}

	fmt.Println("\n=== Key Files to Backup ===")
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("node%d", i)
		dir := filepath.Join(keysDir, name)
		fmt.Printf("  %s/\n", dir)
		fmt.Printf("    staking/staker.key  - TLS private key (for NodeID)\n")
		fmt.Printf("    staking/staker.crt  - TLS certificate\n")
		fmt.Printf("    bls/signer.key      - BLS signing key\n")
		fmt.Printf("    ec/private.key      - EC private key (for P/C chain)\n")
	}

	fmt.Println("\n=== Genesis Initial Stakers (copy to genesis.json) ===")
	for i, vk := range validators {
		fmt.Printf(`  {
    "nodeID": "%s",
    "rewardAddress": "X-lux1...",
    "delegationFee": 20000,
    "weight": 1000000000000000,
    "signer": {
      "publicKey": "%s",
      "proofOfPossession": "%s"
    }
  }%s
`, vk.NodeID.String(), vk.BLSPublicKeyHex(), vk.BLSPoPHex(), func() string {
			if i < 4 {
				return ","
			} else {
				return ""
			}
		}())
	}
}

// envOr returns the value of env var `name` if set + non-empty, else def.
func envOr(name, def string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return def
}

// bootstrapIdentity derives the *keys.ServiceIdentity used to sign the
// KMS dial envelope. Bootstrap mnemonic source order:
//   1. KMS_BOOTSTRAP_MNEMONIC env var — explicit operator override.
//   2. MNEMONIC env var — local dev + CI test seam.
//
// At least one MUST be set so the dial carries identity. Without an
// identity the consensus-auth gate rejects the secret-opcode envelope.
func bootstrapIdentity(servicePath string) (*keys.ServiceIdentity, error) {
	m := strings.TrimSpace(os.Getenv("KMS_BOOTSTRAP_MNEMONIC"))
	if m == "" {
		m = strings.TrimSpace(os.Getenv("MNEMONIC"))
	}
	if m == "" {
		return nil, fmt.Errorf("KMS_BOOTSTRAP_MNEMONIC (or MNEMONIC) must be set to dial KMS")
	}
	return keys.NewServiceIdentity(m, servicePath)
}
