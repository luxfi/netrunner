# Multi-Network Architecture Reference

## Overview

The Lux CLI and netrunner support running up to 4 independent primary networks simultaneously:
- **mainnet** (Network ID: 96369)
- **testnet** (Network ID: 96368)
- **devnet** (Network ID: 96370)
- **local** (Network ID: 1337)

Each network operates in complete isolation with its own:
- gRPC control server
- Node processes (5 validators default)
- Data directories
- Plugin directories
- State files

---

## 1. Complete Port Allocation Table

### Control Layer (gRPC Servers)

| Network  | gRPC Server | gRPC Gateway | Description |
|----------|-------------|--------------|-------------|
| mainnet  | 8369        | 8379         | Primary production network |
| testnet  | 8370        | 8380         | Test network |
| devnet   | 8371        | 8381         | Development network |
| local    | 8372        | 8382         | Local ephemeral network |

### Node Layer (per network, 5 nodes default)

Each network uses a base port with 2 ports per node (HTTP API + Staking P2P).

| Network  | Base Port | Node 1        | Node 2        | Node 3        | Node 4        | Node 5        |
|----------|-----------|---------------|---------------|---------------|---------------|---------------|
| mainnet  | 9630      | 9630/9631     | 9632/9633     | 9634/9635     | 9636/9637     | 9638/9639     |
| testnet  | 9640      | 9640/9641     | 9642/9643     | 9644/9645     | 9646/9647     | 9648/9649     |
| devnet   | 9650      | 9650/9651     | 9652/9653     | 9654/9655     | 9656/9657     | 9658/9659     |
| local    | 9660      | 9660/9661     | 9662/9663     | 9664/9665     | 9666/9667     | 9668/9669     |

### Port Formula

```
HTTP_PORT  = BASE_PORT + (node_index * 2)
STAKING_PORT = BASE_PORT + (node_index * 2) + 1

where node_index = 0..4 for nodes 1-5
```

### Full Port Matrix (All Networks, All Nodes)

```
Network   Node  HTTP   Staking  gRPC   Gateway
-------   ----  -----  -------  -----  -------
mainnet   1     9630   9631     8369   8379
mainnet   2     9632   9633       |      |
mainnet   3     9634   9635       |      |
mainnet   4     9636   9637       |      |
mainnet   5     9638   9639       v      v

testnet   1     9640   9641     8370   8380
testnet   2     9642   9643       |      |
testnet   3     9644   9645       |      |
testnet   4     9646   9647       |      |
testnet   5     9648   9649       v      v

devnet    1     9650   9651     8371   8381
devnet    2     9652   9653       |      |
devnet    3     9654   9655       |      |
devnet    4     9656   9657       |      |
devnet    5     9658   9659       v      v

local     1     9660   9661     8372   8382
local     2     9662   9663       |      |
local     3     9664   9665       |      |
local     4     9666   9667       |      |
local     5     9668   9669       v      v
```

---

## 2. Directory Structure

```
~/.lux/
├── bin/                              # Shared binaries
│   └── luxd                          # Node binary
│
├── plugins/                          # VM plugin management
│   └── current/                      # Active VM plugins (symlinks)
│       ├── srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy  # evm
│       └── <other-vm-ids>
│
├── chains/                           # Canonical chain definitions
│   └── <chain-name>/
│       ├── genesis.json              # Chain genesis
│       ├── config.json               # Chain config
│       └── sidecar.json              # Chain metadata
│
├── keys/                             # Cryptographic keys
│   ├── staker.crt                    # Staking certificate
│   ├── staker.key                    # Staking key
│   └── *.pk                          # Private keys
│
├── runs/                             # Per-network run directories
│   ├── mainnet/
│   │   └── run_20251223_143022/      # Timestamped run
│   │       ├── node1/
│   │       │   ├── db/               # Node database
│   │       │   ├── logs/             # Node logs
│   │       │   ├── staking.crt       # Node certificate
│   │       │   ├── staking.key       # Node TLS key
│   │       │   ├── signer.key        # BLS signer key
│   │       │   └── config.json       # Node config
│   │       ├── node2/
│   │       ├── node3/
│   │       ├── node4/
│   │       ├── node5/
│   │       └── genesis.json          # Network genesis
│   │
│   ├── testnet/
│   │   └── run_<timestamp>/          # Same structure as mainnet
│   │
│   ├── devnet/
│   │   └── run_<timestamp>/
│   │
│   └── local/
│       └── run_<timestamp>/
│
├── server/                           # gRPC server logs
│   ├── mainnet/
│   │   └── server_<timestamp>/
│   │       └── lux-server.log
│   ├── testnet/
│   ├── devnet/
│   └── local/
│
├── snapshots/                        # Network snapshots
│   └── anr-snapshot-<name>/
│
└── config/                           # CLI configuration
    ├── mainnet_network_state.json    # Mainnet state
    ├── testnet_network_state.json    # Testnet state
    ├── devnet_network_state.json     # Devnet state
    ├── local_network_state.json      # Local state
    ├── mainnet.run                   # Mainnet gRPC PID file
    ├── testnet.run                   # Testnet gRPC PID file
    ├── devnet.run                    # Devnet gRPC PID file
    └── local.run                     # Local gRPC PID file
```

