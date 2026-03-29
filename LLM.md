# Lux Netrunner - AI Assistant Knowledge Base

**Last Updated**: 2026-01-07
**Project**: Lux Netrunner
**Organization**: Lux Network
**Documentation Score**: 85/100

## Project Overview

Lux Netrunner is a powerful network orchestration and testing framework for blockchain development. It provides comprehensive tools for creating, managing, and testing multi-node blockchain networks with support for custom VMs, chains (L2 blockchains), and complex network topologies.

## Essential Commands

### Development
```bash
# Build from source
./scripts/build.sh

# Run tests
go test ./...

# Run E2E tests
./scripts/tests.e2e.sh

# Install binary
curl -sSfL https://raw.githubusercontent.com/luxfi/netrunner/main/scripts/install.sh | sh -s
```

### Basic Operations
```bash
# Start RPC server
netrunner server --port=":8080" --grpc-gateway-port=":8081"

# Start network
netrunner control start --number-of-nodes=5 --node-path=/path/to/luxd

# Check health
netrunner control health

# Get status
netrunner control status

# Save snapshot
netrunner control save-snapshot snapshot-name

# Save hot snapshot (no node stop)
netrunner control save-hot-snapshot snapshot-name

# Load snapshot
netrunner control load-snapshot snapshot-name
```

## Architecture

### Core Components
- **RPC Server**: gRPC server with REST gateway for network control
- **Network Manager**: Orchestrates node lifecycle and network topology
- **Node Process**: Wrapper around blockchain node binaries
- **Snapshot Manager**: State persistence and recovery
- **Test Framework**: Comprehensive testing utilities

### Supported Engines
1. **Lux** - Native Lux blockchain (primary)
2. **Geth** - Ethereum nodes
3. **Optimism** - Layer 2 scaling solution
4. **Eth2** - Ethereum 2.0 beacon chain

### Key Interfaces
- `network.Network` - Network management interface
- `node.Node` - Individual node control
- `NodeProcessCreator` - Node process factory
- `api.Client` - API client interface

## Key Technologies

- **Language**: Go 1.21+
- **RPC**: gRPC with REST gateway
- **Documentation**: Next.js with Fumadocs
- **Testing**: Go testing package, E2E test framework
- **Build**: Make, shell scripts
- **Containerization**: Docker support

## Development Workflow

1. **Setup**: Clone repo, install Go 1.21+
2. **Build**: Run `./scripts/build.sh`
3. **Test**: Run `go test ./...` for unit tests
4. **E2E Test**: Run `./scripts/tests.e2e.sh`
5. **Documentation**: Edit MDX files in `/docs/content/docs/`
6. **Doc Build**: Run `cd docs && pnpm build`

## Documentation Status (85/100)

### ✅ Completed Documentation
1. **Introduction** - Comprehensive overview, quick start, API examples
2. **Network Orchestration** - Complete network management guide
3. **Test Network Setup** - Detailed setup instructions
4. **Configuration Reference** - Enhanced with templates, multi-engine support
5. **Testing Scenarios** - All testing patterns documented
6. **Performance Testing** (NEW) - TPS benchmarking, load testing, reports
7. **Network Simulation** (NEW) - Topologies, failures, chaos testing

### ❌ Missing Documentation
- gRPC/REST API reference
- Multi-network management guide
- Custom VM development tutorial
- Production deployment guide

## Key Capabilities

### Network Simulation
- **Topologies**: Star, Mesh, Hierarchical, Custom
- **Failures**: Random, Cascading, Byzantine, Partition
- **Conditions**: Latency, Bandwidth limits, Packet loss
- **Resources**: CPU/Memory/Disk I/O constraints
- **Time**: Fast-forward and slow-motion simulation

### Performance Testing
- **Metrics**: TPS, BPS, Latency, Resource usage
- **Load Types**: Sustained, Burst, Gradual ramp
- **Analysis**: Real-time monitoring, HTML reports
- **Optimization**: Database tuning, network config

### Configuration Management
- **Hierarchy**: Node → Network → File → Default
- **Features**: Hot-reload, Templates, Multi-engine
- **Validation**: Pre-flight checks, port availability

## Recent Documentation Enhancements (2025-11-12)

### New Documentation Files Created
1. **`performance-testing.mdx`** (1020 lines)
   - Transaction throughput testing
   - Block production benchmarks
   - Network scalability tests
   - Resource monitoring
   - Performance reports and CSV export

2. **`network-simulation.mdx`** (890 lines)
   - Network topology simulation
   - Failure injection scenarios
   - Byzantine behavior testing
   - Network condition simulation
   - Chaos engineering

### Enhanced Documentation
- **`configuration.mdx`** - Added 250+ lines:
  - Dynamic runtime configuration
  - Configuration templates
  - Multi-engine support
  - Monitoring configuration
  - Chaos engineering settings

## Testing Patterns

### Test Types Supported
1. **Unit Testing** - Component validation
2. **Integration Testing** - Multi-node consensus
3. **Performance Testing** - Throughput and latency
4. **Chaos Testing** - Failure injection
5. **Regression Testing** - Snapshot-based
6. **Load Testing** - Sustained and burst

### Example Test Scenarios
```go
// Performance test
TestTransactionThroughput()
TestBlockProductionRate()
TestNetworkScalability()

// Chaos test
SimulateNodeFailures()
SimulateNetworkPartition()
SimulateByzantineNode()

// Load test
TestSustainedLoad()
TestBurstLoad()
```

## Best Practices

