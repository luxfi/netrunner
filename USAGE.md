# Netrunner Usage Guide

## Simple, Intuitive Commands

Netrunner now has multi-network support built-in as a core feature, not a subcommand.

## Starting Networks

### Single Network (Default)
```bash
# Start mainnet (default)
netrunner start

# Start specific network
netrunner start --networks testnet

# Start with custom configuration
netrunner start --config config.json
```

### Multiple Networks (Built-in Feature)
```bash
# Start both mainnet and testnet
netrunner start --networks mainnet,testnet

# Start all networks
netrunner start --networks all

# Enable parallel validation with shared DB
netrunner start --networks all --parallel --shared-db
```

## Network Status
```bash
# Check status of running networks
netrunner status

# Detailed status with validators
netrunner status --verbose
```

## Cross-Chain Transactions
```bash
# Submit cross-chain transaction (when running multiple networks)
netrunner tx --from mainnet:C --to testnet:C --amount 1000

# Between subnets
netrunner tx --from zoo --to hanzo --amount 500
```

## Configuration
```bash
# Generate default config
netrunner config init

# Show current config
netrunner config show

# Edit config
netrunner config set mainnet.validators 10
```

## Examples

### Development Setup (Single Network)
```bash
# Quick start for development
netrunner start
```

### Testing Setup (Multiple Networks)
```bash
# Start both networks for testing cross-chain features
netrunner start --networks all --shared-db
```

### Production Setup
```bash
# Start with custom config and monitoring
netrunner start --config production.json --metrics --log-level info
```

## Key Design Principles

1. **No unnecessary subcommands** - Multi-network is a feature, not a separate mode
2. **Smart defaults** - Single network by default, multi-network when needed
3. **Progressive enhancement** - Simple commands for simple tasks, flags for advanced features
4. **Intuitive flags** - `--networks all` instead of `multinet start`

## Comparison

### Old (subcommand approach):
```bash
netrunner multinet start
netrunner multinet configure
netrunner multinet status
```

### New (integrated approach):
```bash
netrunner start --networks all
netrunner config init
netrunner status
```

## Benefits

- **Simpler**: No need to remember separate multinet subcommand
- **Consistent**: Same commands work for single or multiple networks
- **Discoverable**: `netrunner --help` shows all features
- **Flexible**: Flags enable advanced features when needed
- **Backward Compatible**: Existing single-network commands still work