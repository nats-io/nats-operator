SHELL := /bin/bash

# build.e2e builds the nats-operator-e2e test binary.
.PHONY: build.e2e
build.e2e:
	@GOOS=linux GOARCH=amd64 go test -c -o build/nats-operator-e2e ./test/e2e/*.go

# build.operator builds the nats-operator binary.
.PHONY: build.operator
build.operator: gen
	@GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build \
		-ldflags "-d -s -w -X github.com/nats-io/nats-operator/version.GitSHA=`git rev-parse --short HEAD`" \
		-tags netgo \
		-installsuffix cgo \
		-o build/nats-operator ./cmd/operator/main.go

# dep fetches required dependencies.
.PHONY: dep
dep: KUBERNETES_VERSION := 1.12.2
dep: KUBERNETES_CODE_GENERATOR_PKG := k8s.io/code-generator
dep: KUBERNETES_APIMACHINERY_PKG := k8s.io/apimachinery
dep:
	@dep ensure -v
	@go get -d $(KUBERNETES_CODE_GENERATOR_PKG)/...
	@cd $(GOPATH)/src/$(KUBERNETES_CODE_GENERATOR_PKG) && \
		git fetch origin && \
		git checkout -f kubernetes-$(KUBERNETES_VERSION) --quiet
	@go get -d $(KUBERNETES_APIMACHINERY_PKG)/...
	@cd $(GOPATH)/src/$(KUBERNETES_APIMACHINERY_PKG) && \
		git fetch origin && \
		git checkout -f kubernetes-$(KUBERNETES_VERSION) --quiet

# e2e runs the end-to-end test suite.
.PHONY: e2e
e2e: KUBECONFIG ?= $(HOME)/.kube/config
e2e:
	@./test/prepare-secrets.sh
	MODE=run PROFILE=local TARGET=operator $(MAKE) run
	MODE=run PROFILE=local TARGET=e2e $(MAKE) run
	@go test -v ./test/e2e/main_test.go -kubeconfig $(KUBECONFIG) -wait

# run deploys either nats-operator or nats-operator-e2e to the Kubernetes cluster targeted by the current kubeconfig.
.PHONY: run
.SECONDEXPANSION:
run: MODE ?= dev
run: PROFILE ?= local
run: TARGET ?= operator
run: build.$$(TARGET)
run:
	@skaffold $(MODE) -f $(PWD)/hack/skaffold/$(TARGET)/skaffold.yml -p $(PROFILE)

# gen executes the code generation step.
.PHONY: gen
gen: dep
	@./hack/codegen.sh
