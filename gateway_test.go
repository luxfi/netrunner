package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// One chain, however a caller spells it.
//
// The measurement that prompted this: a node answered /v1/chain/C/rpc and
// answered nothing for /v1/chain/c, so the gateway in front of it had a branch
// per spelling and still missed most of them.
func TestChainAliasIsOneRule(t *testing.T) {
	for _, one := range []struct{ path, alias string }{
		{"/v1/chain/c/rpc", "c"},
		{"/v1/chain/c", "c"},
		{"/v1/chain/C/rpc", "c"},
		{"/v1/chain/C", "c"},
		{"/v1/bc/C/rpc", "c"},
		{"/v1/bc/c", "c"},
		{"/v1/chain/p", "p"},
		{"/v1/chain/x/rpc", "x"},
		{"/v1/chain/zoo", "zoo"},
		{"/v1/chain/ZOO", "zoo"},
		{"/v1/chain/hanzo/rpc", "hanzo"},
		{"/v1/chain/Hanzo/rpc", "hanzo"},
		{"/v1/chain/200200", "200200"},

		// Not a chain: the words around the alias are literals, and a path
		// carrying more than an endpoint names something else.
		{"/v1/CHAIN/c", ""},
		{"/ext/bc/C/rpc", ""},
		{"/v1/chain/c/ws", ""},
		{"/v1/chain/c/rpc/extra", ""},
		{"/zoo", ""},
		{"/", ""},
	} {
		if got := chainAlias(one.path); got != one.alias {
			t.Errorf("chainAlias(%q) = %q, want %q", one.path, got, one.alias)
		}
	}
}

// mesh stands in for the three runtimes. Each node answers eth_chainId with the
// chain it is running, and records the path of every other call, so a test can
// see both which node was reached and what it was asked for.
type mesh struct {
	asked map[string]chan string
	cfg   GatewayConfig
}

func newMesh(t *testing.T, running map[string]uint64) *mesh {
	t.Helper()
	m := &mesh{asked: map[string]chan string{}}
	at := map[string]string{}
	for _, n := range networks {
		evm := running[n.name]
		seen := make(chan string, 16)
		m.asked[n.name] = seen
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var call struct {
				Method string `json:"method"`
			}
			json.NewDecoder(r.Body).Decode(&call)
			if call.Method == "eth_chainId" {
				if evm == 0 { // a node that is not running answers nothing useful
					http.Error(w, "down", http.StatusBadGateway)
					return
				}
				fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":"0x%x"}`, evm)
				return
			}
			seen <- r.URL.Path
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":"0x1"}`)
		}))
		t.Cleanup(s.Close)
		at[n.name] = s.URL
	}
	m.cfg = GatewayConfig{
		LuxRPC: at["lux"], ZooRPC: at["zoo"], HanzoRPC: at["hanzo"], ExplorerRPC: at["lux"],
	}
	return m
}

