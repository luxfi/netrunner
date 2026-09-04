package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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
		{"/v1/chain/C/rpc", "c"},
		{"/v1/chain/c/rpc", "c"},
		{"/v1/chain/C", "c"},
		{"/v1/chain/c", "c"},
		{"/v1/bc/C/rpc", "c"},
		{"/v1/bc/c", "c"},
		{"/v1/chain/P", "p"},
		{"/v1/chain/x/rpc", "x"},
		{"/v1/chain/ZOO", "zoo"},
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

// Every spelling of a chain reaches the node that serves it, at the path that
// node serves it at.
func TestEverySpellingReachesItsNode(t *testing.T) {
	// Three stand-ins for the three runtimes, each reporting what it was asked.
	var asked = map[string]chan string{
		"lux": make(chan string, 8), "zoo": make(chan string, 8), "hanzo": make(chan string, 8),
	}
	nodes := map[string]*httptest.Server{}
	for name, seen := range asked {
		seen := seen
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen <- r.URL.Path
			w.WriteHeader(http.StatusOK)
		}))
		defer s.Close()
		nodes[name] = s
	}

	cfg := GatewayConfig{
		LuxRPC:      nodes["lux"].URL,
		ZooRPC:      nodes["zoo"].URL,
		HanzoRPC:    nodes["hanzo"].URL,
		ExplorerRPC: nodes["lux"].URL,
	}
	luxURL, _ := url.Parse(cfg.LuxRPC)
	zooURL, _ := url.Parse(cfg.ZooRPC)
	hanzoURL, _ := url.Parse(cfg.HanzoRPC)
	lux, zoo, hanzo := newProxy(luxURL), newProxy(zooURL), newProxy(hanzoURL)

	for _, one := range []struct{ ask, node, got string }{
		{"/v1/chain/C/rpc", "lux", "/v1/bc/C/rpc"},
		{"/v1/chain/c/rpc", "lux", "/v1/bc/C/rpc"},
		{"/v1/chain/C", "lux", "/v1/bc/C/rpc"},
		{"/v1/chain/c", "lux", "/v1/bc/C/rpc"},
		{"/v1/bc/c", "lux", "/v1/bc/C/rpc"},
		{"/", "lux", "/v1/bc/C/rpc"},
		{"/v1/chain/p", "lux", "/v1/bc/P"},
		{"/v1/chain/P/rpc", "lux", "/v1/bc/P"},
		{"/v1/chain/x", "lux", "/v1/bc/X"},
		{"/ext/bc/C/rpc", "lux", "/v1/bc/C/rpc"},

		// The L2s, reached by name or by chain id, in any case.
		{"/v1/chain/zoo", "zoo", "/v1/chain/C/rpc"},
		{"/v1/chain/ZOO/rpc", "zoo", "/v1/chain/C/rpc"},
		{"/v1/chain/200200", "zoo", "/v1/chain/C/rpc"},
		{"/v1/chain/hanzo", "hanzo", "/v1/chain/C/rpc"},
		{"/v1/chain/Hanzo/rpc", "hanzo", "/v1/chain/C/rpc"},
		{"/v1/chain/36963", "hanzo", "/v1/chain/C/rpc"},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, one.ask, strings.NewReader(`{}`))
		routeLux(rec, req, lux, zoo, hanzo, cfg)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status %d", one.ask, rec.Code)
			continue
		}
		select {
		case got := <-asked[one.node]:
			if got != one.got {
				t.Errorf("%s reached %s at %q, want %q", one.ask, one.node, got, one.got)
			}
		default:
			t.Errorf("%s never reached %s", one.ask, one.node)
		}
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
