#!/usr/bin/env bash

TEST_ROOT="$(dirname "${BASH_SOURCE[0]}")"

source "$(dirname "${BASH_SOURCE[0]}")/helper.sh"

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

    for env in "${TEST_ROOT}"/environments/*.env.sh; do
        [[ -e "${env}" ]] || continue
        env_name="${env##*/}"
        env_name="${env_name%.env.sh}"
        for file in "${TEST_ROOT}"/cases/*.test.sh; do
            [[ -e "${file}" ]] || continue
            name="${file##*/}"
            name="${name%.test.sh}"
            echo "::group::Running test ${name} on ${env_name}"
            if ! "${env}" "${file}"; then
                failed+=("'${name} on ${env_name}'")
                mv "${TEST_ROOT}/logs" "${TEST_ROOT}/logs-${name}-${env_name}"
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
