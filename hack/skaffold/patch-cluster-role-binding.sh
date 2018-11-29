#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

# NAMESPACE contains the name of the namespace where the SERVICE_ACCOUNT service account has been created.
NAMESPACE="${1:-default}"
# SERVICE_ACOUNT contains the name of the service account that will be used by the "nats-operator" and "nats-operator-e2e" pods.
SERVICE_ACCOUNT="${2:-nats-operator}"
# CLUSTER_ROLE_BINDING contains the name of the cluster role binding object to patch.
CLUSTER_ROLE_BINDING="${3:-nats-operator-binding}"

# Patch CLUSTER_ROLE_BINDING in order to give permissions to the right service account (i.e. the service account in the right namespace).
kubectl patch clusterrolebinding "${CLUSTER_ROLE_BINDING}" \
    --patch "{\"subjects\":[{\"kind\":\"ServiceAccount\",\"name\":\"${SERVICE_ACCOUNT}\",\"namespace\":\"${NAMESPACE}\"}]}"
