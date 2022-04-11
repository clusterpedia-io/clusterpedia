#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

REPO_ROOT=$(git rev-parse --show-toplevel)
API_ROOT="${REPO_ROOT}/staging/src/github.com/clusterpedia-io/api"

# GO111MODULE=on go install k8s.io/code-generator/cmd/register-gen
GO111MODULE=on go install k8s.io/code-generator/cmd/deepcopy-gen
GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen
GO111MODULE=on go install k8s.io/code-generator/cmd/client-gen
GO111MODULE=on go install k8s.io/code-generator/cmd/lister-gen
GO111MODULE=on go install k8s.io/code-generator/cmd/informer-gen
# GO111MODULE=on go install k8s.io/code-generator/cmd/openapi-gen

echo "change directory: ${API_ROOT}"
cd "${API_ROOT}"

#echo "Generating with register-gen"
#register-gen \
#    --go-header-file="${REPO_ROOT}/hack/boilerplate.go.txt" \
#    --input-dirs="./cluster/v1alpha1"

echo "Generating with deepcopy-gen"
deepcopy-gen \
    --go-header-file="${REPO_ROOT}/hack/boilerplate.go.txt" \
    --input-dirs="./cluster/v1alpha2" \
    --output-file-base="zz_generated.deepcopy"
deepcopy-gen \
    --go-header-file="${REPO_ROOT}/hack/boilerplate.go.txt" \
    --input-dirs="./clusterpedia/v1beta1" \
    --output-file-base="zz_generated.deepcopy"
deepcopy-gen \
    --go-header-file="${REPO_ROOT}/hack/boilerplate.go.txt" \
    --input-dirs="./clusterpedia" \
    --output-file-base="zz_generated.deepcopy"

echo "Generating with conversion-gen"
conversion-gen \
    --go-header-file="${REPO_ROOT}/hack/boilerplate.go.txt" \
    --input-dirs="./clusterpedia/v1beta1" \
    --output-file-base="zz_generated.conversion"

echo "change directory: ${REPO_ROOT}"
cd "${REPO_ROOT}"

echo "Generating with client-gen"
client-gen \
    --go-header-file="hack/boilerplate.go.txt" \
    --input-base="github.com/clusterpedia-io/api" \
    --input="cluster/v1alpha2" \
    --output-package="github.com/clusterpedia-io/clusterpedia/pkg/generated/clientset" \
    --clientset-name="versioned"

echo "Generating with lister-gen"
lister-gen \
    --go-header-file="hack/boilerplate.go.txt" \
    --input-dirs="github.com/clusterpedia-io/api/cluster/v1alpha2" \
    --output-package="github.com/clusterpedia-io/clusterpedia/pkg/generated/listers"

echo "Generating with informer-gen"
informer-gen \
    --go-header-file="hack/boilerplate.go.txt" \
    --input-dirs="github.com/clusterpedia-io/api/cluster/v1alpha2" \
    --output-package="github.com/clusterpedia-io/clusterpedia/pkg/generated/informers" \
    --versioned-clientset-package="github.com/clusterpedia-io/clusterpedia/pkg/generated/clientset/versioned" \
    --listers-package="github.com/clusterpedia-io/clusterpedia/pkg/generated/listers" \
    --output-package="github.com/clusterpedia-io/clusterpedia/pkg/generated/informers"

#echo "Generating with openapi-gen"
#openapi-gen \
#  --go-header-file hack/boilerplate.go.txt \
#  --input-dirs=github.com/clusterpedia-io/clusterpedia/pkg/apis/pedia/v1alpha1 \
#  --input-dirs "k8s.io/apimachinery/pkg/apis/meta/v1,k8s.io/apimachinery/pkg/runtime,k8s.io/apimachinery/pkg/version" \
#  --output-package=github.com/clusterpedia-io/clusterpedia/pkg/generated/openapi \
#  -O zz_generated.openapi
