// SPDX-License-Identifier: BSD-3-Clause-Eco

package main

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A network owns its chains, and no other network answers for them.
//
// Lux's primary network owns c — its primary-network EVM — along with the
// platform and exchange chains beside it. Hanzo and Zoo are sovereign L1s:
// each runs its own EVM under its own name, and its primary network id is that
// EVM's chain id. So c reaches a Lux node or it reaches nothing, and a bare
// root reaches the root of the network the caller addressed, never another's.
type network struct {
	name   string // lux, hanzo, zoo — how the config names this network's node
	root   string // the alias a bare root resolves to here, and this net's own EVM
	evm    uint64 // the chain id that EVM answers, and the id this network is named by
	path   string // where its node serves that EVM
	prefix string // a brand prefix an older client still writes, if any
	domain string // the api host that addresses this network
	label  string
	symbol string
	vm     string // the EVM implementation its node runs
}

// The three networks, in the order every report lists them.
var networks = []network{
	{
		name: "lux", root: "c", evm: 96369, path: "/v1/chain/c/rpc",
		domain: "api.lux.network", label: "Lux C-Chain (primary-network EVM)",
		symbol: "LUX", vm: "luxfi/geth",
	},
	{
		name: "hanzo", root: "hanzo", evm: 36963, path: "/v1/chain/hanzo/rpc",
		prefix: "/hanzo", domain: "api.hanzo.network", label: "Hanzo EVM",
		symbol: "AI", vm: "lux-rs/evm",
	},
	{
		name: "zoo", root: "zoo", evm: 200200, path: "/v1/chain/zoo/rpc",
		prefix: "/zoo", domain: "api.zoo.network", label: "Zoo EVM",
		symbol: "ZOO", vm: "lux-cpp/evm",
	},
}

// chain is one chain, the network that owns it, and the path its node serves it at.
type chain struct {
	network string
	path    string
}

// chains is every spelling a caller may write, derived from the networks that
// own them: a network owns its own name and its chain id, and Lux's primary
// network carries the platform and exchange chains as well as its EVM.
var chains = func() map[string]chain {
	all := map[string]chain{}
	for _, n := range networks {
		own := chain{network: n.name, path: n.path}
		all[n.root] = own
		all[strconv.FormatUint(n.evm, 10)] = own
	}
	all["p"] = chain{"lux", "/v1/chain/p"}
	all["x"] = chain{"lux", "/v1/chain/x"}
	return all
}()

// lux is the network a caller reaches by addressing the gateway itself: the
// mesh entrance, whose own chain is the primary-network EVM.
func lux() network { return networks[0] }

func networkNamed(name string) network {
	for _, n := range networks {
		if n.name == name {
			return n
		}
	}
	return lux()
}

// networkForHost is the network a caller addressed by name, if they addressed
// one; a request to the gateway's own address addresses no single network.
func networkForHost(host string) (network, bool) {
	for _, n := range networks {
		if host == n.domain {
			return n, true
		}
	}
	return network{}, false
}

// RPC is where a network's node listens.
func (c GatewayConfig) RPC(name string) string {
	switch name {
	case "zoo":
		return c.ZooRPC
	case "hanzo":
		return c.HanzoRPC
	}
	return c.LuxRPC
}

// A network has to say which chain it runs before this gateway will answer for
// it. A config that merely names an upstream "lux" is not evidence: this
// gateway served Hanzo's 36963 as the Lux primary EVM for as long as it took
// anyone to ask the chain instead of reading the label.
type identity struct {
	cfg  GatewayConfig
	mu   sync.Mutex
	seen map[string]sighting
}

type sighting struct {
	evm  uint64
	err  error
	when time.Time
}

// How long an answer stands before the gateway asks again — briefly when the
// last answer was an error, so a node coming up is noticed, and long enough
// when it was good that a healthy chain costs one round trip a quarter minute.
const (
	identityGood  = 15 * time.Second
	identityRetry = 2 * time.Second
)

func newIdentity(cfg GatewayConfig) *identity {
	return &identity{cfg: cfg, seen: map[string]sighting{}}
}

// observe reports the chain id a network's node last said it runs.
func (id *identity) observe(n network) (uint64, error) {
	id.mu.Lock()
	defer id.mu.Unlock()

	if s, ok := id.seen[n.name]; ok {
		stands := identityGood
		if s.err != nil {
			stands = identityRetry
		}
		if time.Since(s.when) < stands {
			return s.evm, s.err
		}
	}
	evm, err := queryChainID(id.cfg.RPC(n.name) + n.path)
	id.seen[n.name] = sighting{evm: evm, err: err, when: time.Now()}
	return evm, err
}

// confirm is the rule: a network answers for its chains only while its node is
// running the chain that network is. Anything else is refused, by name.
func (id *identity) confirm(n network) error {
	evm, err := id.observe(n)
	if err != nil {
		return fmt.Errorf("the %s node at %s did not say which chain it runs: %v",
			n.name, id.cfg.RPC(n.name), err)
	}
	if evm != n.evm {
		return fmt.Errorf("the %s node at %s runs chain %d, but %s is chain %d; "+
			"refusing to serve one network's chain under another's name",
			n.name, id.cfg.RPC(n.name), evm, n.name, n.evm)
	}
	return nil
}

// queryChainID asks a chain which chain it is.
func queryChainID(rpcURL string) (uint64, error) {
	res, err := httpPost(rpcURL, "eth_chainId", []any{})
	if err != nil {
		return 0, err
	}
	if e, ok := res["error"]; ok && e != nil {
		return 0, fmt.Errorf("%v", e)
	}
	hex, ok := res["result"].(string)
	if !ok {
		return 0, fmt.Errorf("eth_chainId answered %v", res["result"])
	}
	return strconv.ParseUint(strings.TrimPrefix(hex, "0x"), 16, 64)
}
