package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

type GatewayConfig struct {
	ListenAddr   string
	LuxRPC       string
	ZooRPC       string
	HanzoRPC     string
	ExplorerRPC  string
}

// chainAlias is THE one place this gateway decides which chain a path names.
//
// `/v1/chain/C/rpc`, `/v1/chain/c`, `/v1/bc/C` and `/v1/bc/c/rpc` are one
// route, not four: the word in the middle is either spelling, the alias is
// matched without regard to case, and the trailing `/rpc` is optional. Only the
// alias is the caller's to spell — the words around it are literals.
//
// It answers with the alias folded to lower case, or "" for a path that names
// no chain, which is then left exactly as the client wrote it.
func chainAlias(p string) string {
	rest, ok := strings.CutPrefix(p, "/v1/chain/")
	if !ok {
		if rest, ok = strings.CutPrefix(p, "/v1/bc/"); !ok {
			return ""
		}
	}
	alias, endpoint, _ := strings.Cut(rest, "/")
	if endpoint != "" && endpoint != "rpc" {
		return ""
	}
	return strings.ToLower(alias)
}

// gatewayPort is the ":8080" of a listen address, for building a URL that
// reaches this gateway wherever it was told to listen.
func gatewayPort(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[i:]
	}
	return ":" + addr
}

func newProxy(target *url.URL) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)
	origDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		origDirector(req)
		req.Host = target.Host
	}
	return proxy
}

// gateway is the running state behind the handler: where each network's node
// is, and what each has said about the chain it runs.
type gateway struct {
	cfg      GatewayConfig
	proxy    map[string]*httputil.ReverseProxy
	explorer *httputil.ReverseProxy
	id       *identity
}

func newGateway(cfg GatewayConfig) (*gateway, error) {
	g := &gateway{
		cfg:   cfg,
		proxy: map[string]*httputil.ReverseProxy{},
		id:    newIdentity(cfg),
	}
	for _, n := range networks {
		at, err := url.Parse(cfg.RPC(n.name))
		if err != nil {
			return nil, fmt.Errorf("%s node address %q: %w", n.name, cfg.RPC(n.name), err)
		}
		g.proxy[n.name] = newProxy(at)
	}
	explorerURL, err := url.Parse(cfg.ExplorerRPC)
	if err != nil {
		return nil, fmt.Errorf("explorer address %q: %w", cfg.ExplorerRPC, err)
	}
	g.explorer = newProxy(explorerURL)
	return g, nil
}

func startGateway(cfg GatewayConfig) error {
	g, err := newGateway(cfg)
	if err != nil {
		return err
	}
	explorerProxy := g.explorer

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := strings.ToLower(r.Host)
		if idx := strings.Index(host, ":"); idx != -1 {
			host = host[:idx]
		}

		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		p := r.URL.Path

		// Programmatic Network & Consensus as a Service endpoints
		if p == "/v1/network" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(g.networkReport())
			return
		}
		if p == "/v1/consensus" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(getConsensusResponse(cfg))
			return
		}
		if p == "/v1/zap" || p == "/zap/rpc" {
			handleZapRPC(cfg, w, r)
			return
		}
		if p == "/v1/chain/validators" || p == "/api/validators" {
			w.Header().Set("Content-Type", "application/json")
			vals, err := fetchPChainValidators(cfg.LuxRPC)
			if err != nil {
				http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(vals)
			return
		}
		if p == "/v1/chain/status" || p == "/api/status" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(g.statusReport())
			return
		}

		// A caller who named a network reaches that network and only that
		// network; one who named the gateway itself reaches the mesh, where
		// every chain is served by the network that owns it.
		if n, named := networkForHost(host); named {
			g.route(w, r, n, onlyItsOwn)
			return
		}
		switch host {
		case "explore.lux.network", "explorer.lux.network":
			routeExplorer(w, r, explorerProxy, cfg)
		default:
			switch {
			case strings.HasPrefix(p, "/explore"), strings.HasPrefix(p, "/v1/explorer"):
				routeExplorer(w, r, explorerProxy, cfg)
			default:
				g.route(w, r, lux(), anyNetwork)
			}
		}
	})

	log.Printf("[+] Gateway reverse proxy listening on %s", cfg.ListenAddr)
	for _, n := range networks {
		log.Printf("    * %-18s -> %s (%s, chain %d, root -> /v1/chain/%s)",
			n.domain, cfg.RPC(n.name), n.label, n.evm, n.root)
	}
	log.Printf("    * /v1/zap           -> Zap RPC Protocol Transport")
	log.Printf("    * explore.lux.network -> Explorer UI & P-Chain Validators")

	server := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return server.ListenAndServe()
}

