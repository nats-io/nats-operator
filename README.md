# NATS Operator

[![License Apache 2.0](https://img.shields.io/badge/License-Apache2-blue.svg)](https://www.apache.org/licenses/LICENSE-2.0)
[![Build Status](https://travis-ci.org/nats-io/nats-operator.svg?branch=master)](https://travis-ci.org/nats-io/nats-operator)
[![Version](https://d25lcipzij17d.cloudfront.net/badge.svg?id=go&type=5&v=0.2.2)](https://github.com/nats-io/nats-operator/releases/tag/v0.2.2)

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
  version: "1.1.0"
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
time="2018-06-07T15:53:17-07:00" level=info msg="nats-operator Version: 0.2.2-v1alpha2+git"
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
NAME               READY     STATUS    RESTARTS   AGE
example-nats-1-1   1/1       Running   0          7m
example-nats-1-2   1/1       Running   0          7m
example-nats-1-3   1/1       Running   0          6m
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
  version: "1.1.0"

  tls:
    # Certificates to secure the NATS client connections:
    serverSecret: "nats-clients-tls"

    # Certificates to secure the routes.
    routesSecret: "nats-routes-tls"
```

In order for TLS to be properly established between the nodes, it is 
necessary to create a wildcard certificate that matches the subdomain
created for the service from the clients and the one for the routes.

The `routesSecret` has to provide the files: `ca.pem`, `route-key.pem`, `route.pem`,
for the CA, server private and public key respectively.

```
$ kubectl create secret generic nats-routes-tls --from-file=ca.pem --from-file=route-key.pem --from-file=route.pem
```

Similarly, the `serverSecret` has to provide the files: `ca.pem`, `server-key.pem`, and `server.pem`
for the CA, server private key and public key used to secure the connection
with the clients.

```
$ kubectl create secret generic nats-clients-tls --from-file=ca.pem --from-file=server-key.pem --from-file=server.pem
```

### Authorization

#### Using ServiceAccounts

The NATS Operator can define permissions based on Roles by using any
present ServiceAccount in a namespace.  This can be enabled by setting
the `enableServiceAccounts` flag to true in the `NatsCluster` configuration.

```yaml
---
apiVersion: nats.io/v1alpha2
kind: NatsCluster
metadata:
  name: example-nats
spec:
  size: 3
  version: "1.2.0"
  pod:
    enableConfigReload: true
  auth:
    enableServiceAccounts: true
```

Permissions for a `ServiceAccount` can be set by creating a
`NatsServiceRole` for that account.  In the example below, there are
two accounts, one is an admin user that has more permissions.

```yaml
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: nats-admin-user
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: nats-user
---
apiVersion: nats.io/v1alpha2
kind: NatsServiceRole
metadata:
  name: nats-user
  namespace: nats-io

  # Specifies which NATS cluster will be mapping this account.
  labels:
    nats_cluster: example-nats
spec:
  permissions:
    publish: ["foo.*", "foo.bar.quux"]
    subscribe: ["foo.bar"]
---
apiVersion: nats.io/v1alpha2
kind: NatsServiceRole
metadata:
  name: nats-admin-user
  namespace: nats-io
  labels:
    nats_cluster: example-nats
spec:
  permissions:
    publish: [">"]
    subscribe: [">"]
```

The above will create two different Secrets which can then be mounted as volumes
for a Pod.

```sh
$ kubectl -n nats-io get secrets
NAME                                       TYPE          DATA      AGE
...
nats-admin-user-example-nats-bound-token   Opaque        1         43m
nats-user-example-nats-bound-token         Opaque        1         43m
```

An example of mounting the secret in a `Pod` can be found below:

```yaml
---
apiVersion: v1
kind: Pod
metadata:
  name: nats-user-pod
  labels:
    nats_cluster: example-nats
spec:
  volumes:
    - name: "token"
      projected:
        sources:
        - secret:
            name: "nats-user-example-nats-bound-token"
            items:
              - key: token
                path: "token"
  restartPolicy: Never
  containers:
    - name: nats-ops
      command: ["/bin/sh"]
      image: "wallyqs/nats-ops:latest"
      tty: true
      stdin: true
      stdinOnce: true
      volumeMounts:
      - name: "token"
        mountPath: "/var/run/secrets/nats.io"
```

Then within the `Pod` the token can be used to authenticate against
the server using the created token.

```sh
$ kubectl -n nats-io attach -it nats-user-pod

/go # nats-sub -s nats://nats-user:`cat /var/run/secrets/nats.io/token`@example-nats:4222 hello.world
Listening on [hello.world]
^C
/go # nats-sub -s nats://nats-admin-user:`cat /var/run/secrets/nats.io/token`@example-nats:4222 hello.world
Can't connect: nats: authorization violation
```

#### Using a single secret with the explicit configuration.

Authorization can also be set for the server by using a secret
where the permissions are defined in JSON:

```json
{
  "users": [
    { "username": "user1", "password": "secret1" },
    { "username": "user2", "password": "secret2",
      "permissions": {
	"publish": ["hello.*"],
	"subscribe": ["hello.world"]
      }
    }
  ],
  "default_permissions": {
    "publish": ["SANDBOX.*"],
    "subscribe": ["PUBLIC.>"]
  }
}
```

Example of creating a secret to set the permissions:

```sh
kubectl create secret generic nats-clients-auth --from-file=clients-auth.json
```

Now when creating a NATS cluster it is possible to set the permissions as
in the following example:

```yaml
apiVersion: "nats.io/v1alpha2"
kind: "NatsCluster"
pmetadata:
  name: "example-nats-auth"
spec:
  size: 3
  version: "1.1.0"

  auth:
    # Definition in JSON of the users permissions
    clientsAuthSecret: "nats-clients-auth"

    # How long to wait for authentication
    clientsAuthTimeout: 5
```

### Configuration Reload

On Kubernetes +v1.10 clusters that have been started with support for
sharing the process namespace (via `--feature-gates=PodShareProcessNamespace=true`),
it is possible to enable on-the-fly reloading of configuration for the
servers that are part of the cluster.  This can also be combined with the
authorization support, so in case the user permissions change, then the
servers will reload and apply the new permissions.

```yaml
apiVersion: "nats.io/v1alpha2"
kind: "NatsCluster"
metadata:
  name: "example-nats-auth"
spec:
  size: 3
  version: "1.1.0"

  pod:
    # Enable on-the-fly NATS Server config reload
    # Note: only supported in Kubernetes clusters with PID namespace sharing enabled.
    enableConfigReload: true

    # Possible to customize version of reloader image
    reloaderImage: connecteverything/nats-server-config-reloader
    reloaderImageTag: "0.2.2-v1alpha2"
    reloaderImagePullPolicy: "IfNotPresent"
  auth:
    # Definition in JSON of the users permissions
    clientsAuthSecret: "nats-clients-auth"

    # How long to wait for authentication
    clientsAuthTimeout: 5
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
