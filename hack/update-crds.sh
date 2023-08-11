#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

REPO_ROOT=$(git rev-parse --show-toplevel)
API_ROOT="${REPO_ROOT}/staging/src/github.com/clusterpedia-io/api"

echo "Generating CRDs With controller-gen"
GO11MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen

GOPATH=$(go env GOPATH | awk -F ':' '{print $1}')
export PATH=$PATH:$GOPATH/bin

cd "${API_ROOT}"
controller-gen crd paths=./cluster/... output:crd:dir="${REPO_ROOT}/kustomize/crds"
controller-gen crd paths=./policy/... output:crd:dir="${REPO_ROOT}/kustomize/crds"
