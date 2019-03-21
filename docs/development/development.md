# Developing `nats-operator`

## Prerequisites

To build `nats-operator` in your local workstation, you need the following software:

* Linux or macOS
* `git`
* `make`
* Go
* [`dep`](https://github.com/golang/dep)

To run `nats-operator`, you additionally need the following software:

* [Docker](https://www.docker.com/)
* [`skaffold`](https://github.com/GoogleContainerTools/skaffold)
* [Kubernetes](https://kubernetes.io/) ([Minikube](https://github.com/kubernetes/minikube), [Docker Desktop](https://www.docker.com/products/docker-desktop) or [Google Kubernetes Engine](https://cloud.google.com/kubernetes-engine/)).

If you plan on running the end-to-end test suite, you additionally need the following software:

* [`cfssl`](https://github.com/cloudflare/cfssl)
* [`cfssljson`](https://github.com/cloudflare/cfssl)

## Cloning the repository

To start developing `nats-operator`, you first need to clone the repository into your `$GOPATH` and switch directories into the freshly cloned repo:

```console
$ git clone https://github.com/nats-io/nats-operator.git $GOPATH/src/github.com/nats-io/nats-operator
$ cd $GOPATH/src/github.com/nats-io/nats-operator
```

## Installing dependencies

To install dependencies required for building `nats-operator`, you must run:

```console
$ make dep
```

## Generating code (optional)

Whenever you add, modify or delete a field from the `NatsCluster` or `NatsServiceRole` types in  `pkg/apis/nats/v1alpha2`, you must run the code generation step after doing so:

```console
$ make gen
```

This will update the following files and directories in order to reflect the changes:

```text
pkg
├── apis
│   └── nats
│       └── v1alpha2
│           └── zz_generated.deepcopy.go (GENERATED)
└── client (GENERATED)
```

## Building

To build the `nats-operator` binary, you must run:

```console
$ make build.operator
```

This will build a static binary targeting `linux-amd64`, suitable to be copied over to a container image.
Building binaries for different platforms is possible, but is out-of-scope for this document.

## Running

The build toolchain leverages on `skaffold` to build a container image of `nats-operator` and to deploy it to the Kubernetes cluster targeted by the current context.
After performing the deployment, `skaffold` will stream the logs of the `nats-operator` pod, and will keep on monitoring the `build/nats-operator` binary for changes.
When such changes occur (e.g. as a result of running `make build`), `skaffold` will re-deploy `nats-operator` to the Kubernetes cluster, and the process will repeat itself.

The exact command you must execute to run `nats-operator` depends on whether you are using a local (Minikube or Docker for Desktop) or a Google Kubernetes Engine cluster, and on whether you want to perform a namespace-scoped or cluster-scoped deployment.

### Local

#### Namespace-scoped

To run a namespace-scoped instance of `nats-operator` against the local Kubernetes cluster targeted by the current context, you must run:

```console
$ make run NAMESPACE=<namespace> PROFILE=local
```

To stop execution and cleanup the deployment, hit `Ctrl+C`.

#### Cluster-scoped

To run a cluster-scoped instance of `nats-operator` against the local Kubernetes cluster targeted by the current context, you must run:

```console
$ make run FEATURE_GATE_CLUSTER_SCOPED=true PROFILE=local
```

To stop execution and cleanup the deployment, hit `Ctrl+C`.

**NOTE:** Cluster-scoped deployments of `nats-operator` _always_ run on the `nats-io` namespace.

### Google Kubernetes Engine

#### Namespace-scoped

To run a namespace-scoped instance of `nats-operator` against the Google Kubernetes Engine cluster targeted by the current context, you must run:

```console
$ make run NAMESPACE=<namespace> PROFILE=gke
```

To stop execution and cleanup the deployment, hit `Ctrl+C`.

#### Cluster-scoped

To run a cluster-scoped instance of `nats-operator` against the Google Kubernetes Engine cluster targeted by the current context, you must run:

```console
$ make run FEATURE_GATE_CLUSTER_SCOPED=true PROFILE=gke
```

To stop execution and cleanup the deployment, hit `Ctrl+C`.

**NOTE:** Cluster-scoped deployments of `nats-operator` _always_ run on the `nats-io` namespace.

## Testing

`nats-operator` includes an end-to-end test suite that is used to validate the implementation.
Some tests require certain advanced features to be enabled on the target Kubernetes cluster.
The test suite will do its best to perform feature detection on the target Kubernetes cluster and to skip tests that depend on these advanced features.
To launch a Minikube cluster suitable for running the full end-to-end test suite, you may run:

```console
$ minikube start \
    --extra-config=apiserver.service-account-signing-key-file=/var/lib/minikube/certs/apiserver.key \
    --extra-config=apiserver.service-account-issuer=api \
    --extra-config=apiserver.service-account-api-audiences=api \
    --kubernetes-version=v1.12.4
```

Then, to run the test suite against the resulting Minikube cluster, you may simply run:

```console
$ make e2e
```

Executing this command will do several things in background:

1. Create the TLS secrets required for testing if they don't exist already.
1. Build the `nats-operator` binary locally.
1. Build a container image containing the `nats-operator` binary and deploy it to the Kubernetes cluster.
1. Build the `nats-operator-e2e` binary (which contains the test suite) locally.
1. Build a container image containing the `nats-operator-e2e` binary and deploy it to the Kubernetes cluster.
1. Wait for the `nats-operator` and `nats-operator-e2e` pods to be running.
1. Start streaming logs from the `nats-operator-e2e` pod and wait for it to terminate.
1. Delete the `nats-operator` and `nats-operator-e2e` pods.
1. Exit with the same exit code as the main container of the `nats-operator-e2e` pod.

This allows for running the test suite from _within_ the Kubernetes cluster (hence allowing for full connectivity to the pod network) while keeping the whole process simple enough to be used in day-to-day development and CI.

### Testing in a different namespace

By default, the end-to-end test suite tests an installation of `nats-operator` in the `default` namespace.
It is however possible to test installation in a different namespace.
To run the test suite against a different namespace, you may simply run:

```console
$ make e2e NAMESPACE=<namespace>
```

`<namespace>` will be automatically created if it doesn't already exist.

### Testing the cluster-scoped mode

To perform a cluster-scoped installation of `nats-operator` and run the end-to-end test suite against it, you may run:

```console
$ make e2e FEATURE_GATE_CLUSTER_SCOPED=1
```

The required `nats-io` namespace will be automatically created if it doesn't already exist. 
