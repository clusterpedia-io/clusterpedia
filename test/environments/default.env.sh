#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Create an environment that manages a single cluster

cases="${1}"

source "$(dirname "${BASH_SOURCE[0]}")/../helper.sh"

function cleanup() {
    "${ROOT}/hack/clean-clusterconfigs.sh" >/dev/null 2>&1
    delete_data_plane data-v1-28 >/dev/null 2>&1
    delete_control_plane control-v1-28 >/dev/null 2>&1
}
trap cleanup EXIT

create_control_plane control-v1-28 v1.28.0 || {
    echo "Failed to create control plane"
    exit 1
}
create_data_plane data-v1-28 v1.28.2 || {
    echo "Failed to create data plane"
    exit 1
}

"${ROOT}/hack/gen-clusterconfigs.sh"

"${cases}"
