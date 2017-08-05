# nats-operator

[![Build Status](https://travis-ci.org/pires/nats-operator.svg?branch=master)](https://travis-ci.org/pires/nats-operator)
[![Docker Repository on Quay](https://quay.io/repository/pires/nats-operator/status "Docker Repository on Quay")](https://quay.io/repository/pires/nats-operator)

`nats-operator` manages NATS clusters atop [Kubernetes][k8s-home], automating their creation and administration.

**Project status: *ALPHA***

The API, spec, status and other user facing objects are subject to change.
We do not support backward-compatibility for the alpha releases.

## Requirements

- Kubernetes 1.7+

[k8s-home]: http://kubernetes.io

## Building the Image

To build the `nats-operator` Docker image:

```
$ docker build -t <image>:<tag> .
```

You'll need Docker `17.06.0-ce` or higher.
