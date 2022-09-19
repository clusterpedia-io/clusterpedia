BASEIMAGE ?= "alpine:3.14"
REGISTRY ?= "ghcr.io/clusterpedia-io/clusterpedia"

GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
RELEASE_ARCHS ?= amd64 arm64

# Git information, used to set clusterpedia version
GIT_VERSION ?= $(shell git describe --tags --dirty)
GIT_COMMIT_HASH ?= $(shell git rev-parse HEAD)
GIT_TREESTATE = "clean"
GIT_DIFF = $(shell git diff --quiet >/dev/null 2>&1; if [ $$? -eq 1 ]; then echo "1"; fi)
ifeq ($(GIT_DIFF), 1)
    GIT_TREESTATE = "dirty"
endif
BUILD_DATE = $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')

KUBE_DEPENDENCE_VERSION = $(shell go list -m k8s.io/kubernetes | cut -d' ' -f2)

LDFLAGS += -X github.com/clusterpedia-io/clusterpedia/pkg/version.gitVersion=$(GIT_VERSION)
LDFLAGS += -X github.com/clusterpedia-io/clusterpedia/pkg/version.gitCommit=$(GIT_COMMIT_HASH)
LDFLAGS += -X github.com/clusterpedia-io/clusterpedia/pkg/version.gitTreeState=$(GIT_TREESTATE)
LDFLAGS += -X github.com/clusterpedia-io/clusterpedia/pkg/version.buildDate=$(BUILD_DATE)

# The `client-go/pkg/version` effects the **User-Agent** when using client-go to request.
# User-Agent="<bin name>/$(GIT_VERSION) ($(GOOS)/$(GOARCH)) kubernetes/$(GIT_COMMIT_HASH)[/<component name>]"
#   eg. "clustersynchro-manager/0.4.0 (linux/amd64) kubernetes/fbf0f4f/clustersynchro-manager"
LDFLAGS += -X k8s.io/client-go/pkg/version.gitVersion=$(GIT_VERSION)
LDFLAGS += -X k8s.io/client-go/pkg/version.gitMajor=$(shell echo '$(subst v,,$(GIT_VERSION))' | cut -d. -f1)
LDFLAGS += -X k8s.io/client-go/pkg/version.gitMinor=$(shell echo '$(subst v,,$(GIT_VERSION))' | cut -d. -f2)
LDFLAGS += -X k8s.io/client-go/pkg/version.gitCommit=$(GIT_COMMIT_HASH)
LDFLAGS += -X k8s.io/client-go/pkg/version.gitTreeState=$(GIT_TREESTATE)
LDFLAGS += -X k8s.io/client-go/pkg/version.buildDate=$(BUILD_DATE)

# The `component-base/version` effects the version obtained using Kubernetes OpenAPI.
#   OpenAPI Path: /apis/clusterpedia.io/v1beta1/resources/version
#   $ kubectl version
LDFLAGS += -X k8s.io/component-base/version.gitVersion=$(KUBE_DEPENDENCE_VERSION)
LDFLAGS += -X k8s.io/component-base/version.gitMajor=$(shell echo '$(subst v,,$(KUBE_DEPENDENCE_VERSION))' | cut -d. -f1)
LDFLAGS += -X k8s.io/component-base/version.gitMinor=$(shell echo '$(subst v,,$(KUBE_DEPENDENCE_VERSION))' | cut -d. -f2)
LDFLAGS += -X k8s.io/component-base/version.gitCommit=$(GIT_COMMIT_HASH)
LDFLAGS += -X k8s.io/component-base/version.gitTreeState=$(GIT_TREESTATE)
LDFLAGS += -X k8s.io/component-base/version.buildDate=$(BUILD_DATE)

VERSION ?= "latest"
LATEST_TAG=$(shell git describe --tags)
ifeq ($(LATEST_TAG),$(shell git describe --abbrev=0 --tags))
	VERSION=$(LATEST_TAG)
endif

all: apiserver binding-apiserver clustersynchro-manager controller-manager

gen-clusterconfigs:
	./hack/gen-clusterconfigs.sh

clean-clusterconfigs:
	./hack/clean-clusterconfigs.sh

.PHONY: crds
crds:
	./hack/update-crds.sh

.PHONY: codegen
codegen:
	./hack/update-codegen.sh

.PHONY: vendor
vendor:
	./hack/update-vendor.sh

.PHONY: lint
lint: golangci-lint
	$(GOLANGLINT_BIN) run

