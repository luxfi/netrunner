#!/usr/bin/env bash
# Enforces the project rule "ZAP internal, ZIP edge" — default-tag builds
# of netrunner must have ZERO of:
#
#   * google.golang.org/grpc            (legacy RPC transport)
#   * grpc-ecosystem/grpc-gateway       (HTTP→gRPC bridge)
#   * google.golang.org/protobuf        (proto-on-the-wire substrate)
#   * go.opentelemetry.io/proto/otlp/*  (OTLP=protobuf — gate behind -tags otlp)
#   * k8s.io/client-go                  (gnostic-models pulls protobuf — gate behind -tags k8s)
#
# Each forbidden dep has a -tags escape hatch for legitimate opt-in use.
# Default builds must never drag any of these in.
#
# Run from netrunner repo root:  bash scripts/check-no-grpc.sh
# CI gate. Fails non-zero with a tagged list of offenders.

set -euo pipefail

cd "$(dirname "$0")/.."

bad=$(go list -deps -f '{{.ImportPath}}' ./... 2>/dev/null \
        | grep -E '^google\.golang\.org/grpc(/|$)|grpc-ecosystem/grpc-gateway|^google\.golang\.org/protobuf(/|$)|^go\.opentelemetry\.io/proto/otlp|^k8s\.io/client-go(/|$)' \
        || true)

if [[ -n "$bad" ]]; then
  echo "ERROR: forbidden transitive deps leaked into default-tag build:" >&2
  echo "$bad" | sed 's/^/  - /' >&2
  echo "" >&2
  echo "ZAP is the only wire protocol on default builds." >&2
  echo "Gate behind -tags grpc (legacy gRPC), -tags otlp (OTLP/protobuf trace export)," >&2
  echo "or -tags k8s (k8s.io/client-go deploy engine) for opt-in use only." >&2
  exit 1
fi

echo "ok: no forbidden deps in default-tag dep graph (grpc/grpc-gateway/protobuf/otlp/k8s clean)"
