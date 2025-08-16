package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"path/filepath"

	badger "github.com/dgraph-io/badger/v3"
	"github.com/ethereum/go-ethereum/common"
)

func main() {
	var dbPath string
	flag.StringVar(&dbPath, "db", "", "BadgerDB path")
	flag.Parse()

	if dbPath == "" {
		log.Fatal("Usage: check-last-block --db <badgerdb>")
	}

	// Open BadgerDB
	opts := badger.DefaultOptions(filepath.Clean(dbPath)).WithReadOnly(true)
	db, err := badger.Open(opts)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	// Check for LastBlock key
	var lastHash common.Hash
	err = db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("LastBlock"))
		if err != nil {
			return fmt.Errorf("no LastBlock key: %v", err)
		}
		return item.Value(func(val []byte) error {
			copy(lastHash[:], val)
			return nil
		})
	})
	
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("LastBlock hash: %s\n", lastHash.Hex())
	}

	// Try to find highest canonical block
	var maxHeight uint64
	err = db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()
		
		prefix := []byte("n")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			key := it.Item().Key()
			if len(key) == 9 && key[0] == 'n' {
				height := binary.BigEndian.Uint64(key[1:9])
				if height > maxHeight {
					maxHeight = height
				}
			}
		}
		return nil
	})

	if err == nil && maxHeight > 0 {
		fmt.Printf("Highest canonical block found: %d\n", maxHeight)
	}
}
