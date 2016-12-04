# E2E Testing

End-to-end (e2e) testing is automated testing for real user scenarios.

## Build and run test

Prerequisites:
- a running Kubernetes cluster and associated kubeconfig. We will need to pass kubeconfig as arguments.
- Have kubeconfig file ready.
- Have NATS operator image ready.

e2e tests are written as go tests. All go test techniques applies, e.g. picking what to run, timeout length.
Let's say I want to run all tests in "test/e2e/":
```
$ go test -v ./test/e2e/ --kubeconfig "$HOME/.kube/config" --operator-image=quay.io/pires/nats-operator
```
