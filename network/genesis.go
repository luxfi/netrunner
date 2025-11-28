package network

import (
	_ "embed"
	"encoding/json"
	"fmt"

	geth_params "github.com/luxfi/geth/params"
)

//go:embed default/genesis.json
var genesisBytes []byte

// LoadLocalGenesis loads the local network genesis from disk
// and returns it as a map[string]interface{}
func LoadLocalGenesis() (map[string]interface{}, error) {
	var (
		genesisMap map[string]interface{}
		err        error
	)
	if err = json.Unmarshal(genesisBytes, &genesisMap); err != nil {
		return nil, err
	}

	cChainGenesis := genesisMap["cChainGenesis"]
	// set the cchain genesis directly from geth
	// Use TestChainConfig as local config
	gethCChainGenesis := geth_params.TestChainConfig

	// Handle both string (already serialized) and map cases
	var cChainGenesisMap map[string]interface{}
	switch v := cChainGenesis.(type) {
	case string:
		// cChainGenesis is already a JSON string, parse it
		if err := json.Unmarshal([]byte(v), &cChainGenesisMap); err != nil {
			return nil, fmt.Errorf("failed to parse cChainGenesis string: %w", err)
		}
	case map[string]interface{}:
		cChainGenesisMap = v
	default:
		return nil, fmt.Errorf(
			"expected field 'cChainGenesis' to be string or map[string]interface{}, but got %T", cChainGenesis)
	}

	// set the `config` key to the actual geth object
	cChainGenesisMap["config"] = gethCChainGenesis
	// and then marshal everything into a string
	configBytes, err := json.Marshal(cChainGenesisMap)
	if err != nil {
		return nil, err
	}
	// this way the whole of `cChainGenesis` is a properly escaped string
	genesisMap["cChainGenesis"] = string(configBytes)
	return genesisMap, nil
}