1. **Start Simple** - Begin with basic scenarios
2. **Use Snapshots** - Regular automated snapshots
3. **Monitor Everything** - Enable metrics from start
4. **Test Incrementally** - Gradual complexity increase
5. **Document Changes** - Update LLM.md with discoveries

## Common Issues and Solutions

### Issue: Nodes exit with code 1
**Solution**: Check binary path, port availability, genesis configuration

### Issue: Network partition doesn't heal
**Solution**: Ensure proper bootstrap node configuration

### Issue: Low TPS in tests
**Solution**: Optimize consensus parameters, use in-memory DB

## Future Work

### Priority 1 (High)
- [ ] Complete gRPC API documentation
- [ ] Multi-network management guide
- [ ] Production deployment guide

### Priority 2 (Medium)
- [ ] Custom VM development tutorial
- [ ] Bridge functionality docs
- [ ] Video tutorials

### Priority 3 (Low)
- [ ] Performance cookbook
- [ ] Troubleshooting guide
- [ ] Interactive examples

## Context for All AI Assistants

This file (`LLM.md`) is symlinked as:
- `.AGENTS.md`
- `CLAUDE.md`
- `QWEN.md`
- `GEMINI.md`

All files reference the same knowledge base. Updates here propagate to all AI systems.

## Rules for AI Assistants

1. **ALWAYS** update LLM.md with significant discoveries
2. **NEVER** commit symlinked files (.AGENTS.md, CLAUDE.md, etc.) - they're in .gitignore
3. **NEVER** create random summary files - update THIS file
4. **USE** absolute paths when referencing files
5. **TEST** documentation builds before claiming completion

---

**Note**: This file serves as the single source of truth for all AI assistants working on this project.

## Local Network Deployment Status (2025-11-12)

### Current Achievement
✅ Successfully deployed 5-node local network using direct luxd execution
- **Network ID**: 12345
- **Binary**: /Users/z/work/lux/node/build/luxd v1.20.1
- **Data Directory**: /tmp/lux-5node-final/
- **Node Ports**: 9650, 9660, 9670, 9680, 9690 (HTTP), 9651, 9661, 9671, 9681, 9691 (Staking)

### Bootstrap Status
✅ **P-Chain**: All 5 nodes bootstrapped successfully
❌ **X-Chain**: Not bootstrapping (nodes not validators)
❌ **C-Chain**: Not bootstrapping (nodes not validators)  
❌ **Q-Chain**: Not bootstrapping (nodes not validators)

### Root Cause Analysis
**Issue**: Nodes are not configured as validators in genesis
**Error**: "node is not a validator" when attempting to get uptime
**Impact**: Only P-Chain can bootstrap. X, C, and Q chains require validator status to participate in consensus.

### Solution Path
To achieve full 4-chain bootstrap, we need to:
1. Generate genesis file with 5 pre-configured validators
2. Extract node IDs and BLS keys from running nodes
3. Include these node IDs in genesis validator set
4. Restart network with validator-enabled genesis

### Build Status
✅ **Netrunner**: Built successfully (`/Users/z/work/lux/netrunner/bin/netrunner` - 43MB)
✅ **Lux-CLI**: Built successfully (`/Users/z/work/lux/cli/bin/lux` - 96MB)
✅ **Luxd Node**: Built successfully (`/Users/z/work/lux/node/build/luxd` - 57MB)

### Deployment Methods Attempted
1. ❌ **Netrunner Server/Client**: 503 error, network terminated prematurely
2. ❌ **Lux-CLI**: Corrupted snapshot, node3 stopped unexpectedly
3. ✅ **Direct luxd execution**: Successful P-Chain bootstrap

### Next Steps
1. Create genesis tool or use existing `github.com/luxfi/genesis` package
2. Generate 5-validator genesis file
3. Configure nodes with proper staking keys
4. Restart network and verify <60s bootstrap for all 4 chains
5. Build e2e tests for X, C, Q chain operations

### Files Created/Modified
- `/tmp/deploy-network.sh` - 5-node deployment script
- `/tmp/lux-5node-final/` - Network data directory with 5 nodes
- `/tmp/start_5node_network.go` - Attempted netrunner Go integration

### Key Learnings
1. P-Chain bootstraps without validator status (platform chain special case)
2. X, C, Q chains require nodes to be in validator set
3. Genesis must be created BEFORE node deployment for validator networks
4. Direct luxd execution is most reliable for custom network configurations

## Local Network Deployment Issues and Solutions (2025-11-12)

### Critical Version/Release Issues Discovered

#### 1. Version Mismatch Between Releases and Tags
**Problem**: Latest GitHub release (v1.14.0) doesn't match latest Git tag (v1.20.3)
- **File**: `/Users/z/work/lux/node/.github/workflows/release.yml`
- **Impact**: Users/tools download v1.14.0 binaries when expecting v1.20.3 code
- **Root Cause**: No automated GitHub release workflow when tags are pushed
- **Solution**: Created workflow to automatically create GitHub releases on tag push

#### 2. Published v1.20.3 Missing Critical Code
**Problem**: v1.20.3 release tarball missing WarpSet/WarpValidator implementations
- **Files Missing**:
  - `consensus/validator/validators.go` (WarpSet, WarpValidator types)
  - `consensus/validator/validator.go` (GetWarpValidatorSet methods)
- **Impact**: Code that compiles locally fails when users download release
- **Verification**: Release tarball extracted and verified missing implementations
- **Solution**: Created new v1.20.4 tag and release with complete code

