# NATS Operator

[![License Apache 2.0](https://img.shields.io/badge/License-Apache2-blue.svg)](https://www.apache.org/licenses/LICENSE-2.0)
[![Build Status](https://travis-ci.org/nats-io/nats-operator.svg?branch=master)](https://travis-ci.org/nats-io/nats-operator)
[![Version](https://d25lcipzij17d.cloudfront.net/badge.svg?id=go&type=5&v=0.2.0)](https://github.com/nats-io/nats-operator/releases/tag/v0.2.0)

NATS Operator manages NATS clusters atop [Kubernetes][k8s-home], automating their creation and administration.

[k8s-home]: http://kubernetes.io

## Requirements

- Kubernetes v1.8+

## Getting Started

The current version of the operator creates a `NatsCluster` [Custom Resources Definition](https://kubernetes.io/docs/tasks/access-kubernetes-api/extend-api-custom-resource-definitions/) (CRD) under the `nats.io` API group, to which you can make requests to create NATS clusters.

To add the `NatsCluster` and NATS Operator to your cluster you can run:

```
$ kubectl apply -f https://raw.githubusercontent.com/nats-io/nats-operator/master/example/deployment.yaml
```

You will then be able to confirm that there is a new CRD registered
in the cluster:

```
$ kubectl get crd

NAME                                    AGE
natsclusters.nats.io                    1s
```

An example of creating a 3 node cluster is below.  The NATS operator will be
responsible of assembling the cluster and replacing pods in case of failures.

```
echo '
apiVersion: "nats.io/v1alpha2"
kind: "NatsCluster"
metadata:
  name: "example-nats-cluster"
spec:
  size: 3
  version: "1.0.4"
' | kubectl apply -f -
```

To list all the NATS clusters:

```sh
$ kubectl get natsclusters.nats.io

NAME                   AGE
example-nats-cluster   1s
```

### RBAC support

If you have RBAC enabled (for example in GKE), you can run:

```
$ kubectl apply -f https://raw.githubusercontent.com/nats-io/nats-operator/master/example/deployment-rbac.yaml
```

Then this will deploy a `nats-operator` on the `nats-io` namespace.

```
$ kubectl -n nats-io logs deployment/nats-operator
time="2018-03-23T00:45:49Z" level=info msg="nats-operator Version: 0.2.0-v1alpha2+git"
time="2018-03-23T00:45:49Z" level=info msg="Git SHA: 4040d87"
time="2018-03-23T00:45:49Z" level=info msg="Go Version: go1.9"
time="2018-03-23T00:45:49Z" level=info msg="Go OS/Arch: linux/amd64"
```

Note that the NATS operator only monitors the `NatsCluster` resources
which are created in the namespace where it was deployed, so if you
want to create a cluster you have to specify the same one as the
operator:

```
$ kubectl -n nats-io apply -f example/example-nats-cluster.yaml
natscluster "example-nats-1" created

$ kubectl -n nats-io get natsclusters
NAME             AGE
example-nats-1   6m

$ kubectl -n nats-io get pods -l nats_cluster=example-nats-1
NAME              READY     STATUS    RESTARTS   AGE
nats-2jgb0tg3sm   1/1       Running   0          7m
nats-h8z9dckvfr   1/1       Running   0          7m
nats-px28gkx5wk   1/1       Running   0          6m
```

### TLS support

By using a pair of opaque secrets (one for the clients and then another for the routes),
it is possible to set TLS for the communication between the clients and also for the
transport between the routes:

```yaml
apiVersion: "nats.io/v1alpha2"
kind: "NatsCluster"
metadata:
  name: "nats"
spec:
  # Number of nodes in the cluster
  size: 3
  version: "1.0.4"

  tls:
    # Certificates to secure the NATS client connections:
    serverSecret: "nats-clients-tls"

    # Certificates to secure the routes.
    routesSecret: "nats-routes-tls"
```

In order for TLS to be properly established between the nodes, it is 
necessary to create a wildcard certificate that matches the subdomain
created for the service from the clients and the one for the routes.

The `serverSecret` has to provide the files: `ca.pem`, `route-key.pem`, `route.pem`,
for the CA, server private and public key respectively.

```
$ kubectl create secret generic nats-routes-tls --from-file=ca.pem --from-file=route-key.pem --from-file=route.pem
```

Similarly, the `clientSecret` has to provide the files: `ca.pem`, `server-key.pem`, and `server.pem`
for the CA, server private key and public key used to secure the connection
with the clients.

```
$ kubectl create secret generic nats-clients-tls --from-file=ca.pem --from-file=server-key.pem --from-file=server.pem
```

## Development

### Building the Docker Image

To build the `nats-operator` Docker image:

```sh
$ docker build -t <image>:<tag> .
```

You'll need Docker `17.06.0-ce` or higher.

### Updating the `NatsCluster` type (code generation)

If you are adding a new field to the `NatsCluster`, then you have to
get the `deepcopy-gen` tools first.

```
$ go get -u github.com/kubernetes/gengo/examples/deepcopy-gen
```

Then run the code generation script in order to recreate
`pkg/spec/zz_generated.deepcopy.go` with the required methods to
support that field filled in:

```sh
$ ./hack/codegen.sh
```

### Running outside the cluster for debugging

For debugging purposes, it is also possible to run the operator 
outside of the cluster without having to build an image:

```
MY_POD_NAMESPACE=default MY_POD_NAME=nats-operator go run cmd/operator/main.go --debug-kube-config-path=$HOME/.kube/config
```
