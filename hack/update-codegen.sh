#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

REPO_ROOT=$(git rev-parse --show-toplevel)
cd "${REPO_ROOT}"

#echo "Generating with register-gen"
#GO111MODULE=on go install k8s.io/code-generator/cmd/register-gen
#register-gen \
#    --go-header-file hack/boilerplate.go.txt \
#    --input-dirs ./pkg/apis/cluster/v1alpha1 \

echo "Generating with deepcopy-gen"
GO111MODULE=on go install k8s.io/code-generator/cmd/deepcopy-gen
deepcopy-gen \
    --go-header-file hack/boilerplate.go.txt \
    --input-dirs=./pkg/apis/cluster/v1alpha2 \
    --output-file-base=zz_generated.deepcopy
deepcopy-gen \
    --go-header-file hack/boilerplate.go.txt \
    --input-dirs=./pkg/apis/clusterpedia/v1beta1 \
    --output-file-base=zz_generated.deepcopy
deepcopy-gen \
    --go-header-file hack/boilerplate.go.txt \
    --input-dirs=./pkg/apis/clusterpedia \
    --output-file-base=zz_generated.deepcopy

echo "Generating with conversion-gen"
GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen
conversion-gen \
    --go-header-file hack/boilerplate.go.txt \
    --input-dirs=./pkg/apis/clusterpedia/v1beta1 \
    --output-file-base=zz_generated.conversion

echo "Generating with client-gen"
GO111MODULE=on go install k8s.io/code-generator/cmd/client-gen
client-gen \
    --go-header-file hack/boilerplate.go.txt \
    --input-base="" \
    --input=github.com/clusterpedia-io/clusterpedia/pkg/apis/cluster/v1alpha2 \
    --output-package=github.com/clusterpedia-io/clusterpedia/pkg/generated/clientset \
    --clientset-name=versioned

echo "Generating with lister-gen"
GO111MODULE=on go install k8s.io/code-generator/cmd/lister-gen
lister-gen \
  --go-header-file hack/boilerplate.go.txt \
  --input-dirs=github.com/clusterpedia-io/clusterpedia/pkg/apis/cluster/v1alpha2 \
  --output-package=github.com/clusterpedia-io/clusterpedia/pkg/generated/listers

echo "Generating with informer-gen"
eO111MODULE=on go install k8s.io/code-generator/cmd/informer-gen
informer-gen \
  --go-header-file hack/boilerplate.go.txt \
  --input-dirs=github.com/clusterpedia-io/clusterpedia/pkg/apis/cluster/v1alpha2 \
  --versioned-clientset-package=github.com/clusterpedia-io/clusterpedia/pkg/generated/clientset/versioned \
  --listers-package=github.com/clusterpedia-io/clusterpedia/pkg/generated/listers \
  --output-package=github.com/clusterpedia-io/clusterpedia/pkg/generated/informers

#echo "Generating with openapi-gen"
#GO111MODULE=on go install k8s.io/code-generator/cmd/openapi-gen
#openapi-gen \
#  --go-header-file hack/boilerplate.go.txt \
#  --input-dirs=github.com/clusterpedia-io/clusterpedia/pkg/apis/pedia/v1alpha1 \
#  --input-dirs "k8s.io/apimachinery/pkg/apis/meta/v1,k8s.io/apimachinery/pkg/runtime,k8s.io/apimachinery/pkg/version" \
#  --output-package=github.com/clusterpedia-io/clusterpedia/pkg/generated/openapi \
#  -O zz_generated.openapi
