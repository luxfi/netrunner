package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"

	"github.com/luxfi/crypto/secp256k1"
)

func rlpEncodeBytes(b []byte) []byte {
	if len(b) == 1 && b[0] < 0x80 {
		return b
	}
	if len(b) <= 55 {
		return append([]byte{byte(0x80 + len(b))}, b...)
	}
	lenBytes := big.NewInt(int64(len(b))).Bytes()
	res := []byte{byte(0xb7 + len(lenBytes))}
	res = append(res, lenBytes...)
	return append(res, b...)
}

func rlpEncodeBigInt(n *big.Int) []byte {
	if n == nil || n.Sign() == 0 {
		return []byte{0x80}
	}
	b := n.Bytes()
	return rlpEncodeBytes(b)
}

func rlpEncodeList(items [][]byte) []byte {
	var payload []byte
	for _, item := range items {
		payload = append(payload, item...)
	}
	if len(payload) <= 55 {
		return append([]byte{byte(0xc0 + len(payload))}, payload...)
	}
	lenBytes := big.NewInt(int64(len(payload))).Bytes()
	res := []byte{byte(0xf7 + len(lenBytes))}
	res = append(res, lenBytes...)
	return append(res, payload...)
}

// signEIP155Tx signs an EVM transaction with replay-protection (EIP-155)
func signEIP155Tx(signer *Signer, chainID int64, nonce uint64, toAddrHex string, value *big.Int, gasLimit uint64, gasPrice *big.Int, data []byte) (string, error) {
	if signer == nil {
		return "", errors.New("no signer")
	}
	privBytes := signer.key

	if len(toAddrHex) >= 2 && toAddrHex[:2] == "0x" {
		toAddrHex = toAddrHex[2:]
	}
	toAddr, err := hex.DecodeString(toAddrHex)
	if err != nil {
		return "", fmt.Errorf("decode toAddr: %w", err)
	}

	itemsToSign := [][]byte{
		rlpEncodeBigInt(new(big.Int).SetUint64(nonce)),
		rlpEncodeBigInt(gasPrice),
		rlpEncodeBigInt(new(big.Int).SetUint64(gasLimit)),
		rlpEncodeBytes(toAddr),
		rlpEncodeBigInt(value),
		rlpEncodeBytes(data),
		rlpEncodeBigInt(big.NewInt(chainID)),
		rlpEncodeBigInt(big.NewInt(0)),
		rlpEncodeBigInt(big.NewInt(0)),
	}
	signPayload := rlpEncodeList(itemsToSign)
	hash := secp256k1.Keccak256(signPayload)

	sig, err := secp256k1.Sign(hash, privBytes)
	if err != nil {
		return "", fmt.Errorf("secp256k1 sign: %w", err)
	}

	r := sig[:32]
	s := sig[32:64]
	recoveryID := int64(sig[64])

	v := chainID*2 + 35 + recoveryID
	vBig := big.NewInt(v)
	rBig := new(big.Int).SetBytes(r)
	sBig := new(big.Int).SetBytes(s)

	finalItems := [][]byte{
		rlpEncodeBigInt(new(big.Int).SetUint64(nonce)),
		rlpEncodeBigInt(gasPrice),
		rlpEncodeBigInt(new(big.Int).SetUint64(gasLimit)),
		rlpEncodeBytes(toAddr),
		rlpEncodeBigInt(value),
		rlpEncodeBytes(data),
		rlpEncodeBigInt(vBig),
		rlpEncodeBigInt(rBig),
		rlpEncodeBigInt(sBig),
	}
	rawTx := rlpEncodeList(finalItems)
	return "0x" + hex.EncodeToString(rawTx), nil
}

// Sample stateful EVM contract with set(uint256), get(), and destroy()
const sampleContractBytecode = "6050600c60003960506000f360043610602e5760003560e01c806360fe47b11460395780636d4ce63c14604157806383197ef014604d57602e565b600154600101600155005b600435600055005b60005460005260206000f35b33ff"

func computeContractAddress(senderHex string, nonce uint64) string {
	if len(senderHex) >= 2 && senderHex[:2] == "0x" {
		senderHex = senderHex[2:]
	}
	sender, _ := hex.DecodeString(senderHex)
	var nonceBytes []byte
	if nonce > 0 {
		nonceBytes = big.NewInt(int64(nonce)).Bytes()
	}
	encoded := rlpEncodeList([][]byte{
		rlpEncodeBytes(sender),
		rlpEncodeBytes(nonceBytes),
	})
	hash := secp256k1.Keccak256(encoded)
	return "0x" + hex.EncodeToString(hash[12:])
}
