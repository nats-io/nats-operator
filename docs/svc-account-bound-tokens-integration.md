# Kubernetes Service Accounts Based NATS Authorization

In Kubernetes, there are `ServiceAccount` which can be created and bound
to a Pod by setting the `serviceAccountName` key on a Pod spec.  This
will then mount a secret JWT token that can be used to make requests
to the Kubernetes API Server (following the RBAC policy that may have
been defined for the ServiceAccount).

As of Kubernetes v1.10, there is a new
[TokenRequest API](https://github.com/kubernetes/community/blob/4a41674f6fb6fa69b3dfaf15267d908e909685d7/contributors/design-proposals/auth/bound-service-account-tokens.md)
that allows creating a new token bound to a `ServiceAccount` but with a different
audience. This means that the same token cannot be used for making
requests to the API server from Kubernetes so it offers better
security in case the token is leaked.  

For the integration of ServiceAccounts with NATS Authorization, this
`TokenRequest API` is used in order to generate an special token that is
usable only by NATS clients of a NATS service created by the NATS
Operator and stored in a secret.

![design](https://user-images.githubusercontent.com/26195/42645289-7f248028-85b2-11e8-8999-686d4d7d8864.png)

For example, in case there is an example-nats cluster created by the
Operator for a nats-user in the default namespace, then something like
the following token is generated (once decoded from base64).

```
{
 alg: "RS256",
 kid: ""
}.
{
 aud: [
  "nats://demo-nats.default.svc"
 ],
 exp: 1531177385,
 iat: 1531173785,
 iss: "api",
 kubernetes.io: {
  namespace: "default",
  secret: {
   name: "nats-user-example-nats-bound-token",
   uid: "daf558c6-83c3-11e8-bbe8-0800272d4b77"
  },
  serviceaccount: {
   name: “nats-user",
   uid: "6bf87559-83bb-11e8-bbe8-0800272d4b77"
  }
 },
 nbf: 1531173785,
 sub: "system:serviceaccount:nats-io:nats-user"
}.
[signature]
```

This token is would be stored in base64 encoded form in a Secret by
the operator in the same namespace as the ServiceAccount that is being
mapped named after the service account and NATS cluster
(`$service_account_name-$nats_cluster_name-bound-token`).

Then, a Pod has to mount the Secret in order to be able to
authenticate with the operated NATS cluster.

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: nats-user-pod
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
  containers:
    - name: nats-ops
      ...
      volumeMounts:
      - name: "token"
        mountPath: "/var/run/secrets/nats.io"
        readOnly: true
```

The servers in the NATS cluster would have loaded the latest JWT for
the service account, so the user can authenticate using that token
following the permissions set by the `NatsServiceRole`.

```sh
$ kubectl -n nats-io attach -it nats-user-pod

/go # nats-sub -s nats://nats-user:`cat /var/run/secrets/nats.io/token`@example-nats:4222 hello.world
Listening on [hello.world]
^C
/go # nats-sub -s nats://nats-admin-user:`cat /var/run/secrets/nats.io/token`@example-nats:4222 hello.world
Can't connect: nats: authorization violation
```

The client will be able to be connected to NATS for as long as the
`NatsServiceRole` exists, so if it is deleted via kubectl then it will
cause a disconnection.

```sh
$ kubectl -n nats-io get natsserviceroles
NAME              CREATED AT
nats-admin-user   7m
nats-user         7m

$ nats-sub -s nats://nats-user:`cat /var/run/secrets/nats.io/token`@example-nats:4222 hello.world
Listening on [hello.world]
Got disconnected!
```

In case the permissions from a `NatsServiceRole` change, then the
configuration reload would apply the latest rules as well.

## Full Kubernetes Config Example

A full example of creating a NATS Cluster, a new `ServiceAccount` and a
`NatsServiceRole` with permissions plus a `Pod` can be found below. 

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
    enableServiceAccounts: true # By default the account mapping is disabled
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: nats-user
---
apiVersion: nats.io/v1alpha2
kind: NatsServiceRole           # ← New CRD for mapping to ServiceAccount
metadata:
  name: nats-user               # ← Has to be the same as the ServiceAccount being mapped
  namespace: nats-io
  labels:
    nats_cluster: example-nats  # Defines to which 
spec:
  permissions:
    publish: ["foo.*", "foo.bar.quux"]
    subscribe: ["foo.bar", "greetings", "hello.world"]
---
apiVersion: v1
kind: Pod
metadata:
  name: nats-user-pod-1
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
      # Service Account Token is mounted via projected volume.
      volumeMounts:
      - name: "token"
        mountPath: "/var/run/secrets/nats.io"
        readOnly: true
```

## Full NATS config example

The NATS Operator maintains a secret for each one of the cluster that
it will be updating with the latest service account mappings with the
reloader sidecar sending HUP to the server to apply the updates,
eventually all servers converging to the same configuration. The
tokens in the config below are actual JWTs that would be used for a
couple of service accounts, one named `nats-admin-user` and another
named `nats-user`.

```js
{
  "port": 4222,
  "http_port": 8222,
  "cluster": {
    "port": 6222,
    "routes": [
      "nats://example-nats-1.example-nats-mgmt.nats-io.svc:6222",
      "nats://example-nats-2.example-nats-mgmt.nats-io.svc:6222",
      "nats://example-nats-3.example-nats-mgmt.nats-io.svc:6222"
    ]
  },
  "authorization": {
    "users": [
      {
        "username": "nats-admin-user",
        "password": "eyJhbGciOiJSUzI1NiIsImtpZCI6IiJ9.eyJhdWQiOlsibmF0czovL2V4YW1wbGUtbmF0cy5uYXRzLWlvLnN2YyJdLCJleHAiOjE1MzEyNDc1NTMsImlhdCI6MTUzMTI0Mzk1MywiaXNzIjoiYXBpIiwia3ViZXJuZXRlcy5pbyI6eyJuYW1lc3BhY2UiOiJuYXRzLWlvIiwic2VjcmV0Ijp7Im5hbWUiOiJuYXRzLWFkbWluLXVzZXItZXhhbXBsZS1uYXRzLWJvdW5kLXRva2VuIiwidWlkIjoiM2E2OTdlYTYtODQ2Ny0xMWU4LWJiZTgtMDgwMDI3MmQ0Yjc3In0sInNlcnZpY2VhY2NvdW50Ijp7Im5hbWUiOiJuYXRzLWFkbWluLXVzZXIiLCJ1aWQiOiIxNWY3ZTMwYi04NDYzLTExZTgtYmJlOC0wODAwMjcyZDRiNzcifX0sIm5iZiI6MTUzMTI0Mzk1Mywic3ViIjoic3lzdGVtOnNlcnZpY2VhY2NvdW50Om5hdHMtaW86bmF0cy1hZG1pbi11c2VyIn0.Z2z0fw1LsZ70r9KEGZidbhnX1fvP8dmWFGt-LA6iZ2HHIDRHlcahlHmPB0OgT2p-2FO10DFV5fPNK78bNpJJ4VfjDXd2_WebITlugpOjgdysdMaiX-Lun1NxJMmZpEPjMJjv8juMcW6z4lX-IZc6sVqafH14lTsG-pdV7jS4xFRZzLCGQu0lM-enE37XzuCzAxkoPqKA7Ti_DGM5rmXbeQiTBfJztpbrGtbymDPbDmze_8dGfb_lyfS5qHX0c2ZY9l02GovO1qQR8ZPPtAEx_hn4KZSs53NJ6iRLSk4kBLYrbXI1v7lCS2Fkp6JybHEkTIrOTF5EhVmuJMXFhx0qSQ",
        "permissions": {
          "publish": [
            ">"
          ],
          "subscribe": [
            ">"
          ]
        }
      },
      {
        "username": "nats-user",
        "password": "eyJhbGciOiJSUzI1NiIsImtpZCI6IiJ9.eyJhdWQiOlsibmF0czovL2V4YW1wbGUtbmF0cy5uYXRzLWlvLnN2YyJdLCJleHAiOjE1MzEyNDU3NzksImlhdCI6MTUzMTI0MjE3OSwiaXNzIjoiYXBpIiwia3ViZXJuZXRlcy5pbyI6eyJuYW1lc3BhY2UiOiJuYXRzLWlvIiwic2VjcmV0Ijp7Im5hbWUiOiJuYXRzLXVzZXItZXhhbXBsZS1uYXRzLWJvdW5kLXRva2VuIiwidWlkIjoiMTkwYWQxZDItODQ2My0xMWU4LWJiZTgtMDgwMDI3MmQ0Yjc3In0sInNlcnZpY2VhY2NvdW50Ijp7Im5hbWUiOiJuYXRzLXVzZXIiLCJ1aWQiOiIxNWU3NWM3OS04NDYzLTExZTgtYmJlOC0wODAwMjcyZDRiNzcifX0sIm5iZiI6MTUzMTI0MjE3OSwic3ViIjoic3lzdGVtOnNlcnZpY2VhY2NvdW50Om5hdHMtaW86bmF0cy11c2VyIn0.T5bIVS-RntDKelnecHPYBaqTVdfiNYRi7Cu7sulo86ZSKhiK_CPFs1m3-ZmeBjGgcrU0l0kmgOCd024_nVQwZvvdNwcrQElWiwhGPaLZiZWTEh4noF6l1xmyVQQRQK4kFKQkbbPOq1YexBE0DQIwWgno2-TW__CLmAvI2qUhm3Osgx4qI1-Y8JaB-ngiGNBJUULlxeRDL1dlZKKl2-F32YDP81j9TSjAySJ-DOGPaN7MegOaUu-YfUyButkhYG6mRyukbguHv9YjyVtJV9wiToYrpUuqWX8jfhn-XALLREyiT1rG8tdkL_AjsjFGwBjWrR0y4u-__U2nxW8m2Y-EyQ",
        "permissions": {
          "publish": [
            "foo.*",
            "foo.bar.quux"
          ],
          "subscribe": [
            "foo.bar",
            "greetings"
          ]
        }
      }
    ]
  }
}
```

The decoded `nats-admin-user` token:

```
{
 alg: "RS256",
 kid: ""
}.
{
 aud: [
  "nats://example-nats.nats-io.svc"
 ],
 exp: 1531247553,
 iat: 1531243953,
 iss: "api",
 kubernetes.io: {
  namespace: "nats-io",
  secret: {
   name: "nats-admin-user-example-nats-bound-token",
   uid: "3a697ea6-8467-11e8-bbe8-0800272d4b77"
  },
  serviceaccount: {
   name: "nats-admin-user",
   uid: "15f7e30b-8463-11e8-bbe8-0800272d4b77"
  }
 },
 nbf: 1531243953,
 sub: "system:serviceaccount:nats-io:nats-admin-user"
}.[signature]
```

The decoded `nats-user` token:

```
{
 alg: "RS256",
 kid: ""
}.
{
 aud: [
  "nats://example-nats.nats-io.svc"
 ],
 exp: 1531245779,
 iat: 1531242179,
 iss: "api",
 kubernetes.io: {
  namespace: "nats-io",
  secret: {
   name: "nats-user-example-nats-bound-token",
   uid: "190ad1d2-8463-11e8-bbe8-0800272d4b77"
  },
  serviceaccount: {
   name: "nats-user",
   uid: "15e75c79-8463-11e8-bbe8-0800272d4b77"
  }
 },
 nbf: 1531242179,
 sub: "system:serviceaccount:nats-io:nats-user"
}.[signature]
```

## Minikube Support

In order to use the service account integration, a couple of feature
flags have to be specified alongside a number of extra-config
parameters in order to sign the tokens.  These features are both part
of Kubernetes v1.10 so they are expected to mature after a few
releases (currently already at v1.11).

```sh
minikube start --feature-gates="TokenRequest=true,PodShareProcessNamespace=true" \
               --extra-config=apiserver.service-account-signing-key-file=/var/lib/localkube/certs/apiserver.key \
	       --extra-config=apiserver.service-account-issuer=api \
	       --extra-config=apiserver.service-account-api-audiences=api \
	       --extra-config=apiserver.service-account-key-file=/var/lib/localkube/certs/sa.pub
```