// ask sends a call to the gateway as if it arrived on the given host, and
// answers with the status and the path the reached node saw.
func (m *mesh) ask(t *testing.T, host, path string) (int, string) {
	t.Helper()
	g, err := newGateway(m.cfg)
	if err != nil {
		t.Fatalf("gateway: %v", err)
	}
	rec := httptest.NewRecorder()
	// Built at the root and then pointed at the path under test, so that a
	// caller writing no path at all is expressible here too.
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"method":"eth_blockNumber"}`))
	req.URL.Path = path
	if n, named := networkForHost(host); named {
		g.route(rec, req, n, onlyItsOwn)
	} else {
		g.route(rec, req, lux(), anyNetwork)
	}
	for _, n := range networks {
		select {
		case got := <-m.asked[n.name]:
			return rec.Code, n.name + " " + got
		default:
		}
	}
	return rec.Code, ""
}

// The mesh as it should run: each network on its own chain.
func healthy() map[string]uint64 {
	return map[string]uint64{"lux": 96369, "hanzo": 36963, "zoo": 200200}
}

// Every spelling of a chain reaches the network that owns it, at the path that
// network's node serves it at.
func TestEverySpellingReachesTheNetworkThatOwnsIt(t *testing.T) {
	m := newMesh(t, healthy())
	for _, one := range []struct{ ask, reached string }{
		// Lux's primary network owns c, and its platform and exchange chains.
		{"/v1/chain/c", "lux /v1/chain/c/rpc"},
		{"/v1/chain/c/rpc", "lux /v1/chain/c/rpc"},
		{"/v1/chain/C", "lux /v1/chain/c/rpc"},
		{"/v1/chain/C/rpc", "lux /v1/chain/c/rpc"},
		{"/v1/bc/c", "lux /v1/chain/c/rpc"},
		{"/v1/chain/96369", "lux /v1/chain/c/rpc"},
		{"/ext/bc/C/rpc", "lux /v1/chain/c/rpc"},
		{"/", "lux /v1/chain/c/rpc"},
		{"/v1/chain/p", "lux /v1/chain/p"},
		{"/v1/chain/x", "lux /v1/chain/x"},

		// The sovereign L1s, by name or by chain id, in any case.
		{"/v1/chain/hanzo", "hanzo /v1/chain/hanzo/rpc"},
		{"/v1/chain/Hanzo/rpc", "hanzo /v1/chain/hanzo/rpc"},
		{"/v1/chain/36963", "hanzo /v1/chain/hanzo/rpc"},
		{"/hanzo", "hanzo /v1/chain/hanzo/rpc"},
		{"/v1/chain/zoo", "zoo /v1/chain/zoo/rpc"},
		{"/v1/chain/ZOO/rpc", "zoo /v1/chain/zoo/rpc"},
		{"/v1/chain/200200", "zoo /v1/chain/zoo/rpc"},
		{"/zoo", "zoo /v1/chain/zoo/rpc"},
	} {
		code, reached := m.ask(t, "", one.ask)
		if code != http.StatusOK {
			t.Errorf("%s: status %d, want 200", one.ask, code)
			continue
		}
		if reached != one.reached {
			t.Errorf("%s reached %q, want %q", one.ask, reached, one.reached)
		}
	}
}

// The rule, as it was measured breaking: the gateway served chain 36963 —
// Hanzo's — to every caller who asked for the Lux primary-network EVM. c is
// Lux's and no one else's, so it reaches the Lux node or it reaches nobody.
func TestCReachesOnlyLux(t *testing.T) {
	m := newMesh(t, healthy())
	for _, spelling := range []string{"/v1/chain/c", "/v1/chain/C", "/v1/chain/C/rpc", "/v1/chain/96369", "/"} {
		code, reached := m.ask(t, "", spelling)
		if code != http.StatusOK || !strings.HasPrefix(reached, "lux ") {
			t.Errorf("%s reached %q (status %d), want the lux node", spelling, reached, code)
		}
	}
}

// A caller who names a network gets that network's chains and no others. c
// exists only on Lux, so it is not a chain a Hanzo or Zoo host has.
func TestANamedHostServesOnlyItsOwnChains(t *testing.T) {
	m := newMesh(t, healthy())
	for _, one := range []struct {
		host, ask, reached string
		code               int
	}{
		{"api.lux.network", "/", "lux /v1/chain/c/rpc", 200},
		{"api.lux.network", "/v1/chain/c", "lux /v1/chain/c/rpc", 200},
		{"api.lux.network", "/v1/chain/hanzo", "", 404},
		{"api.lux.network", "/v1/chain/zoo", "", 404},

		{"api.hanzo.network", "/", "hanzo /v1/chain/hanzo/rpc", 200},
		{"api.hanzo.network", "/v1/chain/hanzo", "hanzo /v1/chain/hanzo/rpc", 200},
		{"api.hanzo.network", "/v1/chain/36963", "hanzo /v1/chain/hanzo/rpc", 200},
		{"api.hanzo.network", "/v1/chain/c", "", 404},
		{"api.hanzo.network", "/v1/chain/C/rpc", "", 404},
		{"api.hanzo.network", "/v1/chain/zoo", "", 404},

		{"api.zoo.network", "/", "zoo /v1/chain/zoo/rpc", 200},
		{"api.zoo.network", "/v1/chain/zoo", "zoo /v1/chain/zoo/rpc", 200},
		{"api.zoo.network", "/v1/chain/200200", "zoo /v1/chain/zoo/rpc", 200},
		{"api.zoo.network", "/v1/chain/c", "", 404},
		{"api.zoo.network", "/v1/chain/hanzo", "", 404},
	} {
		code, reached := m.ask(t, one.host, one.ask)
		if code != one.code || reached != one.reached {
			t.Errorf("%s%s gave (%d, %q), want (%d, %q)",
				one.host, one.ask, code, reached, one.code, one.reached)
		}
	}
}

// A chain nobody owns is not served by guesswork.
func TestAnUnknownChainIsRefused(t *testing.T) {
	m := newMesh(t, healthy())
	for _, ask := range []string{"/v1/chain/zzz", "/v1/chain/1", "/v1/chain/96368"} {
		if code, _ := m.ask(t, "", ask); code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", ask, code)
		}
	}
}

// The measured production failure in full: the config named an upstream "lux",
// but the node it named was running Hanzo's 36963. A gateway that reads its own
// label serves Hanzo's chain as the Lux primary EVM and says ONLINE while doing
// it; one that asks the chain refuses and says which chain it found.
func TestANodeRunningAnotherNetworksChainIsRefused(t *testing.T) {
	wrong := healthy()
	wrong["lux"] = 36963 // a luxd started with --network-id=36963
	m := newMesh(t, wrong)

	code, reached := m.ask(t, "", "/v1/chain/c")
	if code != http.StatusBadGateway {
		t.Fatalf("/v1/chain/c: status %d, want 502; it reached %q", code, reached)
	}
	if reached != "" {
		t.Errorf("/v1/chain/c reached %q; it must reach no node at all", reached)
	}

	// The other two networks are unaffected: one bad upstream is not an outage.
	if code, reached := m.ask(t, "", "/v1/chain/zoo"); code != http.StatusOK || reached != "zoo /v1/chain/zoo/rpc" {
		t.Errorf("zoo gave (%d, %q) while lux was misconfigured", code, reached)
	}

	// And the report says so, rather than printing the label it was given.
	g, err := newGateway(m.cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range g.chainStates() {
		if state["network"] != "lux" {
			continue
		}
		if state["status"] != "WRONG CHAIN" {
			t.Errorf("lux status = %v, want WRONG CHAIN", state["status"])
		}
		if state["chain_id"] != uint64(36963) {
			t.Errorf("lux chain_id = %v, want the 36963 actually found", state["chain_id"])
		}
	}
}

// A network whose node is not answering is reported as unreachable, and is not
// served from another network's node in the meantime.
func TestAnUnreachableNetworkIsRefused(t *testing.T) {
	down := healthy()
	down["lux"] = 0
	m := newMesh(t, down)

	if code, _ := m.ask(t, "", "/v1/chain/c"); code != http.StatusBadGateway {
		t.Errorf("/v1/chain/c: status %d, want 502", code)
	}
	if code, reached := m.ask(t, "", "/v1/chain/hanzo"); code != http.StatusOK || reached != "hanzo /v1/chain/hanzo/rpc" {
		t.Errorf("hanzo gave (%d, %q) while lux was down", code, reached)
	}
}

// Root is the addressed network's own EVM, reached with no path — so a wallet
// pointed at a bare host works, and never lands on another brand's chain.
func TestRootIsTheNetworksOwnChain(t *testing.T) {
	m := newMesh(t, healthy())
	for host, want := range map[string]string{
		"api.lux.network":   "lux /v1/chain/c/rpc",
		"api.hanzo.network": "hanzo /v1/chain/hanzo/rpc",
		"api.zoo.network":   "zoo /v1/chain/zoo/rpc",
	} {
		for _, root := range []string{"/", ""} {
			code, reached := m.ask(t, host, root)
			if code != http.StatusOK || reached != want {
				t.Errorf("%s%q gave (%d, %q), want (200, %q)", host, root, code, reached, want)
			}
		}
	}
}

// Every chain in the table belongs to a network that exists, and every network
// owns its own name and its own chain id. The table is the rule, so it has to
// say what the rule says.
func TestTheTableSaysWhatTheRuleSays(t *testing.T) {
	for alias, c := range chains {
		if networkNamed(c.network).name != c.network {
			t.Errorf("chain %q belongs to unknown network %q", alias, c.network)
		}
	}
	for _, n := range networks {
		if own := chains[n.root]; own.network != n.name {
			t.Errorf("%s does not own its own alias %q", n.name, n.root)
		}
		id := fmt.Sprintf("%d", n.evm)
		if own := chains[id]; own.network != n.name {
			t.Errorf("%s does not own its own chain id %s", n.name, id)
		}
	}
	// c is Lux's, and is the one alias no sovereign L1 may claim.
	if chains["c"].network != "lux" {
		t.Errorf("c belongs to %q, want lux", chains["c"].network)
	}
}

func TestGatewayPort(t *testing.T) {
	for addr, want := range map[string]string{
		":8080": ":8080", "127.0.0.1:8080": ":8080", "0.0.0.0:80": ":80", "8080": ":8080",
	} {
		if got := gatewayPort(addr); got != want {
			t.Errorf("gatewayPort(%q) = %q, want %q", addr, got, want)
		}
	}
}
