# Multinet Support in Netrunner

## Quick Start

The netrunner now supports running multiple networks in parallel with a shared BadgerDB for ACID cross-chain transactions.

## Building

```bash
cd /home/z/work/lux/netrunner
go build -o netrunner .
```

## Usage

### Start Multiple Networks
```bash
# Start both Lux mainnet and testnet with all subnets
netrunner multinet start

# With custom configuration
netrunner multinet start --configs networks.json

# With custom shared database path
netrunner multinet start --shared-db /path/to/shared/db
```

### Configure Networks
```bash
# Generate default configuration file
netrunner multinet configure

# This creates multinetwork-config.json with:
# - Lux Mainnet (96369)
# - Lux Testnet (96368)
# - Zoo Network (subnet on mainnet)
# - Hanzo Network (subnet on mainnet)
```

### Check Status
```bash
# Show status of all networks
netrunner multinet status
```

### Cross-Chain Transactions
```bash
# Transfer 1000 LUX from mainnet to testnet
netrunner multinet crosschain 96369 C 96368 C 1000

# Transfer between subnets (through parent network)
netrunner multinet crosschain 200200 C 36963 C 500
```

## Network Architecture

```
┌─────────────────────────────────────────┐
│           NETRUNNER MULTINET            │
│                                         │
│  ┌─────────────┐   ┌─────────────┐    │
│  │  MAINNET    │   │  TESTNET    │    │
│  │   (96369)   │   │   (96368)   │    │
│  │             │   │             │    │
│  │  P/X/C      │   │  P/X/C      │    │
│  │  Chains     │   │  Chains     │    │
│  └──────┬──────┘   └──────┬──────┘    │
│         │                  │           │
│   ┌─────▼─────┐      ┌────▼────┐      │
│   │  SUBNETS  │      │ SUBNETS │      │
│   │           │      │         │      │
│   │ • Zoo     │      │ • Zoo-T │      │
│   │ • Hanzo   │      │ • Hanzo-T│      │
│   │ • SPC     │      │ • SPC-T │      │
│   └───────────┘      └─────────┘      │
│                                         │
│  ┌─────────────────────────────────┐   │
│  │     SHARED BADGERDB (ACID)      │   │
│  └─────────────────────────────────┘   │
└─────────────────────────────────────────┘
```

## Key Features

1. **Parallel Validation**: Both mainnet and testnet validate simultaneously
2. **Shared Database**: Single BadgerDB for all networks enables ACID transactions
3. **Low Latency**: Direct memory access for cross-chain operations
4. **Subnet Support**: L1s/L2s run as subnets on their parent networks
5. **Unified CLI**: Single `netrunner` command manages everything

## API Endpoints

When running multinet, access networks at their configured ports:

- **Mainnet** (port 9630):
  - P-Chain: `http://localhost:9630/ext/P`
  - X-Chain: `http://localhost:9630/ext/X`
  - C-Chain: `http://localhost:9630/ext/bc/C/rpc`
  - Subnets: `http://localhost:9630/ext/bc/{subnet-chain-id}/rpc`

- **Testnet** (port 9620):
  - P-Chain: `http://localhost:9620/ext/P`
  - X-Chain: `http://localhost:9620/ext/X`
  - C-Chain: `http://localhost:9620/ext/bc/C/rpc`
  - Subnets: `http://localhost:9620/ext/bc/{subnet-chain-id}/rpc`

## Configuration File

Create `multinetwork-config.json`:

```json
[
  {
    "networkID": 96369,
    "name": "Lux Mainnet",
    "type": "primary",
    "httpPort": 9630,
    "stakingPort": 9631,
    "dataDir": "/tmp/multinet/mainnet",
    "validators": 5
  },
  {
    "networkID": 96368,
    "name": "Lux Testnet",
    "type": "primary",
    "httpPort": 9620,
    "stakingPort": 9621,
    "dataDir": "/tmp/multinet/testnet",
    "validators": 3
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

## Development

### Build the netrunner
```bash
cd /home/z/work/lux/netrunner
go build -o netrunner .
```

### Run tests
```bash
go test ./multinet/...
```

### Clean up
```bash
# Stop all networks
pkill -f netrunner

# Remove data
rm -rf /tmp/multinet*
```

## Architecture Benefits

1. **Single Process**: One netrunner manages both networks
2. **Shared State**: Cross-chain transactions are atomic
3. **Resource Efficient**: Shared database reduces overhead
4. **Easy Testing**: Quickly spin up multi-network environments
5. **Production Ready**: Same code runs local and production

## Notes

- Subnets (Zoo, Hanzo, SPC) are L1s/L2s that run ON the primary networks
- They are NOT separate primary networks themselves
- Access subnets through their parent network's RPC endpoint
- The shared BadgerDB enables truly atomic cross-chain operations