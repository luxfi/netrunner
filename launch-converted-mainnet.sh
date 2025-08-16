#!/bin/bash

# Set up paths
DATA_DIR="$HOME/.luxd-converted"
CHAIN_DATA="/home/z/work/lux/genesis/chaindata/C/db/ethdb"

echo "🚀 Launching Lux Node with Converted Mainnet Data"
echo "================================================"
echo "Database: $CHAIN_DATA"
echo "Data dir: $DATA_DIR"

# Create data directory structure
mkdir -p "$DATA_DIR/chainData/C/db"

# Create symlink to our converted database
rm -f "$DATA_DIR/chainData/C/db/ethdb"
ln -s "$CHAIN_DATA" "$DATA_DIR/chainData/C/db/ethdb"

# Launch using netrunner
./build/netrunner server \
  --network-id=96369 \
  --data-dir="$DATA_DIR" \
  --http-host=0.0.0.0 \
  --http-port=9630 \
  --staking-enabled=false \
  --network-peer-list-gossip-frequency=250ms \
  --network-max-reconnect-delay=1s \
  --public-ip=127.0.0.1 \
  --health-check-frequency=2s \
  --api-admin-enabled \
  --api-metrics-enabled \
  --index-enabled \
  --log-level=info