---

## 3. Process Hierarchy

### Starting All Networks

```
User Shell
│
├── lux network start --mainnet
│   └── lux-server (gRPC @ 8369)          # Backend controller
│       └── netrunner.Start()
│           ├── luxd node1 (9630/9631)    # Validator 1
│           ├── luxd node2 (9632/9633)    # Validator 2
│           ├── luxd node3 (9634/9635)    # Validator 3
│           ├── luxd node4 (9636/9637)    # Validator 4
│           └── luxd node5 (9638/9639)    # Validator 5
│
├── lux network start --testnet
│   └── lux-server (gRPC @ 8370)
│       └── netrunner.Start()
│           └── [5 luxd nodes @ 9640-9649]
│
├── lux network start --devnet
│   └── lux-server (gRPC @ 8371)
│       └── netrunner.Start()
│           └── [5 luxd nodes @ 9650-9659]
│
└── lux network start (local)
    └── lux-server (gRPC @ 8372)
        └── netrunner.Start()
            └── [5 luxd nodes @ 9660-9669]
```

### Process Types

| Process | Count per Network | Role |
|---------|-------------------|------|
| lux-server | 1 | gRPC control server, manages network lifecycle |
| luxd | 5 (default) | Validator node, runs consensus |

---

## 4. State File Locations

### Run Files (gRPC Server PID)

| Network | Run File | Contents |
|---------|----------|----------|
| mainnet | `~/.lux/config/mainnet.run` | `{"pid": 12345, "networkType": "mainnet", "grpcPort": 8369}` |
| testnet | `~/.lux/config/testnet.run` | `{"pid": 12346, "networkType": "testnet", "grpcPort": 8370}` |
| devnet | `~/.lux/config/devnet.run` | `{"pid": 12347, "networkType": "devnet", "grpcPort": 8371}` |
| local | `~/.lux/config/local.run` | `{"pid": 12348, "networkType": "local", "grpcPort": 8372}` |

### Network State Files

| Network | State File | Purpose |
|---------|------------|---------|
| mainnet | `~/.lux/config/mainnet_network_state.json` | Tracks deployed chains, endpoints |
| testnet | `~/.lux/config/testnet_network_state.json` | Tracks deployed chains, endpoints |
| devnet | `~/.lux/config/devnet_network_state.json` | Tracks deployed chains, endpoints |
| local | `~/.lux/config/local_network_state.json` | Tracks deployed chains, endpoints |

### State File Format

```json
{
  "networkType": "mainnet",
  "networkID": 96369,
  "httpPort": 9630,
  "grpcPort": 8369,
  "gatewayPort": 8379,
  "rootDataDir": "~/.lux/runs/mainnet/run_20251223_143022"
}
```

---

## 5. Shared vs Isolated Resources

### Shared Resources

| Resource | Path | Description |
|----------|------|-------------|
| luxd binary | `~/.lux/bin/luxd` | Single binary, shared by all networks |
| VM plugins | `~/.lux/plugins/current/` | Shared VM binaries (by VM ID) |
| Chain definitions | `~/.lux/chains/<name>/` | Genesis, config, sidecar for each chain |
| Keys | `~/.lux/keys/` | Cryptographic keys |

### Isolated Resources

