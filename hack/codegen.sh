#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(realpath $(dirname ${BASH_SOURCE})/..)

# Make sure all the build tool dependencies exist in vendor
if [ ! -d "${SCRIPT_ROOT}/vendor" ]; then
  go mod vendor
fi

# Create a FAKE_GOPATH and FAKE_REPOPATH, and symbolic link to real repo path
FAKE_GOPATH="$(mktemp -d)"
trap 'rm -rf ${FAKE_GOPATH}' EXIT
FAKE_REPOPATH="${FAKE_GOPATH}/src/github.com/nats-io/nats-operator"
mkdir -p "$(dirname "${FAKE_REPOPATH}")" && ln -s "${SCRIPT_ROOT}" "${FAKE_REPOPATH}"

# Switch to GOPATH mode, it makes codegen faster
export GOPATH="${FAKE_GOPATH}"
export GO111MODULE="off"

cd "${FAKE_REPOPATH}"
CODEGEN_PKG=${CODEGEN_PKG:-$(cd "${FAKE_REPOPATH}"; ls -d -1 ./vendor/k8s.io/code-generator 2>/dev/null || echo ../code-generator)}

bash -x ${CODEGEN_PKG}/generate-groups.sh all \
    github.com/nats-io/nats-operator/pkg/client github.com/nats-io/nats-operator/pkg/apis \
    "nats:v1alpha2" \
    --go-header-file ${SCRIPT_ROOT}/hack/boilerplate.txt

rm -rf vendor

