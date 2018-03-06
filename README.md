# NATS Operator

<<<<<<< HEAD
[![Build Status](https://travis-ci.org/nats-io/nats-operator.svg?branch=master)](https://travis-ci.org/nats-io/nats-operator)
=======
[![Build Status](https://travis-ci.org/wallyqs/nats-operator-dev.svg?branch=master)](https://travis-ci.org/wallyqs/nats-operator-dev)
>>>>>>> README updates

NATS Operator manages NATS clusters atop [Kubernetes][k8s-home], automating their creation and administration.

[k8s-home]: http://kubernetes.io

## Requirements

- Kubernetes v1.7+
- NATS Server v1.0.4

## Getting Started

The current version of the operator creates a CustomResourceDefinition
under the `nats.io` API group, to which you can make requests to
create NATS clusters.

To install:

```
kubectl apply -f https://raw.githubusercontent.com/nats-io/nats-operator/master/example/deployment.yaml
```

You will then be able to confirm that there is a new CRD registered
in the cluster:

```
kubectl get crd

NAME                                    AGE
natsclusters.nats.io                    1s
```

Example of creating a 3 node cluster:

```
echo '
apiVersion: "nats.io/v1beta1"
kind: "NatsCluster"
metadata:
  name: "example-nats-cluster"
spec:
  size: 3
  version: "1.0.4"
' | kubectl apply -f -
```

In order to list all the current NatsClusters

```sh
kubectl get natsclusters.nats.io

NAME                   AGE
example-nats-cluster   1s
```

## Development

### Building the Docker Image

To build the `nats-operator` Docker image:

```
$ docker build -t <image>:<tag> .
```

You'll need Docker `17.06.0-ce` or higher.

### Updating the NatsCluster type

If you are adding a new field to the `NatsCluster`, then you have to
get the `deepcopy-gen` tools first.

```
go get -u github.com/kubernetes/gengo/examples/deepcopy-gen
```

Then run the code generation script in order to recreate
`pkg/spec/zz_generated.deepcopy.go` with the required methods to
support that field filled in:

```sh
./hack/codegen.sh
```