#### 3. lux-cli Hardcoded Wrong Versions
**Problem**: lux-cli downloads wrong binary versions
- **File**: `/Users/z/work/lux/cli/pkg/constants/constants.go`
- **Issues**:
  - Default luxd version: `v2.0.0` (doesn't exist)
  - NetRunner version: `v1.13.0` (too old)
  - BuildDir: `.lux/bin/luxgo` (should be `luxfi`)
- **Impact**: `lux network start` downloads broken/wrong binaries
- **Solution**:
  ```go
  DefaultNodeVersion    = "v1.20.4"  // was v2.0.0
  DefaultNetRunnerVersion = "v1.20.4"  // was v1.13.0
  BuildDir = ".lux/bin/luxfi"  // was luxgo
  ```

#### 4. No Automated Release Process
**Problem**: Tags created but no GitHub releases published
- **Missing**: Automated workflow to:
  - Build binaries for all platforms (darwin-amd64/arm64, linux-amd64/arm64)
  - Create GitHub release with binaries attached
  - Generate checksums and release notes
- **Solution**: Created `.github/workflows/release.yml` with:
  - Trigger on tag push (v*)
  - Multi-platform binary builds
  - Automated release creation
  - Checksum generation

### Port Configuration Issues

#### Default Port Series Wrong
**Problem**: Code uses 964x series, should use 963x
- **Files**:
  - `/Users/z/work/lux/netrunner/local/network.go` - Node creation
  - `/Users/z/work/lux/netrunner/cmd/control/start/start.go` - CLI defaults
- **Expected Layout**:
  ```
  Node 1: HTTP=9630, Staking=9631
  Node 2: HTTP=9640, Staking=9641
  Node 3: HTTP=9650, Staking=9651
  Node 4: HTTP=9660, Staking=9661
  Node 5: HTTP=9670, Staking=9671
  ```
- **Issue**: Code was incrementing from 9650 (964x), not 9630 (963x)
- **Solution**: Changed `baseHTTPPort` from 9650 to 9630 in network.go

### Genesis Configuration Issues

#### 1. Invalid Bech32 Address Checksums
**Problem**: Manual genesis.json has invalid address checksums
- **Error Message**:
  ```
  invalid checksum (expected v3r5gtp, got 06g97jh) for "lux1w5f4p2cq7ajjpphny0k8fhkp3wr7a7ggv3r5gtp"
  ```
- **Root Cause**: Manual address creation without proper Bech32 checksum calculation
- **Files Affected**: Any manually created `genesis.json`
- **Solution**: Use proper Bech32 encoding library:
  ```go
  import "github.com/luxfi/crypto/address"
  addr, err := address.Format("lux", hrp, publicKeyHash)
  ```

#### 2. Missing Validator Configuration
**Problem**: Nodes not configured as validators in genesis
- **Error**: "node is not a validator" when querying uptime
- **Impact**:
  - P-Chain bootstraps (doesn't require validator status)
  - X, C, Q chains FAIL (require validator status for consensus)
- **Required Fields in Genesis**:
  ```json
  "initialStakers": [
    {
      "nodeID": "NodeID-...",
      "rewardAddress": "lux1...",
      "delegationFee": 20000,
      "stakeAmount": 2000000000000000
    }
  ]
  ```
- **Solution**: Generate genesis with all 5 node IDs in validator set

#### 3. Genesis Generation Tool Needed
**Problem**: No automated way to create valid genesis
- **Manual Process Error-Prone**:
  - Calculate Bech32 checksums
  - Format addresses correctly
  - Set proper stake amounts
  - Include all node IDs
- **Solution Created**: `/Users/z/work/lux/genesis/cmd/genesis-gen/main.go`
  - Generates 5-validator genesis automatically
  - Proper Bech32 address encoding
  - Configurable network ID
  - Pre-funded accounts for testing
  - Outputs to stdout or file

### Validator Setup Requirements

#### Bootstrap Chain Requirements
**P-Chain (Platform Chain)**:
- ✅ Bootstraps WITHOUT validator status
- ✅ Can sync from network as non-validator
- Special case: platform chain manages validators

**X-Chain (Asset Exchange)**:
- ❌ Requires validator status to participate
- ❌ Won't bootstrap without being in validator set
- Error: "node is not a validator"

**C-Chain (Smart Contracts)**:
- ❌ Requires validator status to participate
- ❌ Won't bootstrap without being in validator set
- EVM chain requires consensus participation

**Q-Chain (Quantum-Resistant)**:
- ❌ Requires validator status to participate
- ❌ Won't bootstrap without being in validator set
- New chain requires validator consensus

#### Validator Genesis Requirements
```json
{
  "initialStakers": [
    {
      "nodeID": "NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg",
      "rewardAddress": "lux1wst8jt3z3fm9ce0z6akj3266zmgccdp03hjlaj",
      "delegationFee": 20000,
      "stakeAmount": 2000000000000000
    }
  ]
}
```

**Critical Fields**:
- `nodeID` - From node's staking certificate
- `rewardAddress` - Bech32-encoded with correct checksum
- `stakeAmount` - Minimum 2000 LUX (2000000000000000 nLUX)
- `delegationFee` - Commission percentage (20000 = 20%)

### Solutions Implemented This Session

#### 1. Fixed lux-cli Version References
**File**: `/Users/z/work/lux/cli/pkg/constants/constants.go`
```go
// Before
DefaultNodeVersion      = "v2.0.0"
DefaultNetRunnerVersion = "v1.13.0"
BuildDir               = ".lux/bin/luxgo"

// After
DefaultNodeVersion      = "v1.20.4"
DefaultNetRunnerVersion = "v1.20.4"
BuildDir               = ".lux/bin/luxfi"
```

#### 2. Created GitHub Release Workflow
**File**: `/Users/z/work/lux/node/.github/workflows/release.yml`
- Triggers on tag push (v*)
- Builds for darwin-amd64, darwin-arm64, linux-amd64, linux-arm64
- Creates GitHub release with binaries
- Generates SHA256 checksums
- Auto-populates release notes

#### 3. Fixed Default Port Configuration
**File**: `/Users/z/work/lux/netrunner/local/network.go`
```go
// Changed base port from 9650 to 9630
const baseHTTPPort = 9630
const baseStakingPort = 9631
```

#### 4. Created Genesis Generation Tool
**File**: `/Users/z/work/lux/genesis/cmd/genesis-gen/main.go`
- Generates valid Bech32 addresses
- Creates 5-validator genesis automatically
- Configurable network ID
- Pre-funded test accounts
- Proper stake amounts and fees

#### 5. Created New v1.20.4 Release
**Actions Taken**:
- Tagged commit with complete WarpSet/WarpValidator code
- Pushed tag to GitHub: `git push origin v1.20.4`
- Automated workflow created GitHub release
- Release includes all required implementations

### Deployment Process (Updated)

#### Method 1: Netrunner (Recommended - Easiest)
```bash
# Build netrunner
cd /Users/z/work/lux/netrunner
go build -o bin/netrunner

# Start network (handles genesis automatically)
./bin/netrunner control start \
  --number-of-nodes=5 \
  --node-path=/Users/z/work/lux/node/build/luxd \
  --blockchain-specs='[{"vm_name":"qemuvm","genesis":"/path/to/genesis.json"}]'
```

#### Method 2: Genesis-Gen Tool + Manual Deployment
```bash
# Generate valid genesis
cd /Users/z/work/lux/genesis
go run cmd/genesis-gen/main.go \
  --network-id=12345 \
  --num-validators=5 \
  --output=/tmp/genesis.json

# Start nodes manually with generated genesis
/Users/z/work/lux/node/build/luxd \
  --network-id=12345 \
  --genesis=/tmp/genesis.json \
  --http-port=9630 \
  --staking-port=9631 \
  --data-dir=/tmp/node1
```

#### Method 3: Lux-CLI (After Version Fixes)
```bash
# Build latest lux-cli with version fixes
cd /Users/z/work/lux/cli
go build -o bin/lux

# Start network (now downloads correct v1.20.4 binary)
./bin/lux network start
```

### Manual Deployment Checklist

When deploying manually (without netrunner):

1. **Generate Valid Genesis**:
   ```bash
   go run genesis-gen/main.go --network-id=12345 --num-validators=5
   ```

2. **Verify Genesis Addresses**:
   - All addresses have correct Bech32 checksums
   - InitialStakers array has 5 entries (one per node)
   - Stake amounts ≥ 2000 LUX

3. **Start Bootstrap Node First**:
   ```bash
   luxd --network-id=12345 --genesis=/tmp/genesis.json \
        --http-port=9630 --staking-port=9631 \
        --data-dir=/tmp/node1 \
        --bootstrap-ips= --bootstrap-ids=
   ```

4. **Get Bootstrap Node ID**:
   ```bash
   curl http://localhost:9630/ext/info | jq .result.nodeID
   ```

5. **Start Remaining Nodes**:
   ```bash
   luxd --network-id=12345 --genesis=/tmp/genesis.json \
        --http-port=9640 --staking-port=9641 \
        --data-dir=/tmp/node2 \
        --bootstrap-ips=127.0.0.1:9631 \
        --bootstrap-ids=<node1-id>
   ```

6. **Verify All Chains Bootstrap**:
   ```bash
   # Should show isBootstrapped: true for P, X, C, Q
   curl http://localhost:9630/ext/info | jq
   ```

### Error Messages and Solutions

#### "invalid checksum (expected..., got...)"
**Cause**: Manually created address without proper Bech32 encoding
**Solution**: Use `github.com/luxfi/crypto/address` package
```go
import "github.com/luxfi/crypto/address"
addr, _ := address.Format("lux", hrp, pubKeyHash)
```

#### "node is not a validator"
**Cause**: Node's NodeID not in genesis initialStakers array
**Solution**: Include node's NodeID in genesis validator set
```json
"initialStakers": [{"nodeID": "NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg", ...}]
```

#### "X-Chain not bootstrapping"
**Cause**: Node not configured as validator (X-Chain requires consensus participation)
**Solution**: Restart with validator-enabled genesis containing node's ID

#### "download failed: release not found"
**Cause**: lux-cli trying to download non-existent version (v2.0.0)
**Solution**: Update constants.go with DefaultNodeVersion = "v1.20.4"

#### "503 Service Unavailable" from netrunner
**Cause**: Network terminated prematurely due to node startup failure
**Solution**: Check node binary path, port availability, genesis validity

### Files Modified This Session

#### lux-cli Version Fixes
- `/Users/z/work/lux/cli/pkg/constants/constants.go`

#### Release Workflow
- `/Users/z/work/lux/node/.github/workflows/release.yml` (created)

#### Port Configuration
- `/Users/z/work/lux/netrunner/local/network.go`
- `/Users/z/work/lux/netrunner/cmd/control/start/start.go`

#### Genesis Tools
- `/Users/z/work/lux/genesis/cmd/genesis-gen/main.go` (created)

#### Documentation
- `/Users/z/work/lux/netrunner/CLAUDE.md` (this file)

### Key Takeaways for Future Sessions

1. **Always verify GitHub release matches Git tag** - Code can compile locally but be missing in published releases

2. **Use 963x port series** - Not 964x (9630/9631 for first node)

3. **Validator status is critical** - Only P-Chain bootstraps without it; X/C/Q require validator genesis

4. **Manual genesis is error-prone** - Use genesis-gen tool or netrunner automatic generation

5. **Test with fresh data dirs** - Corrupted state causes mysterious failures

6. **Bootstrap nodes must start first** - Other nodes need bootstrap-ips/bootstrap-ids to connect

7. **Version constants matter** - Wrong version in lux-cli breaks binary downloads

8. **Bech32 checksums are mandatory** - Manual address creation always fails validation

### Success Criteria for Local Network

✅ **Achieved**:
- 5-node network starts successfully
- P-Chain bootstraps (<60s)
- All nodes visible to each other
- HTTP APIs responding

❌ **Remaining**:
- X-Chain bootstrap (requires validator genesis)
- C-Chain bootstrap (requires validator genesis)
- Q-Chain bootstrap (requires validator genesis)
- Cross-chain transactions
- Consensus participation

**To Complete**: Deploy with genesis-gen tool that includes all 5 node IDs as validators


## Genesis cChainGenesis Format Fix (2025-11-16)

### Issue
**Error**: `could not unmarshal genesis JSON: json: cannot unmarshal object into Go struct field UnparsedConfig.cChainGenesis of type string`

**Root Cause**: The `cChainGenesis` field in netrunner's default genesis was an object instead of a JSON string.

**Location**: `/Users/z/work/lux/netrunner/network/default/genesis.json`

**Fix Applied**: Converted `cChainGenesis` object to JSON string:
```python
import json
with open('network/default/genesis.json', 'r') as f:
    genesis = json.load(f)
if isinstance(genesis['cChainGenesis'], dict):
    genesis['cChainGenesis'] = json.dumps(genesis['cChainGenesis'], separators=(',', ':'))
with open('network/default/genesis.json', 'w') as f:
    json.dump(genesis, f, indent=2)
```

**Result**: Nodes can now start successfully with netrunner default genesis

### Peer Connectivity Requirements

**Discovery**: Bootstrap configuration in YAML manifests wasn't being passed to luxd command lines.

**Required Flags**:
- `--bootstrap-ips=127.0.0.1:9631` - Bootstrap node's staking port
- `--bootstrap-ids=NodeID-7Xhw2...` - Bootstrap node's NodeID

**Solution**: Created `/scripts/start-local-network.sh` that properly configures bootstrap for all nodes

**Verification**:
```bash
# Check if bootstrap flags are in process command line
ps aux | grep luxd | grep 9640 | grep bootstrap-ips
```

### Staking Certificate Reuse

**Discovery**: Netrunner has pre-configured staking certificates in `/local/default/node1-5/`

**Files Per Node**:
- `staking.crt` - TLS certificate (determines NodeID)
- `staking.key` - TLS private key
- `signer.key` - BLS signing key (32 bytes)

**NodeIDs Generated** (from netrunner default certificates):
- Node 1: `NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg`
- Node 2: `NodeID-MFrZFVCXPv5iCn6M9K6XduxGTYp891xXZ`
- Node 3: `NodeID-NFBbbJ4qCmNaCzeW7sxErhvWqvEQMnYcN`
- Node 4: `NodeID-GWPcbFJZFfZreETSoWjPimr846mXEKCtu`
- Node 5: `NodeID-P7oB2McjBGgW2NXXWVYjV8JEDFoW9xDE5`

**Usage Pattern**:
```bash
# Copy pre-configured keys to node data directory
for i in {1..5}; do
    mkdir -p $BASE/node$i
    cp $NETRUNNER/local/default/node$i/*.{crt,key} $BASE/node$i/
done

# Start node with explicit staking key paths
$LUXD --staking-tls-cert-file=$BASE/node1/staking.crt \
      --staking-tls-key-file=$BASE/node1/staking.key \
      --staking-signer-key-file=$BASE/node1/signer.key
```

**Benefit**: Ensures NodeIDs match netrunner default genesis validators

---

## Lux CLI Network Architecture Proposal (2025-12-19)

### Executive Summary

This document proposes a production-ready architecture for mainnet network management in the Lux CLI. The design focuses on proper validator key management, 5-node consensus with staking keys from `~/.lux/keys/`, and clean separation from the legacy "local" development command.

### Current State Analysis

#### What Exists

1. **networkcmd/** - Already has `start`, `stop`, `clean`, `status` commands with partial mainnet support
2. **localcmd/** - Minimal PoA development network (single-node, chain ID 1337) - **TO BE REMOVED**
3. **netrunner integration** - CLI uses netrunner's gRPC server for network orchestration
4. **Key infrastructure** - Keys exist at `~/.lux/keys/node-{0-4}/staking/` with `staker.crt` and `staker.key`
5. **Validator config** - `~/.lux/keys/mainnet_validators.json` contains P-chain addresses and private keys

#### Current Issues

1. **localcmd confusion** - The `lux local start` command creates a single-node PoA network for development (chain ID 1337), which is conceptually different from the mainnet/testnet multi-validator network
2. **Missing proper staking key integration** - Current `StartMainnet()` doesn't load the pre-generated staking keys from `~/.lux/keys/node-{0-4}/`
3. **No snapshot management** - Missing `snapshot save` and `snapshot restore` subcommands
4. **Incomplete node configuration** - Nodes aren't configured with proper staking certificates

### Proposed Architecture

#### 1. Command Structure

```
lux network
    start [--mainnet|--testnet]   # Start 5-node network with validator keys
    stop                          # Stop network, preserve state
    status                        # Show network status
    clean                         # Clean all network data
    snapshot
        save <name>               # Save current state to named snapshot
        restore <name>            # Restore from named snapshot
        list                      # List available snapshots
        delete <name>             # Delete a snapshot
```

#### 2. Remove `localcmd/`

The `lux local` command should be **removed entirely**. Rationale:

- It creates confusion between "local development" and "local mainnet simulation"
- The 1337 chain ID PoA network is superseded by proper multi-validator networks
- Users should use `lux network start` for all local network needs
- The `--mainnet` flag already provides mainnet genesis (network ID 96369)
- The `--testnet` flag provides testnet genesis (network ID 96368)

**Files to remove:**
- `/Users/z/work/lux/cli/cmd/localcmd/local.go`
- Remove `localcmd.NewCmd(app)` from `/Users/z/work/lux/cli/cmd/root.go` (line 131)

#### 3. Key Directory Structure (Already Exists)

```
~/.lux/keys/
    node-0/
        staking/
            staker.crt      # TLS certificate for staking
            staker.key      # TLS private key for staking
        bls/               # BLS keys for consensus signatures
        ec/                # EC keys
        mldsa/             # ML-DSA post-quantum keys
        rt/                # Ringtail keys
    node-1/ ... node-4/    # Same structure
    mainnet_validators.json  # P-chain addresses and private keys
```

#### 4. Enhanced Network Start with Staking Keys

**New types in `pkg/network/types.go`:**

```go
package network

// ValidatorConfig represents a single validator's configuration
type ValidatorConfig struct {
    NodeIndex    int
    StakingCert  string // Path to staker.crt
    StakingKey   string // Path to staker.key
    BLSKey       string // Path to BLS key
    NodeID       string // Computed from staking key
    PChainAddr   string // P-chain address for rewards
    HTTPPort     int    // HTTP API port
    StakingPort  int    // P2P staking port
}

// NetworkConfig represents the full network configuration
type NetworkConfig struct {
    NetworkID     uint32
    Validators    []ValidatorConfig
    GenesisPath   string
    RootDataDir   string
    PortBase      int
    DBEngine      string // pebble, leveldb, or badgerdb
}

// LoadValidatorConfigs loads all 5 validator configurations from disk
func LoadValidatorConfigs(keyPath string, portBase int) ([]ValidatorConfig, error) {
    validators := make([]ValidatorConfig, 5)
    for i := 0; i < 5; i++ {
        nodeDir := filepath.Join(keyPath, fmt.Sprintf("node-%d", i))
        stakingDir := filepath.Join(nodeDir, "staking")

        certPath := filepath.Join(stakingDir, "staker.crt")
        keyPath := filepath.Join(stakingDir, "staker.key")

        validators[i] = ValidatorConfig{
            NodeIndex:   i,
            StakingCert: certPath,
            StakingKey:  keyPath,
            HTTPPort:    portBase + (i * 2),      // 9630, 9632, 9634, 9636, 9638
            StakingPort: portBase + (i * 2) + 1,  // 9631, 9633, 9635, 9637, 9639
        }
    }
    return validators, nil
}
```

#### 5. New Snapshot Commands

**New file: `/Users/z/work/lux/cli/cmd/networkcmd/snapshot.go`:**

```go
package networkcmd

func newSnapshotCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "snapshot",
        Short: "Manage network snapshots",
    }
    cmd.AddCommand(newSnapshotSaveCmd())
    cmd.AddCommand(newSnapshotRestoreCmd())
    cmd.AddCommand(newSnapshotListCmd())
    cmd.AddCommand(newSnapshotDeleteCmd())
    return cmd
}

func newSnapshotSaveCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "save <name>",
        Short: "Save current network state to a named snapshot",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            cli, _ := binutils.NewGRPCClient()
            _, err := cli.SaveSnapshot(ctx, args[0])
            return err
        },
    }
}

func newSnapshotRestoreCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "restore <name>",
        Short: "Restore network from a named snapshot",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            cli, _ := binutils.NewGRPCClient()
            _, err := cli.LoadSnapshot(ctx, args[0], opts...)
            return err
        },
    }
}
```

#### 6. Port Allocation Strategy

| Node | HTTP Port | Staking Port |
|------|-----------|--------------|
| node-0 | 9630 | 9631 |
| node-1 | 9632 | 9633 |
| node-2 | 9634 | 9635 |
| node-3 | 9636 | 9637 |
| node-4 | 9638 | 9639 |

### File Structure Changes Summary

#### Files to Remove
```
cmd/localcmd/local.go                    # Remove entire localcmd
```

#### Files to Modify
```
cmd/root.go                              # Remove localcmd.NewCmd(app) import and call
cmd/networkcmd/network.go                # Add snapshot subcommand
cmd/networkcmd/start.go                  # Integrate staking key loading
cmd/networkcmd/status.go                 # Enhance to show validator IDs
```

#### Files to Add
```
cmd/networkcmd/snapshot.go               # New snapshot save/restore/list/delete
pkg/network/types.go                     # New ValidatorConfig, NetworkConfig types
pkg/network/keys.go                      # New key loading utilities
```

### Configuration Flow

```
User runs: lux network start --mainnet

1. Load validator keys from ~/.lux/keys/node-{0-4}/staking/
2. Validate all 5 validators have staker.crt and staker.key
3. Build per-node configurations with:
   - Staking TLS cert/key paths
   - Unique HTTP ports (9630, 9632, 9634, 9636, 9638)
   - Unique staking ports (9631, 9633, 9635, 9637, 9639)
   - BadgerDB database backend
4. Start netrunner gRPC server if not running
5. Call netrunner.Start() with:
   - Global config: network-id=96369, sybil-protection-enabled=true
   - Custom node configs for each validator
   - Genesis from luxfi/genesis package (network ID 96369)
6. Wait for all 5 validators to become healthy
7. Display endpoints table

User runs: lux network snapshot save checkpoint1

1. Connect to running netrunner
2. Call SaveSnapshot("checkpoint1")
3. Snapshot saved to ~/.lux/snapshots/anr-snapshot-checkpoint1/

User runs: lux network stop

1. Call SaveSnapshot("default-XXX") to preserve state
2. Kill netrunner server process
3. "Network stopped successfully"

User runs: lux network snapshot restore checkpoint1

1. Start netrunner server
2. Call LoadSnapshot("checkpoint1", opts...)
3. Wait for healthy
4. Display endpoints
```

### Implementation Priority

#### Phase 1: Core Network Start (HIGH PRIORITY)
1. Implement `pkg/network/types.go` and `pkg/network/keys.go`
2. Update `cmd/networkcmd/start.go` to load staking keys
3. Test 5-node mainnet startup with proper validator keys

#### Phase 2: Snapshot Management (HIGH PRIORITY)
1. Add `cmd/networkcmd/snapshot.go`
2. Register snapshot subcommand in `network.go`
3. Test save/restore workflow

#### Phase 3: Cleanup (MEDIUM PRIORITY)
1. Remove `cmd/localcmd/local.go`
2. Remove from `cmd/root.go`
3. Update documentation

### Security Considerations

1. **Key Protection**: Staking keys at `~/.lux/keys/` should have 0600 permissions
2. **No EWOQ Keys**: As per CLAUDE.md, absolutely no EWOQ keys
3. **Real Genesis**: Use luxfi/genesis package, never generate custom genesis
4. **Sybil Protection**: Always enable sybil-protection-enabled for mainnet

## RLP Block Import Files (IMPORTANT - Do Not Lose!)

### State File Locations

**All RLP files are at:** `~/work/lux/state/rlp/`

| Chain | Network | File Path | Size |
|-------|---------|-----------|------|
| Zoo | Mainnet | `~/work/lux/state/rlp/zoo-mainnet/zoo-mainnet-200200.rlp` | 1.3M |
| Zoo | Testnet | `~/work/lux/state/rlp/zoo-testnet/zoo-testnet-200201.rlp` | - |
| Lux (C-Chain) | Mainnet | `~/work/lux/state/rlp/lux-mainnet/lux-mainnet-96369.rlp` | 1.2G |
| Lux (C-Chain) | Testnet | `~/work/lux/state/rlp/lux-testnet/lux-testnet-96368.rlp` | - |
| SPC | Mainnet | `~/work/lux/state/rlp/spc-mainnet/spc-mainnet-36911.rlp` | - |

### Chain IDs
- **Zoo Mainnet**: 200200
- **Zoo Testnet**: 200201
- **Lux Mainnet (C-Chain)**: 96369
- **Lux Testnet (C-Chain)**: 96368
- **SPC Mainnet**: 36911

### Import Commands

```bash
# Import Zoo mainnet blocks (~1.3M blocks)
curl -X POST http://127.0.0.1:9650/ext/bc/Zoo/rpc \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"debug_importRLPBlocks","params":["'$(base64 -i ~/work/lux/state/rlp/zoo-mainnet/zoo-mainnet-200200.rlp)'"],"id":1}'

# Import C-chain mainnet blocks (~1.2G)
curl -X POST http://127.0.0.1:9650/ext/bc/C/rpc \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"debug_importRLPBlocks","params":["'$(base64 -i ~/work/lux/state/rlp/lux-mainnet/lux-mainnet-96369.rlp)'"],"id":1}'
```

### Key Details
- RLP files contain encoded blocks for historical chain state replay
- Import order: Import Zoo first (smaller), then C-Chain (larger)
- Network must be running with Zoo chain discovered before import

---

## Network ID vs Chain ID Fix (2025-12-23)

### Critical Bug Fixed

**Problem**: Multiple functions in `genesis_config.go` were using C-Chain IDs (`configs.MainnetChainID`/`configs.TestnetChainID` = 96369/96368) when they should use Network IDs (`constants.MainnetID`/`constants.TestnetID` = 1/2).

**Impact**: Networks would fail to start with "network ID mismatch: expected 2, got 96368" or similar errors.

### Constants Clarification

| Constant | Value | Purpose |
|----------|-------|---------|
| `constants.MainnetID` | 1 | P2P Network ID for mainnet |
| `constants.TestnetID` | 2 | P2P Network ID for testnet |
| `constants.CustomID` | 1337 | P2P Network ID for local dev |
| `configs.MainnetChainID` | 96369 | EVM Chain ID for C-Chain mainnet |
| `configs.TestnetChainID` | 96368 | EVM Chain ID for C-Chain testnet |

### Files Fixed

**`/Users/z/work/lux/netrunner/local/genesis_config.go`**:

| Function | Before | After |
|----------|--------|-------|
| `NewMainnetConfigWithKeys()` (line 644) | `configs.MainnetChainID` | `constants.MainnetID` |
| `NewTestnetConfigWithKeys()` (line 652) | `configs.TestnetChainID` | `constants.TestnetID` |
| `NewMainnetConfigFromMnemonic()` (line 985) | `configs.MainnetChainID` | `constants.MainnetID` |
| `NewTestnetConfigFromMnemonic()` (line 990) | `configs.TestnetChainID` | `constants.TestnetID` |

**Earlier fixes in same session** (from conversation summary):
- Line 58: `configs.LocalID` → `configs.CustomID`
- Line 335: `configs.LocalID` → `configs.CustomID`
- Line 996: `configs.LocalID` → `configs.CustomID`

**`/Users/z/work/lux/netrunner/network/config.go`**:
- Line 125: `constants.LocalID` → `constants.CustomID`

**`/Users/z/work/lux/netrunner/server/network.go`**:
- Line 152: `luxd_constants.LocalID` → `luxd_constants.CustomID`
- Lines 174, 182: Changed from hardcoded 96369/96368 to `luxd_constants.MainnetID/TestnetID`

### Zoo Chain Deployment Success

After fixes, successfully deployed Zoo chain on mainnet:
- **Network ID**: 1 (mainnet)
- **Zoo Chain ID**: 200200 (0x30e08)
- **Genesis Hash**: `0x7c548af47de27560779ccc67dda32a540944accc71dac3343da3b9cd18f14933`
- **Treasury**: ~500M ZOO at `0x9011E888251AB053B7bD1cdB598Db4f9DEd94714`
- **RPC Endpoint**: `http://localhost:9630/ext/bc/zoo/rpc`

### RLP Block Import Limitation

**Discovery**: The L2 EVM doesn't support `admin_importChain` RPC method. Blocks in L2 EVM chains come through Lux consensus, not direct import.

**Available RPC modules on Zoo chain**: eth, net, rpc, web3 (no admin or debug)

**Workaround**: For testing, deploy fresh AMM contracts since genesis state is identical.

### Example Usage

```go
// Correct - uses Network ID
cfg, err := local.NewMainnetConfigFromMnemonic(binaryPath, 5)

// Wrong - would use Chain ID instead of Network ID
// Previously: local.NewConfigFromMnemonic(binaryPath, configs.MainnetChainID, 5)
```

---

### Conclusion

This architecture provides a clean, production-ready approach to mainnet network management:

- **Clear command structure**: `lux network {start|stop|status|clean|snapshot}`
- **Proper key integration**: Uses existing staking keys from `~/.lux/keys/`
- **State management**: Snapshot save/restore for reproducible testing
- **No confusion**: Removes legacy `lux local` command entirely
- **Mainnet-first**: Defaults to mainnet configuration with 5 validators

---

## Native BadgerDB Snapshot Fix (2026-01-22)

### Problem
Snapshot creation was using 75GB directory copies instead of efficient native BadgerDB incremental backups.

### Root Cause
1. **Node backup service not initialized**: `DataDir` wasn't passed to admin service config in `node/node.go`
2. **CLI stop command order wrong**: Tried to snapshot databases directly while nodes had exclusive locks

### Files Modified

**`/Users/z/work/lux/node/node/node.go`**:
Added `DataDir` to admin service config to enable backup service:
```go
service := admin.New(admin.Config{
    // ... other fields ...
    DataDir: n.Config.DatabaseConfig.Path,  // ADDED
})
```

**`/Users/z/work/lux/netrunner/local/snapshot.go`**:
Cleaned up to single unified approach using native BadgerDB via admin.snapshot API.
- Removed legacy `copyDir` function
- Removed `hotSnapshotManifest` types and duplicate functions
- Reduced from 984 lines to 629 lines (36% reduction)

**`/Users/z/work/lux/cli/cmd/networkcmd/stop.go`**:
Fixed to use gRPC method (admin.snapshot API) for hot snapshots:
```go
// Before (wrong - tried direct file access while nodes running)
if err := saveNetworkNative(stopNetworkType, snapshotName, useIncremental); err != nil {
    if err := saveNetworkForType(stopNetworkType); err != nil { ... }
}

// After (correct - uses admin.snapshot API via gRPC)
if err := saveNetworkForType(stopNetworkType); err != nil {
    ux.Logger.PrintToUser("Warning: failed to save snapshot: %v", err)
}
```

### Results
- **Before**: 75GB snapshots (directory copy)
- **After**: 132KB snapshots (native BadgerDB incremental)
- **Reduction**: 99.9%+

### Verified Functionality (2026-01-22)

| Feature | Status | Notes |
|---------|--------|-------|
| 5-node testnet | ✅ | Ports 9640-9648 |
| 5-node mainnet | ✅ | Ports 9630-9638 |
| Hot snapshots | ✅ | Via admin.snapshot API |
| Incremental backups | ✅ | 5KB per node compressed |
| Resume from data | ✅ | Auto-detects existing run |
| Track all chains | ✅ | `track-chains="all"` |
| All 11 chains | ✅ | P,C,X,Q,A,B,T,Z,G,K,D |

### Snapshot Format (v2)
```json
{
    "version": 2,
    "network": "testnet",
    "timestamp": 1769140260,
    "created_at": "2026-01-23T03:51:00Z",
    "nodes": {
        "node1": {
            "db_version": 11,
            "incremental_from": 0,
            "backup_file": "node1.backup.zst",
            "compressed_size": 5362
        }
        // ... node2-node5
    }
}
```

### Commands Reference

```bash
# Start 5-node testnet
lux network start --testnet

# Stop with snapshot
lux network stop --testnet --force --snapshot-name my-snapshot

# Resume from snapshot (uses existing data)
lux network start --testnet

# Check snapshot
ls -la ~/.lux/snapshots/lux-snapshot-<name>/
cat ~/.lux/snapshots/lux-snapshot-<name>/manifest.json
```