.PHONY: lint-fix
lint-fix: golangci-lint
	$(GOLANGLINT_BIN) run --fix

.PHONY: test
test:
	go test -race -cover -v ./pkg/...

.PHONY: clean
clean: clean-images
	rm -rf bin

.PHONY: apiserver
apiserver:
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build \
			   -ldflags "$(LDFLAGS)" \
			   -o bin/apiserver \
			   cmd/apiserver/main.go

.PHONY: binding-apiserver
binding-apiserver:
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build \
			   -ldflags "$(LDFLAGS)" \
			   -o bin/binding-apiserver \
			   cmd/binding-apiserver/main.go

.PHONY: clustersynchro-manager
clustersynchro-manager:
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build \
			   -ldflags "$(LDFLAGS)" \
			   -o  bin/clustersynchro-manager \
			   cmd/clustersynchro-manager/main.go

.PHONY: controller-manager
controller-manager:
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build \
			   -ldflags "$(LDFLAGS)" \
			   -o  bin/controller-manager \
			   cmd/controller-manager/main.go

.PHONY: images
images: image-apiserver image-binding-apiserver image-clustersynchro-manager image-controller-manager

image-apiserver:
	GOOS="linux" $(MAKE) apiserver
	docker buildx build \
		-t ${REGISTRY}/apiserver-$(GOARCH):$(VERSION) \
		--platform=linux/$(GOARCH) \
		--load \
		--build-arg BASEIMAGE=$(BASEIMAGE) \
		--build-arg BINNAME=apiserver .

image-binding-apiserver:
	GOOS="linux" $(MAKE) binding-apiserver
	docker buildx build \
		-t ${REGISTRY}/binding-apiserver-$(GOARCH):$(VERSION) \
		--platform=linux/$(GOARCH) \
		--load \
		--build-arg BASEIMAGE=$(BASEIMAGE) \
		--build-arg BINNAME=binding-apiserver .

image-clustersynchro-manager: 
	GOOS="linux" $(MAKE) clustersynchro-manager
	docker buildx build \
		-t $(REGISTRY)/clustersynchro-manager-$(GOARCH):$(VERSION) \
		--platform=linux/$(GOARCH) \
		--load \
		--build-arg BASEIMAGE=$(BASEIMAGE) \
		--build-arg BINNAME=clustersynchro-manager .

image-controller-manager:
	GOOS="linux" $(MAKE) controller-manager
	docker buildx build \
		-t $(REGISTRY)/controller-manager-$(GOARCH):$(VERSION) \
		--platform=linux/$(GOARCH) \
		--load \
		--build-arg BASEIMAGE=$(BASEIMAGE) \
		--build-arg BINNAME=controller-manager .

.PHONY: push-images
push-images: push-apiserver-image push-binding-apiserver-image push-clustersynchro-manager-image push-controller-manager-imag

# clean manifest https://github.com/docker/cli/issues/954#issuecomment-586722447
push-apiserver-image: clean-apiserver-manifest
	set -e; \
	images=""; \
	for arch in $(RELEASE_ARCHS); do \
		GOARCH=$$arch $(MAKE) image-apiserver; \
		image=$(REGISTRY)/apiserver-$$arch:$(VERSION); \
		docker push $$image; \
		images="$$images $$image"; \
		if [ $(VERSION) != latest ]; then \
			latest_image=$(REGISTRY)/apiserver-$$arch:latest; \
			docker tag $$image $$latest_image; \
			docker push $$latest_image; \
		fi; \
	done; \
	docker manifest create $(REGISTRY)/apiserver:$(VERSION) $$images; \
	docker manifest push $(REGISTRY)/apiserver:$(VERSION); \
	if [ $(VERSION) != latest ]; then \
		docker manifest create $(REGISTRY)/apiserver:latest $$images; \
		docker manifest push $(REGISTRY)/apiserver:latest; \
	fi;

