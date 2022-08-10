#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

REPO_ROOT=$(git rev-parse --show-toplevel)
cd "${REPO_ROOT}"

MIN_GO_VERSION=go1.17.0

# Ensure the go tool exists and is a viable version.
function verify_go_version {
  local GO_VERSION
  IFS=" " read -ra GO_VERSION <<< "$(GOFLAGS='' go version)"
      if [[ "${MIN_GO_VERSION}" != $(echo -e "${MIN_GO_VERSION}\n${GO_VERSION[2]}" | sort -s -t. -k 1,1 -k 2,2n -k 3,3n | head -n1) && "${GO_VERSION[2]}" != "devel" ]]; then
        echo "Detected go version: ${GO_VERSION[*]}."
        echo "Clusterpedia requires ${MIN_GO_VERSION} or greater."
        echo "Please install ${MIN_GO_VERSION} or later."
        exit 1
      fi
}
verify_go_version

echo "running 'go mod tidy'"
go mod tidy

echo "running 'go mod vendor'"
go mod vendor

echo "create symbolic links under vendor to the staging repo"
rm -rf ./vendor/github.com/clusterpedia-io/api
ln -s ../../../staging/src/github.com/clusterpedia-io/api/ ./vendor/github.com/clusterpedia-io/api
