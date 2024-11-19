#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Create an environment that manages a single cluster

cases="${1}"
version="${2}"
control_plane_name="control-${version//./-}"
data_plane_name="data-${version//./-}"

source "$(dirname "${BASH_SOURCE[0]}")/../helper.sh"

function cleanup() {
    "${ROOT}/hack/clean-clusterconfigs.sh" >/dev/null 2>&1
    delete_data_plane ${data_plane_name} >/dev/null 2>&1
    delete_control_plane ${control_plane_name} >/dev/null 2>&1
}
trap cleanup EXIT

create_control_plane ${control_plane_name} ${version} || {
    echo "Failed to create control plane"
    exit 1
}
create_data_plane ${data_plane_name} ${version} || {
    echo "Failed to create data plane"
    exit 1
}

"${ROOT}/hack/gen-clusterconfigs.sh"

if ! check_clusterpedia_apiserver; then
    echo "clusterpedia apiserver is not ready"
    exit 1
fi

"${cases}"
