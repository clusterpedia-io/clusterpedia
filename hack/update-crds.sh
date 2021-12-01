#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

echo "Generating CRDs With controller-gen"
GO11MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen
controller-gen crd paths=./pkg/apis/clusters/v1alpha1 output:crd:dir=./deploy/crds
