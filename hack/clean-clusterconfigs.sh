#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

current_context=`kubectl config current-context`

cmd="kubectl config view -o jsonpath='{.contexts[?(@.name == \"$current_context\")].context.cluster}'"
current_cluster=`eval $cmd`

cmd="kubectl config view -o jsonpath='{.clusters[?(@.name == \"$current_cluster\")].cluster.server}' --raw"
server=`eval $cmd`
if [[ -z $server ]] ; then
    echo "current cluster server is ''"
    exit 1
fi

server=${server%/}
pediaserver=$server/apis/clusterpedia.io

for cluster in $(kubectl config get-clusters)
do
    if [[ $cluster == "NAME" ]];then
        continue
    fi

    cmd="kubectl config view -o jsonpath='{.clusters[?(@.name == \"$cluster\")].cluster.server}' --raw"
    clusterserver=`eval $cmd`

    if [[ $clusterserver == $pediaserver* ]]; then
        kubectl config delete-cluster $cluster
    fi
done
