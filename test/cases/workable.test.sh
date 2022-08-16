#!/usr/bin/env bash

# Waiting for Clusterpedia to be ready
function ready() {
    local i
    for ((i = 0; i < 100; i++)); do
        sleep 5
        got="$(kubectl get pediacluster)"
        unexpect="$(echo "${got}" | tail -n +2 | awk '{print $1,$2}' | grep -v True)"
        if [[ "${unexpect}" == "" ]]; then
            return 0
        fi
        echo "got"
        echo "${got}"
        echo "unexpect"
        echo "${unexpect}"
    done
    return 1
}

# Check Clusterpedia is works
function check() {
    local clusters
    local query
    local original
    local unsync
    clusters="$(kubectl get pediacluster -o jsonpath="{.items[*].metadata.name}")"
    for cluster in ${clusters}; do
        kubectl --context="fake-k8s-${cluster}" apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: fake-pod
  namespace: default
spec:
  replicas: 10
  selector:
    matchLabels:
      app: fake-pod
  template:
    metadata:
      labels:
        app: fake-pod
    spec:
      containers:
        - name: fake-container
          image: fake
EOF
    done

    sleep 10

    unsync=()
    for cluster in ${clusters}; do
        echo kubectl --cluster "${cluster}" get pod -o wide
        query=$(kubectl --cluster "${cluster}" get pod -o wide)
        echo "${query}"
        echo kubectl --context="fake-k8s-${cluster}" get pod -o wide
        original=$(kubectl --context="fake-k8s-${cluster}" get pod -o wide)
        echo "${original}"

        diff <(echo "${original}" | awk '{print $1, $2, $3, $4, $6, $7}' | sort) <(echo "${query}" | awk '{print $2, $3, $4, $5, $7, $8}' | sort) || unsync+=("${cluster}")
    done

    if [[ "${#unsync[@]}" -gt 0 ]]; then
        echo "Unsynchronized: ${unsync[*]}"
        return 1
    fi
}

function main() {
    ready || exit 1
    check || exit 1
}

main