// Whether a caller may reach past the network they addressed. A caller who
// named a network gets that network's chains; one who named the gateway
// itself gets the mesh.
const (
	onlyItsOwn = true
	anyNetwork = false
)

// route serves one chain to one caller.
//
// host is the network the caller addressed. It decides what a bare root means —
// root is that network's own EVM, reached with no path, and never another
// network's — and, when the caller named it, which chains exist at all.
func (g *gateway) route(w http.ResponseWriter, r *http.Request, host network, only bool) {
	p := r.URL.Path
	if p == "/v1/chain/validators" || p == "/api/validators" {
		w.Header().Set("Content-Type", "application/json")
		vals, err := fetchPChainValidators(g.cfg.LuxRPC)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(vals)
		return
	}

	alias := chainAlias(p)
	if alias == "" {
		alias = spelling(p, host)
	}
	owned, known := chains[alias]
	if !known {
		refuse(w, http.StatusNotFound, fmt.Sprintf("no chain is named %q", strings.Trim(p, "/")))
		return
	}
	// The rule: a chain belongs to one network, and only that network answers
	// for it. c is the Lux primary-network EVM, so it exists on Lux and
	// nowhere else; hanzo and zoo are their own networks' own EVMs.
	if only && owned.network != host.name {
		refuse(w, http.StatusNotFound, fmt.Sprintf(
			"%s is a chain of the %s network; %s serves %s",
			alias, owned.network, host.domain, host.root))
		return
	}
	if err := g.id.confirm(networkNamed(owned.network)); err != nil {
		refuse(w, http.StatusBadGateway, err.Error())
		return
	}

	r.URL.Path = owned.path
	g.proxy[owned.network].ServeHTTP(w, r)
}

// spelling reads a chain out of a path that does not name one outright: a bare
// root, which is the addressed network's own EVM, or one of the two shapes an
// older client still writes.
func spelling(p string, host network) string {
	if p == "" || p == "/" {
		return host.root
	}
	if rest, old := strings.CutPrefix(p, "/ext/bc/"); old {
		return chainAlias("/v1/bc/" + rest)
	}
	for _, n := range networks {
		if n.prefix != "" && strings.HasPrefix(p, n.prefix) {
			return n.root
		}
	}
	return ""
}

// refuse answers in the shape an eth client reads, so a caller who named a
// chain this address does not serve learns that, rather than reading someone
// else's chain and believing it.
func refuse(w http.ResponseWriter, status int, why string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      nil,
		"error":   map[string]any{"code": -32601, "message": why},
	})
}

func routeExplorer(w http.ResponseWriter, r *http.Request, indexerProxy *httputil.ReverseProxy, cfg GatewayConfig) {
	p := r.URL.Path

	// 1. Validator list endpoint: fetches full validator set from P-chain
	if p == "/v1/chain/validators" || p == "/api/validators" {
		w.Header().Set("Content-Type", "application/json")
		vals, err := fetchPChainValidators(cfg.LuxRPC)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(vals)
		return
	}

	// 2. Multi-chain live status endpoint
	if p == "/v1/chain/status" || p == "/api/status" {
		w.Header().Set("Content-Type", "application/json")
		status := getExplorerStatus(cfg)
		json.NewEncoder(w).Encode(status)
		return
	}

	// 3. Proxy indexer API endpoints if indexerd is running
	if strings.HasPrefix(p, "/v1/indexer") || strings.HasPrefix(p, "/v1/explorer") {
		indexerProxy.ServeHTTP(w, r)
		return
	}

	// 4. Interactive Explorer Web Dashboard
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(explorerHTMLTemplate))
}

