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

# run deploys nats-operator to the Kubernetes cluster targeted by the current kubeconfig.
.PHONY: run
run: MODE ?= dev
run: PROFILE ?= local
run: build.operator
run:
	@skaffold $(MODE) -f $(PWD)/hack/skaffold/operator/skaffold.yml -p $(PROFILE)

# gen executes the code generation step.
.PHONY: gen
gen: dep
	@./hack/codegen.sh
