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
	// the whole of `cChainGenesis` should be set as a string, not a json object...
	gethCChainGenesis := geth_params.LuxLocalChainConfig
	// but the part in geth is only the "config" part.
	// In order to set it easily, first we get the cChainGenesis item
	// convert it to a map
	cChainGenesisMap, ok := cChainGenesis.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf(
			"expected field 'cChainGenesis' of genesisMap to be a map[string]interface{}, but it failed with type %T", cChainGenesis)
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