| Resource | Path Pattern | Description |
|----------|--------------|-------------|
| Node databases | `~/.lux/runs/<network>/run_*/node*/db/` | Per-network, per-node state |
| Node logs | `~/.lux/runs/<network>/run_*/node*/logs/` | Per-network, per-node logs |
| Node config | `~/.lux/runs/<network>/run_*/node*/config.json` | Per-network node configuration |
| Genesis | `~/.lux/runs/<network>/run_*/genesis.json` | Per-network genesis |
| gRPC PID | `~/.lux/config/<network>.run` | Per-network control process |
| Network state | `~/.lux/config/<network>_network_state.json` | Per-network state |

---

## 6. Critical Code Changes Needed

### 6.1 Current State (Already Implemented)

The following are already implemented:

1. **Centralized Port Constants** (`luxfi/constants/server.go`):
   - `GetGRPCPorts(networkType)` - Returns network-specific gRPC ports
   - `GetNetworkPorts(networkType)` - Returns full port configuration

2. **Network-Aware gRPC Server** (`cli/pkg/binutils/processes.go`):
   - `StartServerProcessForNetwork(app, networkType)` - Starts network-specific server
   - `IsServerProcessRunningForNetwork(app, networkType)` - Checks server status
   - `KillgRPCServerProcessForNetwork(app, networkType)` - Stops specific server

3. **Network-Aware CLI** (`cli/cmd/networkcmd/start.go`):
   - `--mainnet`, `--testnet` flags select network type
   - `startPublicNetwork(cfg)` handles network-specific configuration

### 6.2 Changes Required for Full Multi-Network Support

#### A. Port Base Flag Fix

**File**: `cli/cmd/networkcmd/start.go`

Current behavior uses `portBase` flag defaulting to 9630 for all networks. Need to respect network-specific defaults:

```go
// Current (line 456-459)
effectivePortBase := cfg.portBase
if effectivePortBase == 0 {
    effectivePortBase = 9630  // Wrong: same for all networks
}

// Required
effectivePortBase := cfg.portBase
if effectivePortBase == 0 {
    effectivePortBase = constants.GetNetworkPorts(cfg.networkName).NodeBase
}
```

#### B. Network ID in Configuration

**File**: `netrunner/server/network.go`

Ensure network ID is properly propagated from config:

```go
// Line 173-191 - Already handles this via switch on networkID
switch networkID {
case 96369: // LUX Mainnet
    cfg, err = local.NewMainnetConfigFromMnemonic(...)
case 96368: // LUX Testnet
    cfg, err = local.NewTestnetConfigFromMnemonic(...)
default:
    cfg, err = local.NewDefaultConfigNNodes(...)
}
```

#### C. Run Directory Isolation

**File**: `cli/pkg/chain/run.go` (needs creation or modification)

Ensure `EnsureNetworkRunDir` is used consistently:

```go
func EnsureNetworkRunDir(baseDir, networkName string) (string, error) {
    // Creates: <baseDir>/runs/<networkName>/run_<timestamp>/
    // Reuses existing run if node data present
}
```

#### D. Devnet Support

**File**: `cli/cmd/networkcmd/start.go`

Add `--devnet` flag and handler:

```go
var devnet bool

cmd.Flags().BoolVar(&devnet, "devnet", false, "start a devnet node with 5 validators")

func StartDevnet() error {
    return startPublicNetwork(networkConfig{
        networkID:   96370,
        networkName: "devnet",
        portBase:    9650,
    })
}
```

---

## 7. Testing Strategy for Parallel Operation

### 7.1 Unit Tests

**File**: `cli/pkg/binutils/constants_test.go` (exists)

```go
func TestGetGRPCPortsNoOverlap(t *testing.T) {
    networks := []string{"mainnet", "testnet", "devnet", "local"}
    ports := make(map[int]string)
    
    for _, net := range networks {
        p := GetGRPCPorts(net)
        if existing, ok := ports[p.Server]; ok {
            t.Errorf("Port %d used by both %s and %s", p.Server, existing, net)
        }
        ports[p.Server] = net
        ports[p.Gateway] = net
    }
}
```

### 7.2 Integration Tests

**Test Scenario 1: Start All Networks**

```bash
#!/bin/bash
# Start all networks in parallel
lux network start --mainnet &
lux network start --testnet &
lux network start --devnet &
lux network start &  # local

# Wait for all to become healthy
sleep 60

# Verify each network is accessible
curl http://localhost:9630/v1/health  # mainnet
curl http://localhost:9640/v1/health  # testnet
curl http://localhost:9650/v1/health  # devnet
curl http://localhost:9660/v1/health  # local
```

