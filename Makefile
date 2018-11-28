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

# gen executes the code generation step.
.PHONY: gen
gen: dep
	@./hack/codegen.sh
