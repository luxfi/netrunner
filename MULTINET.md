# Multi-Network Integration for Netrunner

## Overview

This integration enables the Lux Netrunner to orchestrate multiple heterogeneous networks running in parallel with shared BadgerDB for fully ACID cross-chain transactions with low latency.

## Architecture

### Core Components

1. **MultiNetworkManager** (`multinetwork/manager.go`)
   - Manages multiple network instances
   - Coordinates parallel consensus validation
   - Handles cross-chain transaction orchestration
   - Provides shared BadgerDB for ACID guarantees

2. **Network Types**
   - **Primary Networks**: Mainnet (96369), Testnet (96368)
   - **Subnets (L1s/L2s)**: Zoo, Hanzo, SPC running on primary networks

3. **Shared Database**
   - Single BadgerDB instance shared across all networks
   - ACID transactions for cross-chain operations
   - Low-latency atomic operations

4. **Parallel Consensus**
   - Each network runs its own consensus engine
   - Snowman consensus for linear chains
   - Avalanche consensus for DAG chains
   - Configurable parameters per network

## Features

### 1. Multi-Network Orchestration
```bash
# Start all networks in parallel
netrunner multinet start

# Configure networks
netrunner multinet configure

# Check status
netrunner multinet status
```

### 2. Cross-Chain ACID Transactions
```go
// Atomic cross-chain transfer
tx := &CrossChainTx{
    SourceNet:   96369,  // Mainnet
    SourceChain: "C",    // C-Chain
    DestNet:     96368,  // Testnet
    DestChain:   "C",    // C-Chain
    Amount:      1000,
    Asset:       "LUX",
}
manager.SubmitCrossChainTx(tx)
```

### 3. Shared Database Access
All networks share a single BadgerDB instance, enabling:
- Atomic multi-network operations
- Cross-chain state consistency
- Low-latency inter-network communication
- ACID guarantees for complex transactions

### 4. Subnet Management
Subnets are managed as children of primary networks:
```go
zooConfig := NetworkConfig{
    NetworkID:   200200,
    Name:        "Zoo Network",
    Type:        NetworkTypeSubnet,
    ParentID:    96369,  // Runs on Mainnet
    Validators:  5,
}
```

## Implementation Details

### Database Structure
```
/shared-db/
├── balances/
│   ├── 96369/C/LUX      # Mainnet C-Chain LUX balances
│   ├── 96368/C/LUX      # Testnet C-Chain LUX balances
│   ├── 200200/C/ZOO     # Zoo subnet balances
│   └── 36963/C/AI       # Hanzo subnet balances
├── transactions/
│   ├── cross-chain/     # Cross-chain transaction records
│   └── pending/         # Pending transactions
└── consensus/
    ├── 96369/           # Mainnet consensus state
    └── 96368/           # Testnet consensus state
```

### Consensus Configuration
```go
type ConsensusParams struct {
    K:                1,  // For single validator testing
    Alpha:            1,
    BetaVirtuous:     1,
    BetaRogue:        2,
    ConcurrentPolls:  4,
    OptimalProcessing: 10,
    MaxProcessing:     1000,
    MaxTimeProcessing: 120s,
}
```

### Network Endpoints
- **Mainnet**: `http://localhost:9630`
  - P-Chain: `/ext/P`
  - X-Chain: `/ext/X`
  - C-Chain: `/ext/bc/C/rpc`
  - Zoo Subnet: `/ext/bc/[zoo-chain-id]/rpc`
  - Hanzo Subnet: `/ext/bc/[hanzo-chain-id]/rpc`

- **Testnet**: `http://localhost:9620`
  - P-Chain: `/ext/P`
  - X-Chain: `/ext/X`
  - C-Chain: `/ext/bc/C/rpc`
  - Test Subnets: `/ext/bc/[subnet-chain-id]/rpc`

## Usage Examples

### 1. Start Multiple Networks
```bash
# Using default configuration
netrunner multinetwork start

# Using custom configuration
netrunner multinetwork start --configs networks.json --shared-db /path/to/db
```

### 2. Submit Cross-Chain Transaction
```bash
# Transfer from Mainnet C-Chain to Testnet C-Chain
netrunner multinetwork crosschain 96369 C 96368 C 1000

# Transfer from Zoo subnet to Hanzo subnet
netrunner multinetwork crosschain 200200 C 36963 C 500
```

### 3. Monitor Status
```bash
# Check all networks
netrunner multinetwork status

# Watch logs
tail -f /tmp/multinetwork/*/logs/*.log
```

## Benefits

1. **Unified Management**: Single process manages all networks
2. **Resource Efficiency**: Shared database reduces storage overhead
3. **Low Latency**: Direct memory access for cross-chain operations
4. **ACID Guarantees**: Atomic cross-chain transactions
5. **Parallel Validation**: All networks validate simultaneously
6. **Simplified Testing**: Easy multi-network test environments

## Testing

### Unit Tests
```bash
cd netrunner/multinetwork
go test -v ./...
```

### Integration Tests
```bash
# Start test networks
netrunner multinetwork start --test-mode

# Run cross-chain tests
go test -v ./integration/...
```

### Performance Benchmarks
```bash
# Benchmark cross-chain transaction throughput
go test -bench=BenchmarkCrossChainTx ./multinetwork/...

# Benchmark parallel consensus
go test -bench=BenchmarkParallelConsensus ./multinetwork/...
```

## Configuration File Format

```json
[
  {
    "networkID": 96369,
    "name": "Lux Mainnet",
    "type": "primary",
    "httpPort": 9630,
    "stakingPort": 9631,
    "dataDir": "/data/mainnet",
    "validators": 5,
    "chains": [
      {
        "chainID": "2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM",
        "vmID": "rWhpuQPF1kb72esV2momhMuTYGkEb1oL29pt2EBXWmSy4kxnT",
        "isEVM": false
      }
    ]
  },
  {
    "networkID": 200200,
    "name": "Zoo Network",
    "type": "subnet",
    "parentID": 96369,
    "validators": 5
  }
]
```

## Future Enhancements

1. **Dynamic Network Addition**: Add/remove networks at runtime
2. **Cross-Subnet Bridges**: Direct subnet-to-subnet transfers
3. **State Channels**: Off-chain scaling for high-frequency transactions
4. **Zero-Knowledge Proofs**: Privacy-preserving cross-chain transfers
5. **Rollup Integration**: L2 rollups for scalability
6. **MEV Protection**: Flashbots-style private mempools

## Conclusion

This integration transforms the Lux Netrunner into a powerful multi-network orchestrator capable of:
- Running multiple consensus algorithms in parallel
- Providing ACID guarantees for cross-chain operations
- Managing complex network topologies with subnets
- Enabling low-latency inter-network communication

The shared BadgerDB approach ensures data consistency while parallel consensus validation maintains network independence and security.