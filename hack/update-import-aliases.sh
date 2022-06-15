#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
cd "${SCRIPT_ROOT}"
ROOT_PATH=$(pwd)

IMPORT_ALIASES_PATH="${ROOT_PATH}/hack/.import-aliases"
INCLUDE_PATH="(${ROOT_PATH}/cmd|\
${ROOT_PATH}/pkg/apiserver|${ROOT_PATH}/pkg/kubeapiserver|\
${ROOT_PATH}/pkg/storage|${ROOT_PATH}/pkg/synchromanager|\
${ROOT_PATH}/pkg/utils|${ROOT_PATH}/pkg/version)"

ret=0
go run "k8s.io/kubernetes/cmd/preferredimports" -confirm -import-aliases "${IMPORT_ALIASES_PATH}" -include-path "${INCLUDE_PATH}"  "${ROOT_PATH}" || ret=$?
if [[ $ret -ne 0 ]]; then
  echo "!!! Unable to fix imports programmatically. Please see errors above." >&2
  exit 1
fi
