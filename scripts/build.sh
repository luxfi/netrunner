#!/bin/bash

# Run with ./scripts/build.sh <optional_build_location>

if ! [[ "$0" =~ scripts/build.sh ]]; then
  echo "must be run from repository root"
  exit 1
fi

VERSION=`cat VERSION`
COMMIT=`git rev-parse --short HEAD 2>/dev/null || echo "unknown"`
BUILD_DATE=`date -u +"%Y-%m-%dT%H:%M:%SZ"`

if [ $# -eq 0 ] ; then
    OUTPUT="bin"
else
    OUTPUT=$1
fi

# Set the CGO flags to use the portable version of BLST
#
# We use "export" here instead of just setting a bash variable because we need
# to pass this flag to all child processes spawned by the shell.
export CGO_CFLAGS="-O -D__BLST_PORTABLE__"

LDFLAGS="-X 'github.com/luxfi/netrunner/cmd.Version=$VERSION'"
LDFLAGS="$LDFLAGS -X 'github.com/luxfi/netrunner/cmd.Commit=$COMMIT'"
LDFLAGS="$LDFLAGS -X 'github.com/luxfi/netrunner/cmd.BuildDate=$BUILD_DATE'"

go build -v -ldflags="$LDFLAGS" -o $OUTPUT/netrunner