**Test Scenario 2: Deploy Chain to Each Network**

```bash
# Deploy same chain definition to each network
lux chain create mychain --evm

lux chain deploy mychain --network mainnet
lux chain deploy mychain --network testnet
lux chain deploy mychain --network devnet
lux chain deploy mychain --network local

# Verify chain is deployed on each
lux chain list --network mainnet
lux chain list --network testnet
lux chain list --network devnet
lux chain list --network local
```

**Test Scenario 3: Stop/Start Individual Networks**

```bash
# Stop only testnet
lux network stop --testnet

# Verify other networks still running
curl http://localhost:9630/v1/health  # mainnet - should succeed
curl http://localhost:9640/v1/health  # testnet - should fail
curl http://localhost:9650/v1/health  # devnet - should succeed

# Restart testnet
lux network start --testnet

# Verify testnet back up
curl http://localhost:9640/v1/health  # should succeed
```

### 7.3 Stress Test

```bash
# Create 20 chains across 4 networks (5 each)
for net in mainnet testnet devnet local; do
    for i in {1..5}; do
        lux chain create chain-${net}-${i} --evm
        lux chain deploy chain-${net}-${i} --network ${net} &
    done
done

wait

# Verify all chains deployed
for net in mainnet testnet devnet local; do
    count=$(lux chain list --network ${net} | grep -c "chain-${net}")
    if [ "$count" -ne 5 ]; then
        echo "FAIL: Expected 5 chains on ${net}, got ${count}"
    fi
done
```

### 7.4 Resource Isolation Test

```bash
# Start mainnet with specific data
lux network start --mainnet

# Create transactions on mainnet
# ...

# Stop mainnet, start testnet
lux network stop --mainnet
lux network start --testnet

# Verify testnet has clean state (no mainnet data)
# Verify mainnet data still exists on disk

# Restart mainnet, verify state persisted
lux network start --mainnet
```

---

## 8. CLI Commands Reference

### Network Management

```bash
# Start networks
lux network start                    # Start local network
lux network start --mainnet          # Start mainnet (96369)
lux network start --testnet          # Start testnet (96368)
lux network start --devnet           # Start devnet (96370)

# Stop networks
lux network stop                     # Stop local network
lux network stop --mainnet           # Stop mainnet
lux network stop --testnet           # Stop testnet
lux network stop --devnet            # Stop devnet

# Status
lux network status                   # Status of local network
lux network status --mainnet         # Status of mainnet
lux network status --all             # Status of all networks
```

### Chain Deployment

```bash
# Deploy to specific network
lux chain deploy <chain> --network mainnet
lux chain deploy <chain> --network testnet
lux chain deploy <chain> --network devnet
lux chain deploy <chain> --network local
```

---

## 9. Environment Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `NETWORK_TYPE` | Network type for backend | `mainnet` |
| `MNEMONIC` | HD wallet seed for key derivation | `word1 word2 ...` |
| `PRIVATE_KEY` | Direct private key (hex) | `0x123...` |

---

## 10. Troubleshooting

### Port Conflict Detection

```bash
# Check for port conflicts
for port in 8369 8370 8371 8372 8379 8380 8381 8382; do
    if lsof -i :$port > /dev/null 2>&1; then
        echo "Port $port in use"
    fi
done
```

### Process Cleanup

```bash
# Kill all gRPC servers
pkill -f "lux-server"

# Kill all luxd nodes
pkill -f "luxd"

# Clean run files
rm -f ~/.lux/config/*.run

# Clean state files (WARNING: loses deployed chain info)
rm -f ~/.lux/config/*_network_state.json
```

### Log Locations

| Network | Server Log | Node Logs |
|---------|------------|-----------|
| mainnet | `~/.lux/server/mainnet/*/lux-server.log` | `~/.lux/runs/mainnet/*/node*/logs/` |
| testnet | `~/.lux/server/testnet/*/lux-server.log` | `~/.lux/runs/testnet/*/node*/logs/` |
| devnet | `~/.lux/server/devnet/*/lux-server.log` | `~/.lux/runs/devnet/*/node*/logs/` |
| local | `~/.lux/server/local/*/lux-server.log` | `~/.lux/runs/local/*/node*/logs/` |

---

*Document Version: 1.0.0*
*Last Updated: 2025-12-23*
*Compatible with: CLI v1.16.x, Netrunner v1.x.x*
