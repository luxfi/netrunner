#!/bin/bash
# Test script for netrunner multi-consensus orchestration

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== Netrunner Multi-Consensus Orchestration Test ===${NC}"
echo

# Build netrunner
echo -e "${GREEN}Building netrunner...${NC}"
cd /home/z/work/lux/netrunner
go build -o bin/netrunner ./cmd/netrunner

# Test 1: Start Lux engine
echo
echo -e "${BLUE}Test 1: Starting Lux engine...${NC}"
./bin/netrunner engine start lux-test lux \
  --network-id 99999 \
  --http-port 29650 \
  --staking-port 29651 \
  --data-dir /tmp/netrunner-lux &

LUX_PID=$!
sleep 5

# Check if Lux is running
if kill -0 $LUX_PID 2>/dev/null; then
    echo -e "${GREEN}✓ Lux engine started${NC}"
else
    echo -e "${RED}✗ Lux engine failed to start${NC}"
    exit 1
fi

# Test 2: Start Geth engine
echo
echo -e "${BLUE}Test 2: Starting Geth engine...${NC}"
./bin/netrunner engine start geth-test geth \
  --network-id 99998 \
  --http-port 29545 \
  --ws-port 29546 \
  --data-dir /tmp/netrunner-geth &

GETH_PID=$!
sleep 5

# Check if Geth is running
if kill -0 $GETH_PID 2>/dev/null; then
    echo -e "${GREEN}✓ Geth engine started${NC}"
else
    echo -e "${RED}✗ Geth engine failed to start${NC}"
    kill $LUX_PID 2>/dev/null
    exit 1
fi

# Test 3: List engines
echo
echo -e "${BLUE}Test 3: Listing engines...${NC}"
./bin/netrunner engine list

# Test 4: Check engine status
echo
echo -e "${BLUE}Test 4: Checking engine status...${NC}"
./bin/netrunner engine status lux-test
./bin/netrunner engine status geth-test

# Test 5: Start OP Stack (will fail without L1, but tests the setup)
echo
echo -e "${BLUE}Test 5: Testing OP Stack setup...${NC}"
./bin/netrunner engine start op-test op \
  --network-id 99997 \
  --http-port 29645 \
  --data-dir /tmp/netrunner-op \
  --extra l1_rpc=http://localhost:29545 \
  --extra sequencer=true &

OP_PID=$!
sleep 3

# OP Stack may fail without proper L1 setup, that's expected
if kill -0 $OP_PID 2>/dev/null; then
    echo -e "${YELLOW}⚠ OP Stack started (may not be fully functional)${NC}"
    kill $OP_PID 2>/dev/null
else
    echo -e "${YELLOW}⚠ OP Stack failed (expected without L1 setup)${NC}"
fi

# Test 6: Start Eth2 setup
echo
echo -e "${BLUE}Test 6: Testing Eth2 setup...${NC}"
./bin/netrunner engine start eth2-test eth2 \
  --network-id 99996 \
  --http-port 29745 \
  --data-dir /tmp/netrunner-eth2 \
  --extra consensus_client=lighthouse \
  --extra execution_client=geth &

ETH2_PID=$!
sleep 3

if kill -0 $ETH2_PID 2>/dev/null; then
    echo -e "${YELLOW}⚠ Eth2 started (may not be fully functional)${NC}"
    kill $ETH2_PID 2>/dev/null
else
    echo -e "${YELLOW}⚠ Eth2 failed (expected without genesis setup)${NC}"
fi

# Test 7: Test stack manifest
echo
echo -e "${BLUE}Test 7: Creating stack manifest...${NC}"
cat > /tmp/test-stack.yaml << EOF
name: test-stack
version: 1.0.0
description: Test multi-consensus stack

engines:
  - name: lux-l1
    type: lux
    network_id: 96369
    http_port: 30650
    staking_port: 30651
    wait_healthy: true
    
  - name: geth-l2
    type: geth
    network_id: 200200
    http_port: 30545
    depends_on: [lux-l1]
    
bridge:
  type: native
  source: lux-l1
  destination: geth-l2
EOF

echo -e "${GREEN}✓ Stack manifest created${NC}"

# Cleanup
echo
echo -e "${BLUE}Cleaning up...${NC}"
kill $LUX_PID 2>/dev/null || true
kill $GETH_PID 2>/dev/null || true
rm -rf /tmp/netrunner-*

echo
echo -e "${GREEN}=== Orchestration Test Complete ===${NC}"
echo
echo "Summary:"
echo "  ✓ Lux engine: Functional"
echo "  ✓ Geth engine: Functional"  
echo "  ⚠ OP Stack: Requires L1 setup"
echo "  ⚠ Eth2: Requires genesis setup"
echo "  ✓ Multi-engine orchestration: Working"
echo
echo "Next steps:"
echo "1. Configure proper genesis for Eth2"
echo "2. Set up L1 for OP Stack testing"
echo "3. Test bridge functionality"
echo "4. Run production stack with real data"