ON_PLUGINS ?= true
REGISTRY ?= "ghcr.io/clusterpedia-io/clusterpedia"

GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
RELEASE_ARCHS ?= amd64 arm64

VERSION ?= $(shell git tag --points-at HEAD --sort -version:refname "v*" | head -1)
ifeq ($(VERSION),)
	VERSION="latest"
endif

BUILDER_IMAGE_TAG = $(shell git rev-parse HEAD)
GIT_DIFF = $(shell git diff --quiet >/dev/null 2>&1; if [ $$? -eq 1 ]; then echo "1"; fi)
ifeq ($(GIT_DIFF), 1)
	BUILDER_IMAGE_TAG := $(BUILDER_IMAGE_TAG)-dirty
endif

HOSTARCH = $(shell go env GOHOSTARCH)

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
ifeq ($(ON_PLUGINS), true)
	hack/builder.sh $@
else
	hack/builder-nocgo.sh $@
endif

.PHONY: binding-apiserver
binding-apiserver:
ifeq ($(ON_PLUGINS), true)
	hack/builder.sh $@
else
	hack/builder-nocgo.sh $@
endif

.PHONY: clustersynchro-manager
clustersynchro-manager:
ifeq ($(ON_PLUGINS), true)
	hack/builder.sh $@
else
	hack/builder-nocgo.sh $@
endif

.PHONY: controller-manager
controller-manager:
	hack/builder-nocgo.sh $@

.PHONY: images
images: image-builder image-apiserver image-binding-apiserver image-clustersynchro-manager image-controller-manager

image-builder:
	docker buildx build \
		-t $(REGISTRY)/builder-$(GOARCH):$(BUILDER_IMAGE_TAG) \
		--platform=linux/$(GOARCH) \
		--load \
		-f builder.dockerfile . ; \

build-image-%:
	GOARCH=$(HOSTARCH) $(MAKE) image-builder
	$(MAKE) $(subst build-,,$@)

image-apiserver:
	docker buildx build \
		-t $(REGISTRY)/apiserver-$(GOARCH):$(VERSION) \
		--platform=linux/$(GOARCH) \
		--load \
		--build-arg BUILDER_IMAGE=$(REGISTRY)/builder-$(HOSTARCH):$(BUILDER_IMAGE_TAG) \
		--build-arg BIN_NAME=apiserver .

image-binding-apiserver:
	docker buildx build \
		-t $(REGISTRY)/binding-apiserver-$(GOARCH):$(VERSION) \
		--platform=linux/$(GOARCH) \
		--load \
		--build-arg BUILDER_IMAGE=$(REGISTRY)/builder-$(HOSTARCH):$(BUILDER_IMAGE_TAG) \
		--build-arg BIN_NAME=binding-apiserver .

image-clustersynchro-manager:
	docker buildx build \
		-t $(REGISTRY)/clustersynchro-manager-$(GOARCH):$(VERSION) \
		--platform=linux/$(GOARCH) \
		--load \
		--build-arg BUILDER_IMAGE=$(REGISTRY)/builder-$(HOSTARCH):$(BUILDER_IMAGE_TAG) \
		--build-arg BIN_NAME=clustersynchro-manager .

image-controller-manager:
	docker buildx build \
		-t $(REGISTRY)/controller-manager-$(GOARCH):$(VERSION) \
		--platform=linux/$(GOARCH) \
		--load \
		--build-arg BUILDER_IMAGE=$(REGISTRY)/builder-$(HOSTARCH):$(BUILDER_IMAGE_TAG) \
		--build-arg BIN_NAME=controller-manager .

.PHONY: push-images
push-images: push-apiserver-image push-binding-apiserver-image push-clustersynchro-manager-image push-controller-manager-image

# clean manifest https://github.com/docker/cli/issues/954#issuecomment-586722447
push-builder-image: clean-builder-manifest
	set -e; \
	images=""; \
	for arch in $(RELEASE_ARCHS); do \
		GOARCH=$$arch $(MAKE) image-builder; \
		image=$(REGISTRY)/builder-$$arch:$(BUILDER_IMAGE_TAG); \
		docker push $$image; \
		images="$$images $$image"; \
		if [ $(VERSION) != latest ]; then \
			tag_image=$(REGISTRY)/builder-$$arch:$(VERSION); \
			docker tag $$image $$tag_image; \
			docker push $$tag_image; \
		fi; \
	done; \
	docker manifest create $(REGISTRY)/builder:$(BUILDER_IMAGE_TAG) $$images; \
	docker manifest push $(REGISTRY)/builder:$(BUILDER_IMAGE_TAG); \
	if [ $(VERSION) != latest ]; then \
		docker manifest create $(REGISTRY)/builder:$(VERSION) $$images; \
		docker manifest push $(REGISTRY)/builder:$(VERSION); \
	fi;

# clean manifest https://github.com/docker/cli/issues/954#issuecomment-586722447
push-apiserver-image: clean-apiserver-manifest push-builder-image
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
push-binding-apiserver-image: clean-binding-apiserver-manifest push-builder-image
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
push-clustersynchro-manager-image: clean-clustersynchro-manager-manifest push-builder-image
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
push-controller-manager-image: clean-controller-manager-manifest push-builder-image
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

clean-builder-manifest:
	docker manifest rm $(REGISTRY)/builder:$(BUILDER_IMAGE_TAG) 2>/dev/null;\
	docker manifest rm $(REGISTRY)/builder:$(VERSION) 2>/dev/null; exit 0

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
	GO111MODULE=on go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.54
GOLANGLINT_BIN=$(shell go env GOPATH)/bin/golangci-lint
else
GOLANGLINT_BIN=$(shell which golangci-lint)
endif
