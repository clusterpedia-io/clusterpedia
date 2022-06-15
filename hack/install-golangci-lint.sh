#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# do proxy setting in China mainland
if [[ -n ${CHINA_MAINLAND:-} ]]; then
  export GOPROXY=https://proxy.golang.com.cn,direct # set domestic go proxy
fi
echo "install golangci-lint"
GO111MODULE=on go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.46.2