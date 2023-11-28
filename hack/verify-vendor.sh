#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..

DIFFROOT="${SCRIPT_ROOT}/vendor"
# The vendor contains soft links,
# which need to be in the same level as the vendor in order to avoid link failure
TMP_DIFFROOT="${SCRIPT_ROOT}/_tmp_vendor"
_tmp="${SCRIPT_ROOT}/_tmp"

cleanup() {
  rm -rf "${TMP_DIFFROOT}"
  rm -rf "${_tmp}"
}
trap "cleanup" EXIT SIGINT

cleanup

mkdir -p "${_tmp}"
mkdir -p "${TMP_DIFFROOT}"
cp -a "${DIFFROOT}"/* "${TMP_DIFFROOT}"
cp "${SCRIPT_ROOT}"/go.mod "$_tmp"/go.mod
cp "${SCRIPT_ROOT}"/go.sum "$_tmp"/go.sum

make -C "${SCRIPT_ROOT}" vendor
echo "diffing ${DIFFROOT} against freshly generated files"

govendor=0
diff -Nqaupr "${DIFFROOT}" "${TMP_DIFFROOT}" || govendor=$?
gomod=0
diff -Naupr "${SCRIPT_ROOT}"/go.mod "${_tmp}"/go.mod || gomod=$?
gosum=0
diff -Naupr "${SCRIPT_ROOT}"/go.sum "${_tmp}"/go.sum || gosum=$?

rm -rf "${DIFFROOT}"
mv "${TMP_DIFFROOT}" "${DIFFROOT}"
cp "${_tmp}"/go.mod "${SCRIPT_ROOT}"/go.mod
cp "${_tmp}"/go.sum "${SCRIPT_ROOT}"/go.sum
if [[ $govendor -eq 0 && $gomod -eq 0 && $gosum -eq 0 ]]
then
  echo "${DIFFROOT} up to date."
else
  echo "${DIFFROOT}, 'go.mod' or 'go.sum' is out of date. Please run 'make vendor'"
  exit 1
fi

reflector=0
diff vendor/k8s.io/client-go/tools/cache/reflector.go pkg/synchromanager/clustersynchro/informer/cache/.reflector.go || reflector=$?

if [[ $reflector -eq 0 ]]
then
  echo "'reflector.go' is up to date."
else
  echo "the file 'reflector.go' in vendor has been changed, please update the 'cache/.reflector.go' and 'reflector.go' in the pkg/synchromanager/clustersynchro/informer"
  exit 1
fi

pager=0
diff vendor/k8s.io/client-go/tools/pager/pager.go pkg/synchromanager/clustersynchro/informer/pager/.pager.go.copy || pager=$?

if [[ $pager -eq 0 ]]
then
  echo "'pager.go' is up to date."
else
  echo "the file 'pager.go' in vendor has been changed, please update the '.pager.go.copy' and 'pager.go' in the pkg/synchromanager/clustersynchro/informer/pager"
  exit 1
fi
