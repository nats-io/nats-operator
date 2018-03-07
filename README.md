# NATS Operator

[![Build Status](https://travis-ci.org/nats-io/nats-operator.svg?branch=master)](https://travis-ci.org/nats-io/nats-operator)

NATS Operator manages NATS clusters atop [Kubernetes][k8s-home], automating their creation and administration.

[k8s-home]: http://kubernetes.io

## Requirements

- Kubernetes v1.7+
- NATS Server v1.0.4

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
apiVersion: "nats.io/v1beta1"
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

## Connecting to your NATS cluster

The NATS operator will create two different services for your cluster,
one is a headless service that is used for internal communication between
the nodes in the cluster.

For example, the following manifest will create a `NatsCluster` CRD named `nats`:

```yaml
apiVersion: "nats.io/v1beta1"
kind: "NatsCluster"
metadata:
  name: "nats"
spec:
  # Number of nodes in the cluster
  size: 3

  # Must use 1.0.4 in order to allow waiting for the A record
  # from nodes in the cluster to be ready
  version: "1.0.4"
```

This will create a `nats` service and along with a headless service named `nats-routes`:

```sh
$ kubectl get svc -l nats_cluster=nats
NAME          TYPE        CLUSTER-IP     EXTERNAL-IP   PORT(S)             AGE
nats          ClusterIP   10.110.87.40   <none>        4222/TCP            5m
nats-routes   ClusterIP   None           <none>        6222/TCP,8222/TCP   5m
```

From your application, you can connect to one of the NATS clusters by using the `nats` service:

```
$ kubectl run -i --tty busybox-nats --image=busybox --restart=Never -- sh

$ nslookup nats.default.svc
Server:    10.96.0.10
Address 1: 10.96.0.10 kube-dns.kube-system.svc.cluster.local

Name:      nats.default.svc
Address 1: 10.110.87.40 nats.default.svc.cluster.local
```

The `nats-routes` headless service will bind an A record to each one 
of the pods that exist in the cluster.

```
$ nslookup nats-routes.default.svc
Server:    10.96.0.10
Address 1: 10.96.0.10 kube-dns.kube-system.svc.cluster.local

Name:      nats-routes.default.svc
Address 1: 172.17.0.10 nats-bx0qb1s1jg.nats-routes.default.svc.cluster.local
Address 2: 172.17.0.12 nats-mnmsjdw21s.nats-routes.default.svc.cluster.local
Address 3: 172.17.0.9 nats-8tlrb638qk.nats-routes.default.svc.cluster.local
```

### TLS support

By using a pair of opaque secrets (one for the clients and then another for the routes),
it is possible to set TLS for the communication between the clients and also for the
transport between the routes:

```yaml
apiVersion: "nats.io/v1beta1"
kind: "NatsCluster"
metadata:
  name: "nats"
spec:
  # Number of nodes in the cluster
  size: 3

  # Must use 1.0.4 in order to allow waiting for the A record
  # from nodes in the cluster to be ready
  version: "1.0.4"

  # Optionally toggle debug/trace
  logging:
    debug: true
    trace: true

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

```
$ docker build -t <image>:<tag> .
```

You'll need Docker `17.06.0-ce` or higher.

### Updating the `NatsCluster` type

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
