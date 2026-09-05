// SPDX-License-Identifier: BSD-3-Clause-Eco
package main

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/luxfi/crypto/secp256k1"
	"golang.org/x/crypto/pbkdf2"
)

// Signer is the account the bot signs with. The seed phrase is read from the
// environment and kept in memory: it is never a constant, never written to a
// file, and never printed. Only the address, which is public, is ever shown.
type Signer struct {
	key     []byte
	Address string
}

// SignerFromEnv builds the signer from LUX_MNEMONIC at the Ethereum account
// path m/44'/60'/0'/0/0. There is no fallback: without the phrase there is no
// signer, and the caller stops rather than sending from some other account.
func SignerFromEnv() (*Signer, error) {
	phrase := strings.Join(strings.Fields(os.Getenv("LUX_MNEMONIC")), " ")
	if phrase == "" {
		return nil, errors.New("LUX_MNEMONIC is not set; the bot has no account to sign with")
	}
	if n := len(strings.Fields(phrase)); n != 12 && n != 15 && n != 18 && n != 21 && n != 24 {
		return nil, fmt.Errorf("LUX_MNEMONIC has %d words; a seed phrase has 12, 15, 18, 21 or 24", n)
	}
	key, err := deriveAccountKey(phrase)
	if err != nil {
		return nil, err
	}
	if _, err := secp256k1.ToPrivateKey(key); err != nil {
		return nil, fmt.Errorf("derived key is not a valid secp256k1 scalar: %w", err)
	}
	return &Signer{key: key, Address: addressOf(key)}, nil
}

// addressOf is the last 20 bytes of the Keccak of the uncompressed public
// point, without its leading tag byte. The compressed form hashes to something
// that looks like an address and is not one, so the point is expanded here.
func addressOf(key []byte) string {
	x, y := secp256k1.S256().ScalarBaseMult(key)
	sum := secp256k1.Keccak256(secp256k1.S256().Marshal(x, y)[1:])
	return "0x" + hex.EncodeToString(sum[12:])
}

// deriveAccountKey walks BIP-32 from the BIP-39 seed to m/44'/60'/0'/0/0.
func deriveAccountKey(phrase string) ([]byte, error) {
	seed := pbkdf2.Key([]byte(phrase), []byte("mnemonic"), 2048, 64, sha512.New)
	sum := hmacSHA512([]byte("Bitcoin seed"), seed)
	key, chain := sum[:32], sum[32:]

	const hardened = uint32(1) << 31
	for _, index := range []uint32{44 | hardened, 60 | hardened, 0 | hardened, 0, 0} {
		var err error
		if key, chain, err = childKey(key, chain, index); err != nil {
			return nil, err
		}
	}
	return key, nil
}

// childKey is BIP-32 CKDpriv. A hardened index commits to the parent key, a
// normal one to the parent's compressed public point.
func childKey(key, chain []byte, index uint32) ([]byte, []byte, error) {
	data := make([]byte, 0, 37)
	if index >= uint32(1)<<31 {
		data = append(data, 0x00)
		data = append(data, key...)
	} else {
		data = append(data, compressedPoint(key)...)
	}
	data = append(data, byte(index>>24), byte(index>>16), byte(index>>8), byte(index))

	sum := hmacSHA512(chain, data)
	order := secp256k1.S256().Params().N
	offset := new(big.Int).SetBytes(sum[:32])
	if offset.Cmp(order) >= 0 {
		return nil, nil, fmt.Errorf("child index %d falls outside the curve order", index)
	}
	child := offset.Add(offset, new(big.Int).SetBytes(key))
	child.Mod(child, order)
	if child.Sign() == 0 {
		return nil, nil, fmt.Errorf("child index %d derives the zero key", index)
	}
	out := make([]byte, 32)
	child.FillBytes(out)
	return out, sum[32:], nil
}

// compressedPoint is the 33-byte SEC1 form of the public point for a key.
func compressedPoint(key []byte) []byte {
	x, y := secp256k1.S256().ScalarBaseMult(key)
	out := make([]byte, 33)
	out[0] = 0x02 + byte(y.Bit(0))
	x.FillBytes(out[1:])
	return out
}

func hmacSHA512(key, data []byte) []byte {
	mac := hmac.New(sha512.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}
