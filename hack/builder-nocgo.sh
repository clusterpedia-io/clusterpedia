#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

REPO_ROOT=$(git rev-parse --show-toplevel)

OUTPUT_DIR=${OUTPUT_DIR:-$REPO_ROOT}
CLUSTERPEDIA_REPO=${CLUSTERPEDIA_REPO:-$REPO_ROOT}

GOOS=${GOOS:-$(go env GOOS)}
GOARCH=${GOARCH:-$(go env GOARCH)}

function build_component() {
    cd $CLUSTERPEDIA_REPO

    LDFLAGS=${BUILD_LDFLAGS:-""}
    if [ -f ./ldflags.sh ]; then
        source ./ldflags.sh
        LDFLAGS+=" $(extra_ldflags)"
    fi

    set -x
	CGO_ENABLED=0 go build -ldflags "${LDFLAGS}" -o ${OUTPUT_DIR}/bin/$1 ./cmd/$1
    set +x
}

build_component $1
