# NATS Operator

[![License Apache 2.0](https://img.shields.io/badge/License-Apache2-blue.svg)](https://www.apache.org/licenses/LICENSE-2.0)
[![Build Status](https://travis-ci.org/nats-io/nats-operator.svg?branch=master)](https://travis-ci.org/nats-io/nats-operator)
[![Version](https://d25lcipzij17d.cloudfront.net/badge.svg?id=go&type=5&v=0.6.0)](https://github.com/nats-io/nats-operator/releases/tag/v0.6.0)

NATS Operator manages NATS clusters atop [Kubernetes][k8s-home] using [CRDs](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/).  If looking to run NATS on K8S without the operator you can also find [Helm charts in the nats-io/k8s repo](https://github.com/nats-io/k8s#helm-charts-for-nats). You can also find more info about running NATS on Kubernetes in the [docs](https://docs.nats.io/nats-on-kubernetes/nats-kubernetes) as well as a minimal setup using `StatefulSets` only without using the operator to get started [here](https://docs.nats.io/nats-on-kubernetes/minimal-setup).

[k8s-home]: http://kubernetes.io

## Requirements

- Kubernetes v1.10+.
  - [Configuration reloading](#configuration-reload) is only supported in Kubernetes v1.12+.
  - [Authentication using service accounts](#auth-service-accounts) is only supported in Kubernetes v1.12+ having the `TokenRequest` API enabled.

## Introduction

NATS Operator provides a `NatsCluster` [Custom Resources Definition](https://kubernetes.io/docs/tasks/access-kubernetes-api/extend-api-custom-resource-definitions/) (CRD) that models a NATS cluster.
This CRD allows for specifying the desired size and version for a NATS cluster, as well as several other advanced options:

```yaml
apiVersion: nats.io/v1alpha2
kind: NatsCluster
metadata:
  name: example-nats-cluster
spec:
  size: 3
  version: "2.0.0"
```

NATS Operator monitors creation/modification/deletion of `NatsCluster` resources and reacts by attempting to perform the any necessary operations on the associated NATS clusters in order to align their current status with the desired one.

## Installing

NATS Operator supports two different operation modes:

* **Namespace-scoped (classic):** NATS Operator manages `NatsCluster` resources on the Kubernetes namespace where it is deployed.
* **Cluster-scoped (experimental):** NATS Operator manages `NatsCluster` resources across all namespaces in the Kubernetes cluster.

The operation mode must be chosen when installing NATS Operator and cannot be changed later.

### Namespace-scoped installation

To perform a namespace-scoped installation of NATS Operator in the Kubernetes cluster pointed at by the current context, you may run:

```console
$ kubectl apply -f https://github.com/nats-io/nats-operator/releases/latest/download/00-prereqs.yaml
$ kubectl apply -f https://github.com/nats-io/nats-operator/releases/latest/download/10-deployment.yaml
``` 

This will, by default, install NATS Operator in the `default` namespace and observe `NatsCluster` resources created in the `default` namespace, alone.
In order to install in a different namespace, you must first create said namespace and edit the manifests above in order to specify its name wherever necessary.

**WARNING:** To perform multiple namespace-scoped installations of NATS Operator, you must manually edit the `nats-operator-binding` cluster role binding in `deploy/00-prereqs.yaml` file in order to add all the required service accounts.
Failing to do so may cause all NATS Operator instances to malfunction.

**WARNING:** When performing a namespace-scoped installation of NATS Operator, you must make sure that all other namespace-scoped installations that may exist in the Kubernetes cluster share the same version.
Installing different versions of NATS Operator in the same Kubernetes cluster may cause unexpected behavior as the schema of the CRDs which NATS Operator registers may change between versions.

Alternatively, you may use [Helm](https://www.helm.sh/) to perform a namespace-scoped installation of NATS Operator.
To do so you may go to [helm/nats-operator](https://github.com/nats-io/nats-operator/tree/master/helm/nats-operator) and use the Helm charts found in that repo.


### Cluster-scoped installation (experimental)

Cluster-scoped installations of NATS Operator must live in the `nats-io` namespace.
This namespace must be created beforehand:

```console
$ kubectl create ns nats-io
```

Then, you must manually edit the manifests in `deployment/` in order to reference the `nats-io` namespace and to enable the `ClusterScoped` feature gate in the NATS Operator deployment.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nats-operator
  namespace: nats-io
spec:
  (...)
    spec:
      containers:
      - name: nats-operator
        (...)
        args:
        - nats-operator
        - --feature-gates=ClusterScoped=true
        (...)
```

Once you have done this, you may install NATS Operator by running:

```console
$ kubectl apply -f https://github.com/nats-io/nats-operator/releases/latest/download/00-prereqs.yaml
$ kubectl apply -f https://github.com/nats-io/nats-operator/releases/latest/download/10-deployment.yaml
``` 

**WARNING:** When performing a cluster-scoped installation of NATS Operator, you must make sure that there are no other deployments of NATS Operator in the Kubernetes cluster.
If you have a previous installation of NATS Operator, you must uninstall it before performing a cluster-scoped installation of NATS Operator.  

## Creating a NATS cluster

Once NATS Operator has been installed, you will be able to confirm that two new CRDs have been registered in the cluster:

```console
$ kubectl get crd
NAME                       CREATED AT
natsclusters.nats.io       2019-01-11T17:16:36Z
natsserviceroles.nats.io   2019-01-11T17:16:40Z
```

To create a NATS cluster, you must create a `NatsCluster` resource representing the desired status of the cluster.
For example, to create a 3-node NATS cluster you may run:

```console
$ cat <<EOF | kubectl create -f -
apiVersion: nats.io/v1alpha2
kind: NatsCluster
metadata:
  name: example-nats-cluster
spec:
  size: 3
  version: "1.3.0"
EOF
```

NATS Operator will react to the creation of such a resource by creating three NATS pods.
These pods will keep being monitored (and replaced in case of failure) by NATS Operator for as long as this `NatsCluster` resource exists.

## Listing NATS clusters

To list all the NATS clusters:

```sh
$ kubectl get nats --all-namespaces
NAMESPACE   NAME                   AGE
default     example-nats-cluster   2m
```

## TLS support

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
  version: "1.3.0"

  tls:
    # Certificates to secure the NATS client connections:
    serverSecret: "nats-clients-tls"

    # Certificates to secure the routes.
    routesSecret: "nats-routes-tls"
```

In order for TLS to be properly established between the nodes, it is 
necessary to create a wildcard certificate that matches the subdomain
created for the service from the clients and the one for the routes.

By default, the `routesSecret` has to provide the files: `ca.pem`, `route-key.pem`, `route.pem`,
for the CA, server private and public key respectively.

```
$ kubectl create secret generic nats-routes-tls --from-file=ca.pem --from-file=route-key.pem --from-file=route.pem
```

Similarly, by default the `serverSecret` has to provide the files: `ca.pem`, `server-key.pem`, and `server.pem`
for the CA, server private key and public key used to secure the connection
with the clients.

```
$ kubectl create secret generic nats-clients-tls --from-file=ca.pem --from-file=server-key.pem --from-file=server.pem
```

NATS also supports kubernetes.io/tls secrets (like the ones managed by cert-manager) and any secrets containing a CA, private and public keys with arbitrary names.
It is possible to overwrite the default names as follows:

```yaml
apiVersion: "nats.io/v1alpha2"
kind: "NatsCluster"
metadata:
  name: "nats"
spec:
  # Number of nodes in the cluster
  size: 3
  version: "1.3.0"

  tls:
    # Certificates to secure the NATS client connections:
    serverSecret: "nats-clients-tls"
    # Name of the CA in serverSecret
    serverSecretCAFileName: "ca.crt"
    # Name of the key in serverSecret
    serverSecretKeyFileName: "tls.key"
    # Name of the certificate in serverSecret
    serverSecretCertFileName: "tls.crt"

    # Certificates to secure the routes.
    routesSecret: "nats-routes-tls"
    # Name of the CA in routesSecret
    routesSecretCAFileName: "ca.crt"
    # Name of the key in routesSecret
    routesSecretKeyFileName: "tls.key"
    # Name of the certificate in routesSecret
    routesSecretCertFileName: "tls.crt"
```

### Cert-Manager

If [cert-manager](https://github.com/jetstack/cert-manager) is available in your cluster, you can easily generate TLS certificates for NATS as follows:

Create a self-signed cluster issuer (or namespace-bound issuer) to create NATS' CA certificate:

```yaml
apiVersion: cert-manager.io/v1alpha2
kind: ClusterIssuer
metadata:
  name: selfsigning
spec:
  selfSigned: {}
```

Create your NATS cluster's CA certificate using the new `selfsigning` issuer:

```yaml
apiVersion: cert-manager.io/v1alpha2
kind: Certificate
metadata:
  name: nats-ca
spec:
  secretName: nats-ca
  duration: 8736h # 1 year
  renewBefore: 240h # 10 days
  issuerRef:
    name: selfsigning
    kind: ClusterIssuer
  commonName: nats-ca
  usages: 
    - cert sign # workaround for odd cert-manager behavior
  organization:
  - Your organization
  isCA: true
```

Create your NATS cluster issuer based on the new `nats-ca` CA:

```yaml
apiVersion: cert-manager.io/v1alpha2
kind: Issuer
metadata:
  name: nats-ca
spec:
  ca:
    secretName: nats-ca
```

Create your NATS cluster's server certificate (assuming NATS is running in the `nats-io` namespace, otherwise, set the `commonName` and `dnsNames` fields appropriately):

```yaml
apiVersion: cert-manager.io/v1alpha2
kind: Certificate
metadata:
  name: nats-server-tls
spec:
  secretName: nats-server-tls
  duration: 2160h # 90 days
  renewBefore: 240h # 10 days
  usages:
  - signing
  - key encipherment
  - server auth
  issuerRef:
    name: nats-ca
    kind: Issuer
  organization:
  - Your organization
  commonName: nats.nats-io.svc.cluster.local
  dnsNames:
  - nats.nats-io.svc
```

Create your NATS cluster's routes certificate (assuming NATS is running in the `nats-io` namespace, otherwise, set the `commonName` and `dnsNames` fields appropriately):

```yaml
apiVersion: cert-manager.io/v1alpha2
kind: Certificate
metadata:
  name: nats-routes-tls
spec:
  secretName: nats-routes-tls
  duration: 2160h # 90 days
  renewBefore: 240h # 10 days
  usages:
  - signing
  - key encipherment
  - server auth
  - client auth # included because routes mutually verify each other
  issuerRef:
    name: nats-ca
    kind: Issuer
  organization:
  - Your organization
  commonName: "*.nats-mgmt.nats-io.svc.cluster.local"
  dnsNames:
  - "*.nats-mgmt.nats-io.svc"
```

### Authorization

<a name="auth-service-accounts"></a>
#### Using ServiceAccounts

The NATS Operator can define permissions based on Roles by using any present ServiceAccount in a namespace.
This feature requires a Kubernetes v1.12+ cluster having the `TokenRequest` API enabled.
To try this feature using `minikube` v0.30.0+, you can configure it to start as follows:

```console
$ minikube start \
  --extra-config=apiserver.service-account-signing-key-file=/var/lib/minikube/certs/apiserver.key \
  --extra-config=apiserver.service-account-issuer=api \
  --extra-config=apiserver.service-account-api-audiences=api \
  --kubernetes-version=v1.12.4
```

Please note that availability of this feature across Kubernetes offerings may vary widely.

ServiceAccounts integration can then be enabled by setting the
`enableServiceAccounts` flag to true in the `NatsCluster` configuration.

```yaml
apiVersion: nats.io/v1alpha2
kind: NatsCluster
metadata:
  name: example-nats
spec:
  size: 3
  version: "1.3.0"

  pod:
    # NOTE: Only supported in Kubernetes v1.12+.
    enableConfigReload: true
  auth:
    # NOTE: Only supported in Kubernetes v1.12+ clusters having the "TokenRequest" API enabled.
    enableServiceAccounts: true
```

Permissions for a `ServiceAccount` can be set by creating a
`NatsServiceRole` for that account.  In the example below, there are
two accounts, one is an admin user that has more permissions.

```yaml
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

Please note that `NatsServiceRole` must be created in the same namespace as 
`NatsCluster` is running, but `bound-token` will be created for `ServiceAccount` 
resources that can be placed in various namespaces.

An example of mounting the secret in a `Pod` can be found below:

```yaml
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

<a name="configuration-reload"></a>
### Configuration Reload

On Kubernetes v1.12+ clusters it is possible to enable on-the-fly reloading of configuration for the servers that are part of the cluster.
This can also be combined with the authorization support, so in case the user permissions change, then the servers will reload and apply the new permissions.

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
    # NOTE: Only supported in Kubernetes v1.12+.
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

## Connecting operated NATS clusters to external NATS clusters

By using the `extraRoutes` field on the spec you can make the operated
NATS cluster create routes against clusters outside of Kubernetes:

```yaml
apiVersion: "nats.io/v1alpha2"
kind: "NatsCluster"
metadata:
  name: "nats"
spec:
  size: 3
  version: "1.4.1"

  extraRoutes:
    - route: "nats://nats-a.example.com:6222"
    - route: "nats://nats-b.example.com:6222"
    - route: "nats://nats-c.example.com:6222"
```

It is also possible to connect to another operated NATS cluster as follows:

```yaml
apiVersion: "nats.io/v1alpha2"
kind: "NatsCluster"
metadata:
  name: "nats-v2-2"
spec:
  size: 3
  version: "1.4.1"

  extraRoutes:
    - cluster: "nats-v2-1"
```

## Development

### Building the Docker Image

To build the `nats-operator` Docker image:

```sh
$ docker build -f docker/operator/Dockerfile . -t <image:tag>
```

To build the `nats-server-config-reloader`:

```sh
$ docker build -f docker/reloader/Dockerfile . -t <image:tag>
```

You'll need Docker `17.06.0-ce` or higher.
