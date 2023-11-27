#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

function extra_ldflags() {
    BUILD_DATE=$(date -u +'%Y-%m-%dT%H:%M:%SZ')

    # Git information, used to set clusterpedia version
    GIT_VERSION=$(git describe --tags --dirty)
    GIT_COMMIT_HASH=$(git rev-parse HEAD)

    GIT_TREESTATE="clean"
    GIT_DIFF=$(git diff --quiet >/dev/null 2>&1; if [ $? -eq 1 ]; then echo "1"; fi)
    if [ "${GIT_DIFF}" == "1" ]; then
        GIT_TREESTATE="dirty"
    fi

    KUBE_DEPENDENCE_VERSION=$(go list -m k8s.io/kubernetes | cut -d' ' -f2)

    local ldflags
    ldflags+=" -X github.com/clusterpedia-io/clusterpedia/pkg/version.gitVersion=${GIT_VERSION}"
    ldflags+=" -X github.com/clusterpedia-io/clusterpedia/pkg/version.gitCommit=${GIT_COMMIT_HASH}"
    ldflags+=" -X github.com/clusterpedia-io/clusterpedia/pkg/version.gitTreeState=${GIT_TREESTATE}"
    ldflags+=" -X github.com/clusterpedia-io/clusterpedia/pkg/version.buildDate=${BUILD_DATE}"

    # The `client-go/pkg/version` effects the **User-Agent** when using client-go to request.
    # User-Agent="<bin name>/$(GIT_VERSION) ($(GOOS)/$(GOARCH)) kubernetes/$(GIT_COMMIT_HASH)[/<component name>]"
    #   eg. "clustersynchro-manager/0.4.0 (linux/amd64) kubernetes/fbf0f4f/clustersynchro-manager"
    ldflags+=" -X k8s.io/client-go/pkg/version.gitVersion=${GIT_VERSION}"
    ldflags+=" -X k8s.io/client-go/pkg/version.gitMajor=$(echo ${GIT_VERSION#v} | cut -d. -f1)"
    ldflags+=" -X k8s.io/client-go/pkg/version.gitMinor=$(echo ${GIT_VERSION#v} | cut -d. -f2)"
    ldflags+=" -X k8s.io/client-go/pkg/version.gitCommit=${GIT_COMMIT_HASH}"
    ldflags+=" -X k8s.io/client-go/pkg/version.gitTreeState=${GIT_TREESTATE}"
    ldflags+=" -X k8s.io/client-go/pkg/version.buildDate=${BUILD_DATE}"


    # The `component-base/version` effects the version obtained using Kubernetes OpenAPI.
    #   OpenAPI Path: /apis/clusterpedia.io/v1beta1/resources/version
    #   $ kubectl version

    ldflags+=" -X k8s.io/component-base/version.gitVersion=${KUBE_DEPENDENCE_VERSION}"
    ldflags+=" -X k8s.io/component-base/version.gitMajor=$(echo ${KUBE_DEPENDENCE_VERSION#v} | cut -d. -f1)"
    ldflags+=" -X k8s.io/component-base/version.gitMinor=$(echo ${KUBE_DEPENDENCE_VERSION#v} | cut -d. -f2)"
    ldflags+=" -X k8s.io/component-base/version.gitCommit=${GIT_COMMIT_HASH}"
    ldflags+=" -X k8s.io/component-base/version.gitTreeState=${GIT_TREESTATE}"
    ldflags+=" -X k8s.io/component-base/version.buildDate=${BUILD_DATE}"

    echo $ldflags
}
