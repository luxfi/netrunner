#!/bin/bash
set -e

LUXD=/Users/z/work/lux/node/build/luxd
NETRUNNER=/Users/z/work/lux/netrunner
GENESIS=/tmp/fixed-genesis.json
BASE=/tmp/lux-working

pkill -9 luxd 2>/dev/null || true
rm -rf $BASE && mkdir -p $BASE

for i in {1..5}; do
    mkdir -p $BASE/node$i
    cp $NETRUNNER/local/default/node$i/*.{crt,key} $BASE/node$i/
done

echo "Starting node 1..."
$LUXD --network-id=1337 --genesis-file=$GENESIS --http-port=9630 --staking-port=9631 \
  --data-dir=$BASE/node1 --staking-tls-cert-file=$BASE/node1/staking.crt \
  --staking-tls-key-file=$BASE/node1/staking.key --staking-signer-key-file=$BASE/node1/signer.key \
  --http-host=0.0.0.0 --public-ip=127.0.0.1 > $BASE/n1.log 2>&1 &

sleep 15
N1ID=$(curl -s http://localhost:9630/ext/info -d '{"jsonrpc":"2.0","id":1,"method":"info.getNodeID"}' -H 'content-type:application/json;' | jq -r '.result.nodeID')
[ -z "$N1ID" ] && echo "ERROR: No Node ID" && tail -10 $BASE/n1.log && exit 1
echo "Node 1 ID: $N1ID"

for i in 2 3 4 5; do
    P=$((9630+(i-1)*10))
    SP=$((9631+(i-1)*10))
    $LUXD --network-id=1337 --genesis-file=$GENESIS --http-port=$P --staking-port=$SP \
      --data-dir=$BASE/node$i --staking-tls-cert-file=$BASE/node$i/staking.crt \
      --staking-tls-key-file=$BASE/node$i/staking.key --staking-signer-key-file=$BASE/node$i/signer.key \
      --http-host=0.0.0.0 --public-ip=127.0.0.1 \
      --bootstrap-ips=127.0.0.1:9631 --bootstrap-ids=$N1ID > $BASE/n$i.log 2>&1 &
    sleep 2
done

echo "Waiting 50s for bootstrap..."
sleep 50

PEERS=$(curl -s http://localhost:9630/ext/info -d '{"jsonrpc":"2.0","id":1,"method":"info.peers"}' -H 'content-type:application/json;' | jq -r '.result.numPeers')
CBS=$(curl -s http://localhost:9630/ext/info -d '{"jsonrpc":"2.0","id":1,"method":"info.isBootstrapped","params":{"chain":"C"}}' -H 'content-type:application/json;' | jq -r '.result.isBootstrapped')

echo "Peers: $PEERS/4, C-Chain: $CBS"
[ "$CBS" = "true" ] && echo "✅ Network ready for transactions!"