# clean manifest https://github.com/docker/cli/issues/954#issuecomment-586722447
push-binding-apiserver-image: clean-binding-apiserver-manifest
	set -e; \
	images=""; \
	for arch in $(RELEASE_ARCHS); do \
		GOARCH=$$arch $(MAKE) image-binding-apiserver; \
		image=$(REGISTRY)/binding-apiserver-$$arch:$(VERSION); \
		docker push $$image; \
		images="$$images $$image"; \
		if [ $(VERSION) != latest ]; then \
			latest_image=$(REGISTRY)/binding-apiserver-$$arch:latest; \
			docker tag $$image $$latest_image; \
			docker push $$latest_image; \
		fi; \
	done; \
	docker manifest create $(REGISTRY)/binding-apiserver:$(VERSION) $$images; \
	docker manifest push $(REGISTRY)/binding-apiserver:$(VERSION); \
	if [ $(VERSION) != latest ]; then \
		docker manifest create $(REGISTRY)/binding-apiserver:latest $$images; \
		docker manifest push $(REGISTRY)/binding-apiserver:latest; \
	fi;

# clean manifest https://github.com/docker/cli/issues/954#issuecomment-586722447
push-clustersynchro-manager-image: clean-clustersynchro-manager-manifest
	set -e; \
	images=""; \
	for arch in $(RELEASE_ARCHS); do \
		GOARCH=$$arch $(MAKE) image-clustersynchro-manager; \
		image=$(REGISTRY)/clustersynchro-manager-$$arch:$(VERSION); \
		docker push $$image; \
		images="$$images $$image"; \
		if [ $(VERSION) != latest ]; then \
			latest_image=$(REGISTRY)/clustersynchro-manager-$$arch:latest; \
			docker tag $$image $$latest_image; \
			docker push $$latest_image; \
		fi; \
	done; \
	docker manifest create $(REGISTRY)/clustersynchro-manager:$(VERSION) $$images; \
	docker manifest push $(REGISTRY)/clustersynchro-manager:$(VERSION); \
	if [ $(VERSION) != latest ]; then \
		docker manifest create $(REGISTRY)/clustersynchro-manager:latest $$images; \
		docker manifest push $(REGISTRY)/clustersynchro-manager:latest; \
	fi;

# clean manifest https://github.com/docker/cli/issues/954#issuecomment-586722447
push-controller-manager-image: clean-controller-manager-manifest
	set -e; \
	images=""; \
	for arch in $(RELEASE_ARCHS); do \
		GOARCH=$$arch $(MAKE) image-controller-manager; \
		image=$(REGISTRY)/controller-manager-$$arch:$(VERSION); \
		docker push $$image; \
		images="$$images $$image"; \
		if [ $(VERSION) != latest ]; then \
			latest_image=$(REGISTRY)/controller-manager-$$arch:latest; \
			docker tag $$image $$latest_image; \
			docker push $$latest_image; \
		fi; \
	done; \
	docker manifest create $(REGISTRY)/controller-manager:$(VERSION) $$images; \
	docker manifest push $(REGISTRY)/controller-manager:$(VERSION); \
	if [ $(VERSION) != latest ]; then \
		docker manifest create $(REGISTRY)/controller-manager:latest $$images; \
		docker manifest push $(REGISTRY)/controller-manager:latest; \
	fi;

clean-images: clean-apiserver-manifest clean-clustersynchro-manager-manifest
	docker images|grep $(REGISTRY)/apiserver|awk '{print $$3}'|xargs docker rmi --force
	docker images|grep $(REGISTRY)/clustersynchro-manager|awk '{print $$3}'|xargs docker rmi --force

clean-apiserver-manifest:
	docker manifest rm $(REGISTRY)/apiserver:$(VERSION) 2>/dev/null;\
	docker manifest rm $(REGISTRY)/apiserver:latest 2>/dev/null; exit 0

clean-binding-apiserver-manifest:
	docker manifest rm $(REGISTRY)/binding-apiserver:$(VERSION) 2>/dev/null;\
	docker manifest rm $(REGISTRY)/binding-apiserver:latest 2>/dev/null; exit 0

clean-clustersynchro-manager-manifest:
	docker manifest rm $(REGISTRY)/clustersynchro-manager:$(VERSION) 2>/dev/null;\
	docker manifest rm $(REGISTRY)/clustersynchro-manager:latest 2>/dev/null; exit 0

clean-controller-manager-manifest:
	docker manifest rm $(REGISTRY)/controller-manager:$(VERSION) 2>/dev/null;\
	docker manifest rm $(REGISTRY)/controller-manager:latest 2>/dev/null; exit 0

.PHONY: golangci-lint
golangci-lint:
ifeq (, $(shell which golangci-lint))
	GO111MODULE=on go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.49.0
GOLANGLINT_BIN=$(shell go env GOPATH)/bin/golangci-lint
else
GOLANGLINT_BIN=$(shell which golangci-lint)
endif
