#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..

API_DIFFROOT="${SCRIPT_ROOT}/staging/src/github.com/clusterpedia-io/api"
GENERATED_DIFFROOT="${SCRIPT_ROOT}/pkg/generated"

_tmp="${SCRIPT_ROOT}/_tmp"
TMP_API_DIFFROOT="${_tmp}/api"
TMP_GENERATED_DIFFROOT="${_tmp}/generated"

cleanup() {
  rm -rf "${_tmp}"
}
trap "cleanup" EXIT SIGINT

cleanup

mkdir -p "${_tmp}"
cp -a "${API_DIFFROOT}" "${TMP_API_DIFFROOT}"
cp -a "${GENERATED_DIFFROOT}" "${TMP_GENERATED_DIFFROOT}"

"${SCRIPT_ROOT}/hack/update-codegen.sh"

diff_code() {
    local diff=$1
    local tmp=$2

    echo "diffing ${diff} against freshly generated codegen"
    ret=0
    diff -Naupr "${diff}" "${tmp}" || ret=$?
    cp -a "${tmp}"/* "${diff}"
    if [[ $ret -eq 0 ]]; then
      echo "${diff} up to date."
    else
      echo "${diff} is out of date. Please run 'make codegen'"
      exit 1
    fi
}

diff_code ${API_DIFFROOT} ${TMP_API_DIFFROOT}
diff_code ${GENERATED_DIFFROOT} ${TMP_GENERATED_DIFFROOT}
