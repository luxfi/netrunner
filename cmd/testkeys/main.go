package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/luxfi/keys"
)

func main() {
	mnemonic := os.Getenv("LUX_MNEMONIC")
	if mnemonic == "" {
		fmt.Println("LUX_MNEMONIC not set")
		os.Exit(1)
	}

	home, _ := os.UserHomeDir()
	keysDir := home + "/.lux/keys"
	ks := keys.NewKeyStore(keysDir)

	// Check if node0-4 keys already exist
	existingNodeKeys := make([]*keys.ValidatorKey, 5)
	allExist := true
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("node%d", i)
		loaded, err := ks.Load(name)
		if err != nil || loaded == nil {
			allExist = false
			break
		}
		existingNodeKeys[i] = loaded
	}

	if allExist {
		fmt.Println("=== Loading existing keys (stable NodeIDs) ===")
		for i, vk := range existingNodeKeys {
			fmt.Printf("V%d: NodeID=%s P-addr=%s\n", i, vk.NodeID.String(), vk.PChainAddr.String())
		}
		return
	}

	// Keys don't exist - derive from mnemonic and persist (first run only)
	fmt.Println("=== Deriving 5 validators from mnemonic (first run) ===")
	validators, err := keys.DeriveValidatorsFromMnemonic(mnemonic, 5)
	if err != nil {
		fmt.Printf("Error deriving: %v\n", err)
		os.Exit(1)
	}

	for i, vk := range validators {
		fmt.Printf("V%d: NodeID=%s P-addr=%s\n", i, vk.NodeID.String(), vk.PChainAddr.String())

		// Save key
		name := fmt.Sprintf("node%d", i)
		if err := ks.Save(name, vk); err != nil {
			fmt.Printf("  Error saving: %v\n", err)
		} else {
			fmt.Printf("  Saved to %s/%s\n", keysDir, name)
		}
	}

	// Also list what's in keys directory
	fmt.Println("\n=== Keys directory contents ===")
	entries, _ := os.ReadDir(keysDir)
	var names []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "node") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Printf("  %s\n", n)
	}
}
