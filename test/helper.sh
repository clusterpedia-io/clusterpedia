#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

ROOT="$(dirname "${BASH_SOURCE[0]}")/.."

# check whether command is installed.
function cmd_exist() {
    local command="${1}"
    type "${command}" >/dev/null 2>&1
}

# Check dependencies is installed or not and exit if not
function check_dependencies() {
    local dependencies=("${@}")
    local not_installed=()
    for dependency in "${dependencies[@]}"; do
        if ! cmd_exist "${dependency}"; then
            not_installed+=("${dependency}")
        fi
    done

    if [[ "${#not_installed[@]}" -ne 0 ]]; then
        echo "Error: Some dependencies are not installed:"
        for dependency in "${not_installed[@]}"; do
            echo "  - ${dependency}"
        done
        exit 1
    fi
}

# build the image for the test
function build_image() {
    make ON_PLUGINS=true VERSION=test REGISTRY=localtest images
}

# load the image into the kind cluster
function load_image() {
    local name="${1}"
    local image="${2}"
    if ! docker image inspect "${image}" >/dev/null 2>&1; then
        docker pull "${image}"
    fi
    kind load docker-image "${image}" --name "${name}"
}

# create a kind cluster and load necessary images
function create_cluster() {
    local name="${1:-kind}"
    local version="${2:-v1.23.4}"

    kind create cluster --name "${name}" --image "docker.io/kindest/node:${version}"
    load_image "${name}" localtest/clustersynchro-manager-amd64:test
    load_image "${name}" localtest/apiserver-amd64:test
    load_image "${name}" localtest/controller-manager-amd64:test
    load_image "${name}" postgres:12

# delete the kind cluster
function delete_cluster() {
    local name="${1:-kind}"
    kind delete cluster --name "${name}"
}

# install the Clusterpedia into the kind cluster
function install_clusterpedia() {
    kubectl kustomize "${ROOT}/test/kustomize" | kubectl apply -f -
    echo kubectl get all -n clusterpedia-system
    kubectl get all -n clusterpedia-system
}

# build pedia_cluster resources
function build_pedia_cluster() {
    local name="${1}"
    local kubeconfig="${2}"
    kubeconfig="$(echo "${kubeconfig}" | base64 | tr -d "\n")"
    cat <<EOF
apiVersion: cluster.clusterpedia.io/v1alpha2
kind: PediaCluster
metadata:
  name: ${name}
spec:
  kubeconfig: "${kubeconfig}"
  syncResources:
  - group: ""
    resources:
     - namespaces
     - pods
EOF
}

HOST_IP=""

# get the host IP for internal communication
function host_docker_internal() {
    if [[ "${HOST_IP}" == "" ]]; then
        # Need Docker 18.03
        HOST_IP=$(docker run --rm docker.io/library/alpine sh -c "nslookup host.docker.internal | grep 'Address' | grep -v '#' | grep -v ':53' | awk '{print \$2}' | head -n 1")

        if [[ "${HOST_IP}" == "" ]]; then
            # For Docker running on Linux used 172.17.0.1 which is the Docker-host in Docker’s default-network.
            HOST_IP="172.17.0.1"
        fi
    fi
    echo "${HOST_IP}"
}

TMPDIR="${TMPDIR:-/tmp/}"

# Install kwokctl tools
function install_kwokctl() {
    if cmd_exist kwokctl; then
        return 0
    fi
    wget "https://github.com/kubernetes-sigs/kwok/releases/download/v0.1.0/kwokctl-$(go env GOOS)-$(go env GOARCH)" -O "/usr/local/bin/kwokctl" &&
        chmod +x "/usr/local/bin/kwokctl"
}

# create a control plane cluster and install the Clusterpedia
function create_control_plane() {
    local name="${1}"
    local version="${2}"
    create_cluster "${name}" "${version}"
    install_clusterpedia
}

# delete the control plane cluster
function delete_control_plane() {
    local name="${1}"
    delete_cluster "${name}"
}

# create a worker fake cluster
function create_data_plane() {
    local name="${1}"
    local version="${2:-v1.19.16}"
    local kubeconfig
    local pedia_cluster
    local ip

    install_kwokctl

    KWOK_KUBE_VERSION="${version}" kwokctl create cluster --name "${name}" --quiet-pull
    ip="$(host_docker_internal)"
    kubeconfig="$(kwokctl get kubeconfig --name="${name}" | sed "s#/127.0.0.1:#/${ip}:#" || :)"
    if [[ "${kubeconfig}" == "" ]]; then
        echo "kubeconfig is empty"
        return 1
    fi
    kwokctl --name="${name}" kubectl apply -f - <<EOF
apiVersion: v1
kind: Node
metadata:
  annotations:
    kwok.x-k8s.io/node: fake
    node.alpha.kubernetes.io/ttl: "0"
  labels:
    beta.kubernetes.io/arch: amd64
    beta.kubernetes.io/os: linux
    kubernetes.io/arch: amd64
    kubernetes.io/hostname: fake-node
    kubernetes.io/os: linux
    kubernetes.io/role: agent
    node-role.kubernetes.io/agent: ""
    type: kwok-controller
  name: fake-node
EOF
    pedia_cluster="$(build_pedia_cluster "${name}" "${kubeconfig}")"
    echo "${pedia_cluster}" | kubectl apply -f -
}

# delete the worker fake cluster
function delete_data_plane() {
    local name="${1}"

    kubectl delete PediaCluster "${name}"
    kwokctl delete cluster --name "${name}"
}
