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
BUILDDATE = $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')

LDFLAGS := "-X github.com/clusterpedia-io/clusterpedia/pkg/version.gitVersion=$(GIT_VERSION) \
				-X github.com/clusterpedia-io/clusterpedia/pkg/version.gitCommit=$(GIT_COMMIT_HASH) \
				-X github.com/clusterpedia-io/clusterpedia/pkg/version.gitTreeState=$(GIT_TREESTATE) \
				-X github.com/clusterpedia-io/clusterpedia/pkg/version.buildDate=$(BUILDDATE)"

VERSION ?= "latest"
LATEST_TAG=$(shell git describe --tags)
ifeq ($(LATEST_TAG),$(shell git describe --abbrev=0 --tags))
	VERSION=$(LATEST_TAG)
endif

all: apiserver clustersynchro-manager

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

.PHONY: test
test:
	go test -race -cover -v ./pkg/...

.PHONY: clean
clean: clean-images
	rm -rf bin

.PHONY: apiserver
apiserver:
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build \
			   -ldflags $(LDFLAGS) \
			   -o bin/apiserver \
			   cmd/apiserver/main.go

.PHONY: clustersynchro-manager
clustersynchro-manager:
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build \
			   -ldflags $(LDFLAGS) \
			   -o  bin/clustersynchro-manager \
			   cmd/clustersynchro-manager/main.go

.PHONY: images
images: image-apiserver image-clustersynchro-manager

image-apiserver:
	GOOS="linux" $(MAKE) apiserver
	docker buildx build \
		-t ${REGISTRY}/apiserver-$(GOARCH):$(VERSION) \
		--platform=linux/$(GOARCH) \
		--load \
		--build-arg BASEIMAGE=$(BASEIMAGE) \
		--build-arg BINNAME=apiserver .
	    
image-clustersynchro-manager: 
	GOOS="linux" $(MAKE) clustersynchro-manager
	docker buildx build \
		-t $(REGISTRY)/clustersynchro-manager-$(GOARCH):$(VERSION) \
		--platform=linux/$(GOARCH) \
		--load \
		--build-arg BASEIMAGE=$(BASEIMAGE) \
		--build-arg BINNAME=clustersynchro-manager .

.PHONY: push-images
push-images: push-apiserver-image push-clustersynchro-manager-image

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

clean-images: clean-apiserver-manifest clean-clustersynchro-manager-manifest
	docker images|grep $(REGISTRY)/apiserver|awk '{print $$3}'|xargs docker rmi --force
	docker images|grep $(REGISTRY)/clustersynchro-manager|awk '{print $$3}'|xargs docker rmi --force

clean-apiserver-manifest:
	docker manifest rm $(REGISTRY)/apiserver:$(VERSION) 2>/dev/null;\
	docker manifest rm $(REGISTRY)/apiserver:latest 2>/dev/null; exit 0

clean-clustersynchro-manager-manifest:
	docker manifest rm $(REGISTRY)/clustersynchro-manager:$(VERSION) 2>/dev/null;\
	docker manifest rm $(REGISTRY)/clustersynchro-manager:latest 2>/dev/null; exit 0

.PHONY: golangci-lint
golangci-lint:
ifeq (, $(shell which golangci-lint))
	GO111MODULE=on go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.45.2
GOLANGLINT_BIN=$(shell go env GOPATH)/bin/golangci-lint
else
GOLANGLINT_BIN=$(shell which golangci-lint)
endif
