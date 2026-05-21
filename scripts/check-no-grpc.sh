#!/usr/bin/env bash
# Enforces the project rule: default-tag builds of netrunner have ZERO
# google.golang.org/grpc and grpc-gateway transitive deps.
#
# Run from netrunner repo root:  bash scripts/check-no-grpc.sh
#
# CI gate. Fails non-zero with a tagged list of offenders.

set -euo pipefail

cd "$(dirname "$0")/.."

bad=$(go list -deps -f '{{.ImportPath}}' ./... 2>/dev/null \
        | grep -E '^google\.golang\.org/grpc(/|$)|grpc-ecosystem/grpc-gateway' \
        || true)

if [[ -n "$bad" ]]; then
  echo "ERROR: gRPC transitive deps leaked into default-tag build:" >&2
  echo "$bad" | sed 's/^/  - /' >&2
  echo "" >&2
  echo "ZAP is the only wire protocol on default builds. gate via -tags grpc only." >&2
  exit 1
fi

echo "ok: no grpc/grpc-gateway in default-tag dep graph"
