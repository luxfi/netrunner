#!/usr/bin/env bash
set -e

export RUN_E2E="true"
# e.g.,
# ./scripts/tests.e2e.sh $DEFAULT_VERSION1 $DEFAULT_VERSION2 $DEFAULT_SUBNET_EVM_VERSION
if ! [[ "$0" =~ scripts/tests.e2e.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

DEFAULT_VERSION_1=1.22.80
DEFAULT_VERSION_2=1.22.79
DEFAULT_SUBNET_EVM_VERSION=0.8.35

if [ $# == 0 ]; then
    VERSION_1=$DEFAULT_VERSION_1
    VERSION_2=$DEFAULT_VERSION_2
    SUBNET_EVM_VERSION=$DEFAULT_SUBNET_EVM_VERSION
else
    VERSION_1=$1
    if [[ -z "${VERSION_1}" ]]; then
      echo "Missing version argument!"
      echo "Usage: ${0} [VERSION_1] [VERSION_2] [SUBNET_EVM_VERSION]" >> /dev/stderr
      exit 255
    fi
    VERSION_2=$2
    if [[ -z "${VERSION_2}" ]]; then
      echo "Missing version argument!"
      echo "Usage: ${0} [VERSION_1] [VERSION_2] [SUBNET_EVM_VERSION]" >> /dev/stderr
      exit 255
    fi
    SUBNET_EVM_VERSION=$3
    if [[ -z "${SUBNET_EVM_VERSION}" ]]; then
      echo "Missing version argument!"
      echo "Usage: ${0} [VERSION_1] [VERSION_2] [SUBNET_EVM_VERSION]" >> /dev/stderr
      exit 255
    fi
fi

echo "Running e2e tests with:"
echo VERSION_1: ${VERSION_1}
echo VERSION_2: ${VERSION_2}
echo SUBNET_EVM_VERSION: ${SUBNET_EVM_VERSION}

if [ ! -f /tmp/node-v${VERSION_1}/node ]
then
    ############################
    # download node
    # https://github.com/luxfi/node/releases
    GOARCH=$(go env GOARCH)
    GOOS=$(go env GOOS)
    DOWNLOAD_URL=https://github.com/luxfi/node/releases/download/v${VERSION_1}/node-linux-${GOARCH}-v${VERSION_1}.tar.gz
    DOWNLOAD_PATH=/tmp/node.tar.gz
    if [[ ${GOOS} == "darwin" ]]; then
      DOWNLOAD_URL=https://github.com/luxfi/node/releases/download/v${VERSION_1}/node-macos-v${VERSION_1}.zip
      DOWNLOAD_PATH=/tmp/node.zip
    fi

    rm -rf /tmp/node-v${VERSION_1}
    rm -rf /tmp/node-build
    rm -f ${DOWNLOAD_PATH}

    echo "downloading node ${VERSION_1} at ${DOWNLOAD_URL}"
    curl -L ${DOWNLOAD_URL} -o ${DOWNLOAD_PATH}

    echo "extracting downloaded node"
    if [[ ${GOOS} == "linux" ]]; then
      tar xzvf ${DOWNLOAD_PATH} -C /tmp
      # tarball extracts to build/luxd, move to expected location
      mv /tmp/build /tmp/node-v${VERSION_1}
      # create symlink from node -> luxd for compatibility
      ln -sf luxd /tmp/node-v${VERSION_1}/node
    elif [[ ${GOOS} == "darwin" ]]; then
      unzip ${DOWNLOAD_PATH} -d /tmp/node-build
      mv /tmp/node-build/build /tmp/node-v${VERSION_1}
      ln -sf luxd /tmp/node-v${VERSION_1}/node
    fi
    find /tmp/node-v${VERSION_1}
fi

if [ ! -f /tmp/node-v${VERSION_2}/node ]
then
    ############################
    # download node
    # https://github.com/luxfi/node/releases
    GOARCH=$(go env GOARCH)
    GOOS=$(go env GOOS)
    DOWNLOAD_URL=https://github.com/luxfi/node/releases/download/v${VERSION_2}/node-linux-${GOARCH}-v${VERSION_2}.tar.gz
    DOWNLOAD_PATH=/tmp/node.tar.gz
    if [[ ${GOOS} == "darwin" ]]; then
      DOWNLOAD_URL=https://github.com/luxfi/node/releases/download/v${VERSION_2}/node-macos-v${VERSION_2}.zip
      DOWNLOAD_PATH=/tmp/node.zip
    fi

    rm -rf /tmp/node-v${VERSION_2}
    rm -rf /tmp/node-build
    rm -f ${DOWNLOAD_PATH}

    echo "downloading node ${VERSION_2} at ${DOWNLOAD_URL}"
    curl -L ${DOWNLOAD_URL} -o ${DOWNLOAD_PATH}

    echo "extracting downloaded node"
    if [[ ${GOOS} == "linux" ]]; then
      tar xzvf ${DOWNLOAD_PATH} -C /tmp
      # tarball extracts to build/luxd, move to expected location
      mv /tmp/build /tmp/node-v${VERSION_2}
      # create symlink from node -> luxd for compatibility
      ln -sf luxd /tmp/node-v${VERSION_2}/node
    elif [[ ${GOOS} == "darwin" ]]; then
      unzip ${DOWNLOAD_PATH} -d /tmp/node-build
      mv /tmp/node-build/build /tmp/node-v${VERSION_2}
      ln -sf luxd /tmp/node-v${VERSION_2}/node
    fi
    find /tmp/node-v${VERSION_2}
fi

if [ ! -f /tmp/evm-v${SUBNET_EVM_VERSION}/evm ]
then
    ############################
    # download evm plugin
    # https://github.com/luxfi/evm/releases
    GOARCH=$(go env GOARCH)
    GOOS=$(go env GOOS)
    # evm releases are raw binaries named evm-plugin-{os}-{arch}
    DOWNLOAD_URL=https://github.com/luxfi/evm/releases/download/v${SUBNET_EVM_VERSION}/evm-plugin-linux-${GOARCH}
    if [[ ${GOOS} == "darwin" ]]; then
      DOWNLOAD_URL=https://github.com/luxfi/evm/releases/download/v${SUBNET_EVM_VERSION}/evm-plugin-darwin-${GOARCH}
    fi

    rm -rf /tmp/evm-v${SUBNET_EVM_VERSION}
    mkdir -p /tmp/evm-v${SUBNET_EVM_VERSION}

    echo "downloading evm ${SUBNET_EVM_VERSION} at ${DOWNLOAD_URL}"
    curl -L ${DOWNLOAD_URL} -o /tmp/evm-v${SUBNET_EVM_VERSION}/evm
    chmod +x /tmp/evm-v${SUBNET_EVM_VERSION}/evm

    # NOTE: We are copying the evm binary here to a plugin hardcoded as srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy which corresponds to the VM name `subnetevm` used as such in the test
    mkdir -p /tmp/node-v${VERSION_1}/plugins/
    cp /tmp/evm-v${SUBNET_EVM_VERSION}/evm /tmp/node-v${VERSION_1}/plugins/srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy
    find /tmp/evm-v${SUBNET_EVM_VERSION}/evm
fi
############################
echo "building runner"
./scripts/build.sh

# Set the CGO flags to use the portable version of BLST
#
# We use "export" here instead of just setting a bash variable because we need
# to pass this flag to all child processes spawned by the shell.
export CGO_CFLAGS="-O -D__BLST_PORTABLE__"

echo "building e2e.test"
# to install the ginkgo binary (required for test build and run)
go install -v github.com/onsi/ginkgo/v2/ginkgo@v2.1.3
ACK_GINKGO_RC=true ginkgo build --tags e2e ./tests/e2e
./tests/e2e/e2e.test --help

snapshots_dir=/tmp/network-runner-root-data/snapshots-e2e/
rm -rf $snapshots_dir

killall netrunner || true

echo "launch local test cluster in the background"
bin/netrunner \
server \
--log-level debug \
--port=":8080" \
--snapshots-dir=$snapshots_dir \
--grpc-gateway-port=":8081" &
#--disable-nodes-output \
PID=${!}

function cleanup()
{
  echo "shutting down network runner"
  kill ${PID}
}
trap cleanup EXIT

echo "running e2e tests"
./tests/e2e/e2e.test \
--ginkgo.v \
--ginkgo.fail-fast \
--log-level debug \
--grpc-endpoint="0.0.0.0:8080" \
--grpc-gateway-endpoint="0.0.0.0:8081" \
--node-path-1=/tmp/node-v${VERSION_1}/node \
--node-path-2=/tmp/node-v${VERSION_2}/node \
--subnet-evm-path=/tmp/evm-v${SUBNET_EVM_VERSION}/evm