func fetchPChainValidators(luxRPC string) (map[string]any, error) {
	client := http.Client{Timeout: 3 * time.Second}
	reqBody, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "platform.getCurrentValidators",
		"params":  map[string]any{},
	})
	resp, err := client.Post(luxRPC+"/v1/bc/P", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var res map[string]any
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}
	return res, nil
}

// chainStates reports what each network's node is actually running.
//
// The chain id here is read from the chain, not asserted beside it: this
// report used to label the Lux row 96369 while the node it pointed at was
// answering 36963, and a label cannot notice that.
func (g *gateway) chainStates() []map[string]any {
	states := make([]map[string]any, 0, len(networks))
	for _, n := range networks {
		state := map[string]any{
			"name":     n.label,
			"domain":   n.domain,
			"symbol":   n.symbol,
			"rpc_path": "/v1/chain/" + n.root,
			"vm":       n.vm,
			"network":  n.name,
			"expects":  n.evm,
		}
		evm, err := g.id.observe(n)
		switch {
		case err != nil:
			state["chain_id"] = nil
			state["status"] = "UNREACHABLE"
			state["detail"] = err.Error()
		case evm != n.evm:
			state["chain_id"] = evm
			state["status"] = "WRONG CHAIN"
			state["detail"] = fmt.Sprintf(
				"the %s node runs chain %d; %s is chain %d", n.name, evm, n.name, n.evm)
		default:
			state["chain_id"] = evm
			state["status"] = "ONLINE"
			state["block_tip"] = queryBlockNumber(g.cfg.RPC(n.name) + n.path)
		}
		states = append(states, state)
	}
	return states
}

func (g *gateway) statusReport() map[string]any {
	return map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"chains":    g.chainStates(),
		"consensus": map[string]any{
			"type":            "Snowman (Probabilistic Metastable Subsampling)",
			"sample_size_k":   5,
			"quorum_size_a":   4,
			"decision_beta":   2,
			"sampling_detail": "Per block consensus round, a randomized sample of K=5 (production K=20) validators is polled out of the total N registered validators, achieving O(1) communication overhead per node while maintaining cryptographic and probabilistic finality guarantee.",
		},
	}
}

// The CLI and the ZAP transport ask for the same reports without a gateway
// running, so they get one that lives as long as the question.
func getNetworkResponse(cfg GatewayConfig) map[string]any {
	g, err := newGateway(cfg)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return g.networkReport()
}

func getExplorerStatus(cfg GatewayConfig) map[string]any {
	g, err := newGateway(cfg)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return g.statusReport()
}

func (g *gateway) networkReport() map[string]any {
	vals, _ := fetchPChainValidators(g.cfg.LuxRPC)
	var validatorCount int
	if vals != nil && vals["result"] != nil {
		if vMap, ok := vals["result"].(map[string]any); ok {
			if vList, ok := vMap["validators"].([]any); ok {
				validatorCount = len(vList)
			}
		}
	}

	return map[string]any{
		"network":      "Lux Multi-Chain Mesh",
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
		"architecture": "Network of Sovereign EVM Chains & Shared Validators",
		"chains":       g.chainStates(),
		"p_chain": map[string]any{
			"total_validators": validatorCount,
			"rpc_path":         "/v1/chain/p",
			"staking_currency": "LUX",
		},
		"routing": map[string]any{
			"canonical_prefix": "/v1/chain/",
			"root_is_own_evm":  true,
			"chain_of_network": "a chain is served by the network that owns it, and by no other",
		},
	}
}

