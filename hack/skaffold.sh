#!/bin/bash

set -e

# sed needs different args to -i depending on the flavor of the tool that is installed.
sedi () {
    (sed --version >/dev/null 2>&1 && sed -i "$@") || sed -i "" "$@"
}

# FEATURE_GATE_CLUSTER_SCOPED holds the value of the "ClusterScoped" feature gate.
FEATURE_GATE_CLUSTER_SCOPED=${FEATURE_GATE_CLUSTER_SCOPED:-false}
# MODE is the mode in which to run skaffold.
MODE=${MODE:-dev}
# NAMESPACE is the namespace where to deploy "nats-operator".
NAMESPACE=${NAMESPACE:-default}
# PROFILE is the skaffold profile to use.
PROFILE=${PROFILE:-local}
# TARGET is the target to run.
TARGET=${TARGET:-operator}

# Grab the absolute path to the root of the repository.
ROOT_DIR="$(git rev-parse --show-toplevel)"
# Switch directories to "ROOT_DIR".
pushd "${ROOT_DIR}" > /dev/null

# Grab the relative path to the temporary directory where to copy manifest templates to.
TMP_DIR="tmp/${TARGET}/skaffold"
# Create the temporary directory if it does not exist.
mkdir -p "${TMP_DIR}"
# Copy manifest templates to the temporary directory.
cp -r "${ROOT_DIR}/hack/${TARGET}/skaffold/"* "${TMP_DIR}/"

# Replace the "__TMP_DIR__" placeholder in "skaffold.yml".
sedi -e "s|__TMP_DIR__|${TMP_DIR}|" "${TMP_DIR}/skaffold.yml"

# Validate the value of FEATURE_GATE_CLUSTER_SCOPED and override NAMESPACE as required.
case "${FEATURE_GATE_CLUSTER_SCOPED}" in
    "1"|"true")
        # Override the value of NAMESPACE.
        NAMESPACE="nats-io"
        ;;
     "0"|"false")
        # There's nothing to do here.
        ;;
     *)
        echo "unsupported value for FEATURE_GATE_CLUSTER_SCOPED: \"${FEATURE_GATE_CLUSTER_SCOPED}\"" && exit 1
        ;;
esac

# Replace the adequate manifests based on the target.
case "${TARGET}" in
    "e2e"|"operator")
        # Replace the "__FEATURE_GATE_CLUSTER_SCOPED__" placeholder in the manifests.
        sedi -e "s|__FEATURE_GATE_CLUSTER_SCOPED__|${FEATURE_GATE_CLUSTER_SCOPED}|" "${TMP_DIR}/"*
        # Replace the "__NAMESPACE__" placeholder in the manifests
        sedi -e "s|__NAMESPACE__|${NAMESPACE}|" "${TMP_DIR}/"*
        ;;
    *)
        echo "unsupported target: \"${TARGET}\"" && exit 1
        ;;
esac

# Check whether we need to build a binary.
case "${MODE}" in
    "dev"|"run")
        if [[ "${TARGET}" == "e2e" ]]; then
            # Create the required secrets.
            "${ROOT_DIR}/hack/e2e/prepare-secrets.sh" "${NAMESPACE}"
        fi
        make -C "${ROOT_DIR}" "build.${TARGET}"
        ;;
    "delete")
        # There's nothing to do here.
        ;;
    *)
        echo "unsupported mode: \"${MODE}\"" && exit 1
esac

# Make sure the target namespace exists.
kubectl get namespace "${NAMESPACE}" > /dev/null 2>&1 || kubectl create namespace "${NAMESPACE}"

# Label the minikube node with an external ip
kubectl label nodes minikube nats.io/node-external-ip=127.0.0.1 --overwrite

# Run skaffold.
skaffold "${MODE}" -f "${TMP_DIR}/skaffold.yml" -n "${NAMESPACE}" -p "${PROFILE}"
