#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

cd $GOPATH/src/github.com/nats-io/nats-operator/test/certs

# Create the secret containing the root CA certificate.
# Do not fail if a secret with the same name already exists.
kubectl create secret generic nats-ca --from-file=ca.pem 2>/dev/null || true
# Create the secret containing the client certificate and private key.
# Do not fail if a secret with the same name already exists.
kubectl create secret generic nats-certs --from-file=ca.pem --from-file=server-key.pem --from-file=server.pem 2>/dev/null  || true
# Create the secret containing the certificate and private key used for route connections.
# Do not fail if a secret with the same name already exists.
kubectl create secret generic nats-routes-tls --from-file=ca.pem --from-file=route-key.pem --from-file=route.pem 2>/dev/null  || true
