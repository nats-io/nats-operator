#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# NAMESPACE contains the name of the namespace where the secrets must be created.
NAMESPACE="${1:-default}"

# CURRENT_DIR contains the absolute path to the directory where the current script is located.
CURRENT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null && pwd )"

# Swiwtch to "./certs" so we can create the certificates.
pushd "${CURRENT_DIR}/certs" > /dev/null

# Create the root CA.
cfssl gencert \
  -initca ca-csr.json | cfssljson -bare ca

# Create the client certificate.
cfssl gencert \
  -ca=ca.pem \
  -ca-key=ca-key.pem \
  -config=ca-config.json \
  -profile=nats \
  client-csr.json | cfssljson -bare client

# Create the server certificate.
cfssl gencert \
  -ca=ca.pem \
  -ca-key=ca-key.pem \
  -config=ca-config.json \
  -hostname="nats,*.nats,*.nats.${NAMESPACE},*.nats.${NAMESPACE}.svc,*.nats.${NAMESPACE}.svc.cluster.local" \
  -profile=nats \
  server-csr.json | cfssljson -bare server

# Create the route certificate.
cfssl gencert \
  -ca=ca.pem \
  -ca-key=ca-key.pem \
  -config=ca-config.json \
  -hostname="nats-mgmt,*.nats-mgmt,*.nats-mgmt.${NAMESPACE},*.nats-mgmt.${NAMESPACE}.svc,*.nats-mgmt.${NAMESPACE}.svc.cluster.local" \
  -profile=nats \
  route-csr.json | cfssljson -bare route

# Create the secret containing the root CA certificate.
# Do not fail if a secret with the same name already exists.
kubectl --namespace "${NAMESPACE}" create secret generic nats-ca --from-file=ca.pem 2>/dev/null || true
# Create the secret containing the client certificate and private key.
# Do not fail if a secret with the same name already exists.
kubectl --namespace "${NAMESPACE}" create secret generic nats-certs --from-file=ca.pem --from-file=server-key.pem --from-file=server.pem 2>/dev/null  || true
# Create the secret containing the certificate and private key used for route connections.
# Do not fail if a secret with the same name already exists.
kubectl --namespace "${NAMESPACE}" create secret generic nats-routes-tls --from-file=ca.pem --from-file=route-key.pem --from-file=route.pem 2>/dev/null  || true

# Switch back to the original directory.
popd > /dev/null
