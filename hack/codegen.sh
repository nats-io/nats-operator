#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# CODEGEN_PKG is the name of the package containing the "generate-groups.sh" script.
CODEGEN_PKG="${CODEGEN_PKG:-${GOPATH}/src/k8s.io/code-generator}"
# HACK_DIR is the path to the current directory.
HACK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# ROOT_PKG is the name of the root package.
ROOT_PKG="github.com/nats-io/nats-operator"

# Generate the required code (deepcopy implementations, typed clients, listers and informers) using "generate-groups.sh".
"${CODEGEN_PKG}/generate-groups.sh" \
  "deepcopy,client,informer,lister" \
  ${ROOT_PKG}/pkg/client \
  ${ROOT_PKG}/pkg/apis \
  nats:v1alpha2 \
  --go-header-file "${HACK_DIR}/boilerplate.txt"