func getConsensusResponse(cfg GatewayConfig) map[string]any {
	vals, _ := fetchPChainValidators(cfg.LuxRPC)
	var validatorCount int
	if vals != nil && vals["result"] != nil {
		if vMap, ok := vals["result"].(map[string]any); ok {
			if vList, ok := vMap["validators"].([]any); ok {
				validatorCount = len(vList)
			}
		}
	}

	return map[string]any{
		"service":   "Programmatic Consensus as a Service (CaaS)",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"engine":    "Snowman++ (Metastable Subsampling DAG & Linear Chain)",
		"parameters": map[string]any{
			"k_sample_size":        5,
			"alpha_quorum_size":    4,
			"beta_decision_rounds": 2,
			"total_registered_n":  validatorCount,
		},
		"probabilistic_sampling": map[string]any{
			"sampled_per_round":  "Exactly K validators (K=5 locally, K=20 on mainnet) are selected uniformly at random weighted by stake",
			"communication_cost": "O(K) per validator per round = O(1) message complexity vs O(N^2) in classical BFT (pBFT/Tendermint)",
			"safety_guarantee":   "Metastable amplification drives network to irreversible unanimous state with error prob < 10^-9",
			"liveness_guarantee": "Subsampling breaks symmetry within 2-3 network hops under Byzantine fault threshold < 33%",
		},
		"features": []string{
			"Dynamic stake-weighted validator sampling",
			"Subnet and L2 consensus leasing",
			"Shared security across multiple sovereign VMs (Go, C++, Rust)",
			"Cross-chain atomic warp messaging",
		},
	}
}

func queryBlockNumber(url string) string {
	res, err := httpPost(url, "eth_blockNumber", []any{})
	if err == nil && res != nil && res["result"] != nil {
		return fmt.Sprintf("%v", res["result"])
	}
	return "0x0"
}

const explorerHTMLTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Lux Network Explorer & Validator Topology</title>
  <style>
    :root {
      --bg: #090a0f;
      --card: #12141d;
      --border: #232738;
      --text: #e2e8f0;
      --muted: #94a3b8;
      --primary: #3b82f6;
      --success: #10b981;
      --accent: #8b5cf6;
      --warning: #f59e0b;
    }
    * { box-sizing: border-box; margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; }
    body { background: var(--bg); color: var(--text); padding: 24px; }
    .container { max-width: 1200px; margin: 0 auto; }
    header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px; border-bottom: 1px solid var(--border); padding-bottom: 16px; }
    h1 { font-size: 24px; font-weight: 700; color: #fff; display: flex; align-items: center; gap: 10px; }
    .badge { background: #1e293b; color: var(--primary); padding: 4px 10px; border-radius: 6px; font-size: 12px; font-weight: 600; }
    .grid-3 { display: grid; grid-template-columns: repeat(auto-fit, minmax(320px, 1fr)); gap: 16px; margin-bottom: 24px; }
    .card { background: var(--card); border: 1px solid var(--border); border-radius: 12px; padding: 20px; }
    .card-title { font-size: 14px; color: var(--muted); text-transform: uppercase; margin-bottom: 8px; font-weight: 600; }
    .card-val { font-size: 28px; font-weight: 700; color: #fff; margin-bottom: 6px; }
    .card-sub { font-size: 13px; color: var(--muted); }
    .status-dot { display: inline-block; width: 10px; height: 10px; border-radius: 50%; background: var(--success); margin-right: 6px; }
    table { width: 100%; border-collapse: collapse; margin-top: 12px; }
    th { text-align: left; padding: 10px 12px; font-size: 12px; color: var(--muted); border-bottom: 1px solid var(--border); text-transform: uppercase; }
    td { padding: 12px; font-size: 13px; border-bottom: 1px solid #1a1e2e; }
    tr:hover td { background: #161a29; }
    .mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; color: #38bdf8; }
    .info-box { background: #171b2b; border-left: 4px solid var(--primary); padding: 16px; border-radius: 0 8px 8px 0; margin-bottom: 24px; }
    .info-box h3 { font-size: 15px; color: #fff; margin-bottom: 6px; }
    .info-box p { font-size: 13px; color: var(--muted); line-height: 1.5; }
    footer { text-align: center; font-size: 12px; color: var(--muted); margin-top: 32px; }
  </style>
</head>
<body>
  <div class="container">
    <header>
      <h1><span>⚡</span> Lux Network Explorer & Validator Topology</h1>
      <span class="badge" id="live-indicator"><span class="status-dot"></span>LIVE SYNC</span>
    </header>

    <div class="grid-3" id="chains-grid"></div>

    <div class="info-box">
      <h3>Probabilistic Consensus Sampling Architecture (Snowman Protocol)</h3>
      <p>
        <strong>How many validators are really sampled per block?</strong><br>
        In Snowman consensus, validators do <em>not</em> broadcast votes to all <strong>N</strong> validators on every block (which would cause <em>O(N²)</em> communication overhead).
        Instead, on every proposal round, a node randomly selects a small sample of <strong>K = 5</strong> (mainnet <strong>K = 20</strong>) validators uniformly at random (weighted by stake).
        When an <strong>α = 4/5 (14/20)</strong> quorum of sampled validators agree over <strong>β = 2</strong> consecutive rounds, the block achieves irrevocable finality with mathematical guarantee (error probability &lt; 10⁻⁹).
        The explorer below displays the <strong>full validator set (N)</strong> registered on the P-Chain, while each block verification round only samples <strong>K</strong> peers.
      </p>
    </div>

    <div class="card">
      <div class="card-title">P-Chain Full Registered Validator Set (N)</div>
      <table>
        <thead>
          <tr>
            <th>Node ID</th>
            <th>Stake Weight</th>
            <th>Uptime</th>
            <th>Fee</th>
            <th>Status</th>
            <th>Public Key</th>
          </tr>
        </thead>
        <tbody id="validators-tbody">
          <tr><td colspan="6" style="text-align:center; padding: 24px; color: var(--muted);">Loading validators from P-Chain...</td></tr>
        </tbody>
      </table>
    </div>

    <footer>
      Lux Universe • Native Go & Rust Core • All 3 Chains Live (Lux, Zoo, Hanzo)
    </footer>
  </div>

  <script>
    async function updateStatus() {
      try {
        var res = await fetch('/api/status');
        var data = await res.json();
        var grid = document.getElementById('chains-grid');
        var html = '';
        for (var i = 0; i < data.chains.length; i++) {
          var c = data.chains[i];
          html += '<div class="card">' +
            '<div class="card-title"><span class="status-dot"></span>' + c.name + '</div>' +
            '<div class="card-val">' + c.block_tip + '</div>' +
            '<div class="card-sub">' +
              '<strong>Domain:</strong> <span class="mono">' + c.domain + '</span><br>' +
              '<strong>Chain ID:</strong> ' + c.chain_id + ' | <strong>Coin:</strong> ' + c.symbol + '<br>' +
              '<strong>RPC Path:</strong> <span class="mono">' + c.rpc_path + '</span>' +
            '</div>' +
          '</div>';
        }
        grid.innerHTML = html;
      } catch (e) {
        console.error("status err", e);
      }
    }

    async function updateValidators() {
      try {
        var res = await fetch('/api/validators');
        var data = await res.json();
        var vals = data.result ? data.result.validators : [];
        var tbody = document.getElementById('validators-tbody');
        if (!vals || vals.length === 0) {
          tbody.innerHTML = '<tr><td colspan="6">No validators returned</td></tr>';
          return;
        }
        var html = '';
        for (var i = 0; i < vals.length; i++) {
          var v = vals[i];
          var pk = v.signer && v.signer.publicKey ? v.signer.publicKey.substring(0, 18) + '...' : 'N/A';
          var uptime = v.uptime ? parseFloat(v.uptime).toFixed(2) + '%' : '100.00%';
          var connected = v.connected !== false;
          html += '<tr>' +
            '<td class="mono">' + v.nodeID + '</td>' +
            '<td><strong>' + parseInt(v.weight).toLocaleString() + '</strong> LUX</td>' +
            '<td style="color: ' + (parseFloat(uptime) > 90 ? 'var(--success)' : 'var(--warning)') + '">' + uptime + '</td>' +
            '<td>' + (v.delegationFee || '2.0') + '%</td>' +
            '<td><span style="color: ' + (connected ? 'var(--success)' : 'var(--muted)') + '">● ' + (connected ? 'Active' : 'Standby') + '</span></td>' +
            '<td class="mono" title="' + (v.signer ? v.signer.publicKey : '') + '">' + pk + '</td>' +
          '</tr>';
        }
        tbody.innerHTML = html;
      } catch (e) {
        console.error("vals err", e);
      }
    }

    updateStatus();
    updateValidators();
    setInterval(updateStatus, 1500);
    setInterval(updateValidators, 5000);
  </script>
</body>
</html>
`
