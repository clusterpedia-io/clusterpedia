#!/usr/bin/env bash

TEST_ROOT="$(dirname "${BASH_SOURCE[0]}")"

source "$(dirname "${BASH_SOURCE[0]}")/helper.sh"

single_versions=(
    v1.30.0
    v1.23.17

#   Since ubuntu in github action uses cgroup v2 by default
#   and can't install clusters below 1.19, skip them for now!
#   v1.18.20
#   v1.14.10
)

multiple_versions=(
    v1.30.0

#   Since ubuntu in github action uses cgroup v2 by default
#   and can't install clusters below 1.19, skip them for now!
#   v1.14.10
)

function main() {
    local name
    local env_name
    local failed=()

    echo "::group::check dependencies"
    check_dependencies docker kind kubectl 
    echo "::endgroup::"

    echo "::group::Build image"
    build_image
    echo "::endgroup::"

    for version in "${single_versions[@]}"; do
        for file in "${TEST_ROOT}"/cases/*.test.sh; do
            [[ -e "${file}" ]] || continue
            name="${file##*/}"
            name="${name%.test.sh}"

            echo "::group::Running [single cluster] test ${name} on ${version}"
            if ! "${TEST_ROOT}/environments/single.env.sh" "${file}" "${version}"; then
                failed+=("'${name} on ${version}'")
                mv "${TEST_ROOT}/logs" "${TEST_ROOT}/logs-${name}-single-cluster-${version}"
            else
                # Clean up logs
                rm -rf "${TEST_ROOT}/logs"
            fi
            echo "::endgroup::"
        done
    done

    for version in "${multi_versions[@]}"; do
        for file in "${TEST_ROOT}"/cases/*.test.sh; do
            [[ -e "${file}" ]] || continue
            name="${file##*/}"
            name="${name%.test.sh}"

            echo "::group::Running [multiple clusters] test ${name} on ${version}"
            if ! "${TEST_ROOT}/environments/multiple.env.sh" "${file}" "${version}"; then
                failed+=("'${name} on ${version}'")
                mv "${TEST_ROOT}/logs" "${TEST_ROOT}/logs-${name}-multiple-clusters-${version}"
            else
                # Clean up logs
                rm -rf "${TEST_ROOT}/logs"
            fi
            echo "::endgroup::"
        done
    done

    if [[ "${#failed[@]}" -gt 0 ]]; then
        echo "Failed tests: ${failed[*]}"
        exit 1
    fi
}

main
