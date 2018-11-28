#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Stop minikube if it is running.
minikube status | grep Running && {
    minikube stop
}

# Start minikube using the default bootstrapper (kubeadm), which already enforces RBAC by default.
sudo minikube start  --vm-driver=none --kubernetes-version=v1.10.10 --feature-gates=PodShareProcessNamespace=true
minikube update-context

# Wait once again for cluster to be ready
JSONPATH='{range .items[*]}{@.metadata.name}:{range @.status.conditions[*]}{@.type}={@.status};{end}{end}'
until kubectl get nodes -o jsonpath="$JSONPATH" 2>&1 | grep -q "Ready=True"; do sleep 1; done

# Bootstrap own user policy
kubectl create clusterrolebinding cluster-admin-binding --clusterrole=cluster-admin --serviceaccount=kube-system:default

# Confirm delete the CRD in case present right now
kubectl get crd | grep natscluster && {
    kubectl delete crd natsclusters.nats.io
}

# Deploy the manifest with RBAC enabled
kubectl apply -f ../../example/deployment-rbac.yaml

# Wait until the CRD is ready
attempts=0
until kubectl get crd natsclusters.nats.io -o yaml | grep InitialNamesAccepted; do
    if [[ attempts -eq 60 ]]; then
        echo "Gave up waiting for CRD to be ready..."
        kubectl -n nats-io logs deployment/nats-operator
        exit 1
    fi
    ((++attempts))

    echo "Waiting for CRD... ($attempts attempts)"
    sleep 1
done

# Deploy an example manifest and wait for pods to appear
kubectl -n nats-io apply -f ../../example/example-nats-cluster.yaml

# Wait until 3 pods appear
attempts=0
until kubectl -n nats-io get pods | grep -v operator | grep nats | grep Running | wc -l | grep 3; do
    if [[ attempts -eq 60 ]]; then
        echo "Gave up waiting for NatsCluster to be ready..."
        kubectl -n nats-io logs deployment/nats-operator
        kubectl -n nats-io logs -l nats_cluster=example-nats-1
        exit 1
    fi

    echo "Waiting for pods to appear ($attempts attempts)..."
    ((++attempts))
    sleep 1
done

# Show output to confirm.
kubectl -n nats-io logs -l nats_cluster=example-nats-1
