# Version
GIT_HEAD_COMMIT ?= $(shell git rev-parse --short HEAD)
VERSION         ?= $(or $(shell git describe --abbrev=0 --tags --match "v*" 2>/dev/null),$(GIT_HEAD_COMMIT))
GOOS                 ?= $(shell go env GOOS)
GOARCH               ?= $(shell go env GOARCH)

# Defaults
REGISTRY        ?= ghcr.io
REPOSITORY      ?= corentinptrl/capsule-coredns
GIT_TAG_COMMIT  ?= $(shell git rev-parse --short $(VERSION))
GIT_MODIFIED_1  ?= $(shell git diff $(GIT_HEAD_COMMIT) $(GIT_TAG_COMMIT) --quiet && echo "" || echo ".dev")
GIT_MODIFIED_2  ?= $(shell git diff --quiet && echo "" || echo ".dirty")
GIT_MODIFIED    ?= $(shell echo "$(GIT_MODIFIED_1)$(GIT_MODIFIED_2)")
GIT_REPO        ?= $(shell git config --get remote.origin.url)
BUILD_DATE      ?= $(shell git log -1 --format="%at" | xargs -I{} sh -c 'if [ "$(shell uname)" = "Darwin" ]; then date -r {} +%Y-%m-%dT%H:%M:%S; else date -d @{} +%Y-%m-%dT%H:%M:%S; fi')
IMG_BASE        ?= $(REPOSITORY)
IMG             ?= $(IMG_BASE):$(VERSION)
CAPSULE_IMG     ?= $(REGISTRY)/$(IMG_BASE)
CAPSULE_VERSION ?= "0.12.4"
CLUSTER_NAME    ?= capsule-coredns

## Kubernetes Version Support
KUBERNETES_SUPPORTED_VERSION ?= "v1.34.0"

## Tool Binaries
KUBECTL ?= kubectl
HELM ?= helm

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

# Options for 'bundle-build'
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Sorting imports
.PHONY: goimports
goimports:
	goimports -w -l -local "github.com/projectcapsule/capsule" .

# Linting code as PR is expecting
.PHONY: golint
golint: golangci-lint
	$(GOLANGCI_LINT) run -c .golangci.yaml --verbose

.PHONY: golint-fix
golint-fix: golangci-lint
	$(GOLANGCI_LINT) run -c .golangci.yaml --verbose --fix

GOLANGCI_LINT          := $(LOCALBIN)/golangci-lint
GOLANGCI_LINT_VERSION  := v2.8.0
GOLANGCI_LINT_LOOKUP   := golangci/golangci-lint
golangci-lint: ## Download golangci-lint locally if necessary.
	@test -s $(GOLANGCI_LINT) && $(GOLANGCI_LINT) -h | grep -q $(GOLANGCI_LINT_VERSION) || \
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/$(GOLANGCI_LINT_LOOKUP)/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION))

GINKGO := $(LOCALBIN)/ginkgo
ginkgo:
	$(call go-install-tool,$(GINKGO),github.com/onsi/ginkgo/v2/ginkgo)

CT         := $(LOCALBIN)/ct
CT_VERSION := v3.14.0
CT_LOOKUP  := helm/chart-testing
ct:
	@test -s $(CT) && $(CT) version | grep -q $(CT_VERSION) || \
	$(call go-install-tool,$(CT),github.com/$(CT_LOOKUP)/v3/ct@$(CT_VERSION))

KIND         := $(LOCALBIN)/kind
KIND_VERSION := v0.30.0
KIND_LOOKUP  := kubernetes-sigs/kind
kind:
	@test -s $(KIND) && $(KIND) --version | grep -q $(KIND_VERSION) || \
	$(call go-install-tool,$(KIND),sigs.k8s.io/kind/cmd/kind@$(KIND_VERSION))

# go-install-tool will 'go install' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-install-tool
[ -f $(1) ] || { \
    set -e ;\
    GOBIN=$(LOCALBIN) go install $(2) ;\
}
endef

COREDNS_VERSION ?= v1.13.2

.PHONY: docker-build

docker-build:
	@if [ ! -d coredns ]; then \
		git clone --branch $(COREDNS_VERSION) https://github.com/coredns/coredns; \
	fi
	@mkdir -p coredns/plugin/capsule
	@cp -f *.go coredns/plugin/capsule/
	@cp -f go.mod coredns/plugin/capsule/
	@grep -q '^replace github.com/CorentinPtrl/capsule_coredns => ./plugin/capsule' coredns/go.mod || \
		sed -i '/^go /a replace github.com/CorentinPtrl/capsule_coredns => ./plugin/capsule' coredns/go.mod
	@sed -i '/^capsule:github.com\/CorentinPtrl\/capsule_coredns/d' coredns/plugin.cfg
	@grep -q '^capsule:github.com/CorentinPtrl/capsule_coredns' coredns/plugin.cfg || \
		sed -i '/kubernetes:kubernetes/i capsule:github.com/CorentinPtrl/capsule_coredns' coredns/plugin.cfg
	@cd coredns && GOFLAGS=-mod=mod go generate
	@cd coredns && go mod tidy
	@cd coredns && GOFLAGS="-buildvcs=false" make gen
	@cd coredns && GOFLAGS="-buildvcs=false" make
	@docker build \
		--no-cache \
		-t $(CAPSULE_IMG):latest \
		-t $(CAPSULE_IMG):$(VERSION) \
		-f coredns/Dockerfile coredns


# Running e2e tests in a KinD instance
.PHONY: e2e
e2e: ginkgo
	$(MAKE) docker-build && $(MAKE) e2e-build && $(MAKE) e2e-exec && $(MAKE) e2e-destroy

e2e-build: kind
	$(MAKE) e2e-build-cluster
	$(MAKE) e2e-load-image
	$(MAKE) e2e-install

e2e-build-cluster: kind
	$(KIND) create cluster --wait=60s --name $(CLUSTER_NAME) --image kindest/node:$(KUBERNETES_SUPPORTED_VERSION)

.PHONY: e2e-load-image
e2e-load-image: kind
	$(KIND) load docker-image $(CAPSULE_IMG):latest --name $(CLUSTER_NAME)

.PHONY: e2e-install
e2e-install:
	$(HELM) upgrade \
	    --dependency-update \
		--debug \
		--install \
		--namespace capsule-system \
		--create-namespace \
		 --version $(CAPSULE_VERSION) \
		--set 'manager.livenessProbe.failureThreshold=10' \
		--set 'webhooks.hooks.nodes.enabled=true' \
		--set "webhooks.exclusive=true"\
		capsule \
		oci://ghcr.io/projectcapsule/charts/capsule
	@$(KUBECTL) apply -f hack/coredns.yaml

.PHONY: e2e-exec
e2e-exec: ginkgo
	$(GINKGO) -v -tags e2e ./e2e

.PHONY: e2e-destroy
e2e-destroy: kind
	$(KIND) delete cluster --name $(CLUSTER_NAME)
