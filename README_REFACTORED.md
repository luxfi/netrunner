# Netrunner - Refactored Design

## Core Philosophy
Multi-network support is now a **built-in feature**, not a separate subcommand.

## Command Structure

```bash
# BEFORE (subcommand approach):
netrunner multinet start
netrunner multinet configure
netrunner multinet status
netrunner multinet crosschain

# AFTER (integrated approach):
netrunner start --networks all
netrunner config init
netrunner status
netrunner tx --from mainnet:C --to testnet:C --amount 1000
```

## Usage Examples

### 1. Starting Networks

```bash
# Single network (default behavior)
netrunner start                        # Starts mainnet by default

# Multiple networks (built-in feature)
netrunner start --networks all         # Start all networks
netrunner start --networks mainnet,testnet  # Start specific networks

# With advanced features
netrunner start --networks all --parallel --shared-db
```

### 2. Configuration

```bash
# No need for separate multinet configure
netrunner config init                  # Generate config for all networks
netrunner config show                  # Show current configuration
netrunner config set mainnet.port 9630 # Modify specific settings
```

### 3. Status Checking

```bash
# Works for single or multiple networks automatically
netrunner status                       # Shows status of all running networks
netrunner status mainnet              # Status of specific network
netrunner status --json               # JSON output for scripts
```

### 4. Cross-Chain Transactions

```bash
# Simplified syntax
netrunner tx --from mainnet:C --to testnet:C --amount 1000 --asset LUX

# Short form
netrunner tx mainnet:C testnet:C 1000
```

## Implementation

The key changes:

1. **Remove multinet subcommand** - Features integrated into main commands
2. **Smart detection** - Commands automatically detect if multiple networks are running
3. **Progressive disclosure** - Simple by default, powerful when needed
4. **Unified state** - Shared DB is automatic when multiple networks are running

## Benefits

### User Experience
- ✅ **Simpler** - No need to learn separate multinet commands
- ✅ **Intuitive** - Flags like `--networks all` are self-explanatory
- ✅ **Discoverable** - All features visible in main help
- ✅ **Consistent** - Same patterns for single or multiple networks

### Technical
- ✅ **Cleaner codebase** - Less command duplication
- ✅ **Better defaults** - Smart behavior based on context
- ✅ **Easier testing** - Unified command structure
- ✅ **Future-proof** - Easy to add more networks

## Migration Guide

For users familiar with the subcommand approach:

| Old Command | New Command |
|------------|-------------|
| `netrunner multinet start` | `netrunner start --networks all` |
| `netrunner multinet configure` | `netrunner config init` |
| `netrunner multinet status` | `netrunner status` |
| `netrunner multinet crosschain 96369 C 96368 C 1000` | `netrunner tx mainnet:C testnet:C 1000` |

## Configuration File

The configuration remains the same but is now in `netrunner.yaml`:

```yaml
networks:
  mainnet:
    id: 96369
    http_port: 9630
    staking_port: 9631
    validators: 5

  testnet:
    id: 96368
    http_port: 9620
    staking_port: 9621
    validators: 3

  zoo:
    id: 200200
    type: subnet
    parent: mainnet
    validators: 5

features:
  shared_db: true        # Enable shared BadgerDB
  parallel: true         # Parallel validation
  cross_chain_tx: true   # Cross-chain transactions
```

## Summary

By making multi-network support a core feature rather than a subcommand:
- The CLI becomes more intuitive
- Commands are shorter and easier to remember
- The same commands work regardless of how many networks are running
- Advanced features are available through flags, not separate command trees

This follows the principle of **"Make simple things simple, and complex things possible"**.