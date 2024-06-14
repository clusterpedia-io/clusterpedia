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
GO111MODULE=on go install k8s.io/kube-openapi/cmd/openapi-gen

GOPATH=$(go env GOPATH | awk -F ':' '{print $1}')
export PATH=$PATH:$GOPATH/bin

echo "change directory: ${API_ROOT}"
cd "${API_ROOT}"

#echo "Generating with register-gen"
#register-gen \
#    --go-header-file="${REPO_ROOT}/hack/boilerplate.go.txt" \
#    --input-dirs="./cluster/v1alpha1"

echo "Generating with deepcopy-gen"
deepcopy-gen \
    --go-header-file="${REPO_ROOT}/hack/boilerplate.go.txt" \
    --bounding-dirs="cluster/v1alpha2" \
    --output-file="zz_generated.deepcopy.go"
deepcopy-gen \
    --go-header-file="${REPO_ROOT}/hack/boilerplate.go.txt" \
    --bounding-dirs="policy/v1alpha1" \
    --output-file="zz_generated.deepcopy.go"
deepcopy-gen \
    --go-header-file="${REPO_ROOT}/hack/boilerplate.go.txt" \
    --bounding-dirs="clusterpedia/v1beta1" \
    --output-file="zz_generated.deepcopy.go"
deepcopy-gen \
    --go-header-file="${REPO_ROOT}/hack/boilerplate.go.txt" \
    --bounding-dirs="clusterpedia" \
    --output-file="zz_generated.deepcopy.go"

echo "Generating with conversion-gen"
conversion-gen \
    --go-header-file="${REPO_ROOT}/hack/boilerplate.go.txt" \
    --base-peer-dirs="./clusterpedia/v1beta1" \
    --output-file="zz_generated.conversion.go"

echo "change directory: ${REPO_ROOT}"
cd "${REPO_ROOT}"

go_path="${REPO_ROOT}/_go"
cleanup() {
  rm -rf "${go_path}"
}
trap "cleanup" EXIT SIGINT
cleanup

go_pkg="${go_path}/src/github.com/clusterpedia-io/clusterpedia"
go_pkg_dir=$(dirname "${go_pkg}")
mkdir -p "${go_pkg_dir}"

if [[ ! -e "${go_pkg_dir}" || "$(readlink "${go_pkg_dir}")" != "${REPO_ROOT}" ]]; then
  ln -snf "${REPO_ROOT}" "${go_pkg_dir}"
fi
export GOPATH="${go_path}"

echo "Generating with client-gen"
client-gen \
    --go-header-file="hack/boilerplate.go.txt" \
    --input-base="github.com/clusterpedia-io/api" \
    --input="cluster/v1alpha2,policy/v1alpha1" \
    --output-dir="pkg/generated/clientset" \
    --output-pkg="github.com/clusterpedia-io/clusterpedia/pkg/generated/clientset" \
    --clientset-name="versioned" \
    --plural-exceptions="ClusterSyncResources:ClusterSyncResources"

echo "Generating with lister-gen"
lister-gen \
    --go-header-file="hack/boilerplate.go.txt" \
    --output-dir="pkg/generated/listers" \
    --output-pkg="github.com/clusterpedia-io/clusterpedia/pkg/generated/listers" \
    --plural-exceptions="ClusterSyncResources:ClusterSyncResources" \
    github.com/clusterpedia-io/api/cluster/v1alpha2 github.com/clusterpedia-io/api/policy/v1alpha1


echo "Generating with informer-gen"
informer-gen \
    --go-header-file="hack/boilerplate.go.txt" \
    --versioned-clientset-package="github.com/clusterpedia-io/clusterpedia/pkg/generated/clientset/versioned" \
    --listers-package="github.com/clusterpedia-io/clusterpedia/pkg/generated/listers" \
    --output-dir="pkg/generated/informers" \
    --output-pkg="github.com/clusterpedia-io/clusterpedia/pkg/generated/informers" \
    --plural-exceptions="ClusterSyncResources:ClusterSyncResources" \
    github.com/clusterpedia-io/api/cluster/v1alpha2 github.com/clusterpedia-io/api/policy/v1alpha1

echo "Generating with openapi-gen"
openapi-gen \
    --go-header-file="hack/boilerplate.go.txt" \
    --output-dir="pkg/generated/openapi" \
    --output-pkg="github.com/clusterpedia-io/clusterpedia/pkg/generated/openapi" \
    --output-file="zz_generated.openapi.go" \
    github.com/clusterpedia-io/api/cluster/v1alpha2 github.com/clusterpedia-io/api/policy/v1alpha1 github.com/clusterpedia-io/api/clusterpedia/v1beta1 \
    k8s.io/apimachinery/pkg/apis/meta/v1 k8s.io/apimachinery/pkg/runtime k8s.io/apimachinery/pkg/version
