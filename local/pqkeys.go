// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package local

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"

	"github.com/luxfi/crypto/mldsa"
	"github.com/luxfi/crypto/mlkem"
	"github.com/luxfi/netrunner/network/node"
	"github.com/luxfi/node/config"
)

// generatePQKeyFlags mints a fresh ML-DSA-65 staking keypair (FIPS 204) and an
// ML-KEM-768 handshake keypair (FIPS 203) and returns the four luxd config
// flags that carry them as base64-encoded PEM.
//
// Under the strict post-quantum security profile (the default for the luxfi
// genesis), every node MUST present these keys at startup or luxd aborts with
// "strict-PQ profile requires the staking ML-DSA keypair". Netrunner otherwise
// generates only the classical TLS staking cert + BLS signer, so a PQ-profile
// network never comes up. Injecting these flags at node launch closes that gap
// — the netrunner counterpart of node/cmd/pqkeygen.
func generatePQKeyFlags() (map[string]interface{}, error) {
	pemB64 := func(typ string, der []byte) (string, error) {
		var b bytes.Buffer
		if err := pem.Encode(&b, &pem.Block{Type: typ, Bytes: der}); err != nil {
			return "", err
		}
		return base64.StdEncoding.EncodeToString(b.Bytes()), nil
	}

	dsa, err := mldsa.GenerateKey(rand.Reader, mldsa.MLDSA65)
	if err != nil {
		return nil, err
	}
	kemPub, kemPriv, err := mlkem.GenerateKeyPair(rand.Reader, mlkem.MLKEM768)
	if err != nil {
		return nil, err
	}

	dsaKey, err := pemB64("ML-DSA-65 PRIVATE KEY", dsa.Bytes())
	if err != nil {
		return nil, err
	}
	dsaPub, err := pemB64("ML-DSA-65 PUBLIC KEY", dsa.PublicKey.Bytes())
	if err != nil {
		return nil, err
	}
	kemKey, err := pemB64("ML-KEM-768 PRIVATE KEY", kemPriv.Bytes())
	if err != nil {
		return nil, err
	}
	kemPubPEM, err := pemB64("ML-KEM-768 PUBLIC KEY", kemPub.Bytes())
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		config.StakingMLDSAKeyContentKey:      dsaKey,
		config.StakingMLDSAPubKeyContentKey:   dsaPub,
		config.HandshakeMLKEMKeyContentKey:    kemKey,
		config.HandshakeMLKEMPubKeyContentKey: kemPubPEM,
	}, nil
}

// ensurePQKeys injects fresh PQ staking/handshake key flags into a node config
// when they are absent, so every node satisfies the strict-PQ security profile.
// A no-op if the caller already supplied ML-DSA content (e.g. from a keys dir).
func ensurePQKeys(nodeConfig *node.Config) error {
	if nodeConfig.Flags == nil {
		nodeConfig.Flags = map[string]interface{}{}
	}
	if _, ok := nodeConfig.Flags[config.StakingMLDSAKeyContentKey]; ok {
		return nil
	}
	pqFlags, err := generatePQKeyFlags()
	if err != nil {
		return err
	}
	for k, v := range pqFlags {
		nodeConfig.Flags[k] = v
	}
	return nil
}
