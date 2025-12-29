package local

import (
    "fmt"
    "os"
    "testing"
    
    "github.com/btcsuite/btcd/btcutil/hdkeychain"
    "github.com/btcsuite/btcd/chaincfg"
    "github.com/luxfi/go-bip39"
    luxcrypto "github.com/luxfi/crypto/secp256k1"
    "golang.org/x/crypto/sha3"
)

func TestMnemonicDerivation(t *testing.T) {
    mnemonic := os.Getenv("LUX_MNEMONIC")
    if mnemonic == "" {
        t.Skip("LUX_MNEMONIC not set")
    }
    
    fmt.Printf("Mnemonic: %s...\n", mnemonic[:20])
    
    if !bip39.IsMnemonicValid(mnemonic) {
        t.Fatal("Invalid mnemonic")
    }
    
    seed := bip39.NewSeed(mnemonic, "")
    
    // Derive using BIP44 m/44'/60'/0'/0/0
    masterKey, _ := hdkeychain.NewMaster(seed, &chaincfg.MainNetParams)
    purpose, _ := masterKey.Derive(hdkeychain.HardenedKeyStart + 44)
    coinType, _ := purpose.Derive(hdkeychain.HardenedKeyStart + 60)
    account, _ := coinType.Derive(hdkeychain.HardenedKeyStart + 0)
    change, _ := account.Derive(0)
    addressKey, _ := change.Derive(0)
    
    ecPrivKey, _ := addressKey.ECPrivKey()
    privKeyBytes := ecPrivKey.Serialize()
    
    privKey, err := luxcrypto.ToPrivateKey(privKeyBytes)
    if err != nil {
        t.Fatalf("Error: %v", err)
    }
    pubKey := privKey.PublicKey()
    shortID := pubKey.Address()
    
    // Get Ethereum address
    pubKeyBytes := pubKey.Bytes()
    hash := sha3.NewLegacyKeccak256()
    hash.Write(pubKeyBytes[1:]) // skip the 0x04 prefix
    ethAddr := hash.Sum(nil)[12:]
    
    fmt.Printf("\n=== Key Derivation Results (m/44'/60'/0'/0/0) ===\n")
    fmt.Printf("Lux Short ID: %s\n", shortID.String())
    fmt.Printf("Ethereum Address: 0x%x\n", ethAddr)
    fmt.Printf("Expected Treasury: 0x9011E888251AB053B7bD1cdB598Db4f9DEd94714\n")
    
    // Also derive using coin type 9000 for comparison
    coinType9000, _ := purpose.Derive(hdkeychain.HardenedKeyStart + 9000)
    account9000, _ := coinType9000.Derive(hdkeychain.HardenedKeyStart + 0)
    change9000, _ := account9000.Derive(0)
    addressKey9000, _ := change9000.Derive(0)
    ecPrivKey9000, _ := addressKey9000.ECPrivKey()
    privKey9000, _ := luxcrypto.ToPrivateKey(ecPrivKey9000.Serialize())
    shortID9000 := privKey9000.PublicKey().Address()
    
    fmt.Printf("\n=== Comparison: Coin Type 9000 (m/44'/9000'/0'/0/0) ===\n")
    fmt.Printf("Lux Short ID (9000): %s\n", shortID9000.String())
    
    fmt.Printf("\n=== Genesis Treasury P-chain Address ===\n")
    fmt.Printf("Expected: P-lux1jqg73zp9r2c98daarnd4nrd5l80dj3c5eha5fl\n")
}
