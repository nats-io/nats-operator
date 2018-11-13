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
$ make build
```

This will build a static binary targeting `linux-amd64`, suitable to be copied over to a container image.
Building binaries for different platforms is possible, but is out-of-scope for this document.

## Running

The build toolchain leverages on `skaffold` to build a container image of `nats-operator` and to deploy it to the Kubernetes cluster targeted by the current context.
After performing the deployment, `skaffold` will stream the logs of the `nats-operator` pod, and will keep on monitoring the `build/nats-operator` binary for changes.
When such changes occur (e.g. as a result of running `make build`), `skaffold` will re-deploy `nats-operator` to the Kubernetes cluster, and the process will repeat itself.

The exact command you must execute to run `nats-operator` depends on whether you are using a local (Minikube or Docker for Desktop) or a Google Kubernetes Engine cluster.

### Local

To run `nats-operator` against the local Kubernetes cluster targeted by the current context, you must run:

```console
$ make run PROFILE=local
```

To stop execution and cleanup the deployment, hit `Ctrl+C`.

### Google Kubernetes Engine

To run `nats-operator` against the Google Kubernetes Engine cluster targeted by the current context, you must run:

```console
$ make run PROFILE=gke
```

To stop execution and cleanup the deployment, hit `Ctrl+C`.
