#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

cd $GOPATH/src/github.com/nats-io/nats-operator/test/certs

# Root CA
kubectl create secret generic nats-ca --from-file=ca.pem

# Clients
kubectl create secret generic nats-certs --from-file=ca.pem --from-file=server-key.pem --from-file=server.pem

# Routes
kubectl create secret generic nats-routes-tls --from-file=ca.pem --from-file=route-key.pem --from-file=route.pem
