#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

echo "--- Updating NatsCluster CRD Deepcopy..."
deepcopy-gen --logtostderr -v=1 \
             --input-dirs="github.com/nats-io/nats-operator/pkg/spec" \
	     --output-file-base zz_generated.deepcopy  \
	     --bounding-dirs "github.com/nats-io/nats-operator/pkg/spec" \
             --go-header-file hack/boilerplate.txt
