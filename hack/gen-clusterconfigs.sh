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

cmd="kubectl config view -o jsonpath='{.clusters[?(@.name == \"$current_cluster\")].cluster.tls-server-name}' --raw"
tls_server_name=`eval $cmd`

cmd="kubectl config view -o jsonpath='{.clusters[?(@.name == \"$current_cluster\")].cluster.insecure-skip-tls-verify}' --raw"
insecure_skip_tls_verify=`eval $cmd`

cmd="kubectl config view -o jsonpath='{.clusters[?(@.name == \"$current_cluster\")].cluster.certificate-authority}' --raw"
certificate_authority=`eval $cmd`

cmd="kubectl config view -o jsonpath='{.clusters[?(@.name == \"$current_cluster\")].cluster.certificate-authority-data}' --raw"
certificate_authority_data=`eval $cmd`

echo "Current Context: $current_context"
echo "Current Cluster: $current_cluster"
echo -e "\tServer: $server"
echo -e "\tTLS Server Name: $tls_server_name"
echo -e "\tInsecure Skip TLS Verify: $insecure_skip_tls_verify"
echo -e "\tCertificate Authority: $certificate_authority"
if ! [[ -z $certificate_authority_data ]]; then
    echo -e "\tCertificate Authority Data: ***"
else
    echo -e "\tCertificate Authority Data:"
fi
echo

server=${server%/}
pediaserver=$server/apis/clusterpedia.io

set_cluster(){
    local cluster=$1
    local server=$2
    kubectl config set-cluster $cluster --server $server

    if ! [[ -z $tls_server_name ]]; then
        kubectl config set clusters.$cluster.tls-server-name $tls_server_name 1>/dev/null
    fi

    if ! [[ -z $insecure_skip_tls_verify ]]; then
        kubectl config set clusters.$cluster.insecure-skip-tls-verify $insecure_skip_tls_verify 1>/dev/null
    fi

    if ! [[ -z $certificate_authority ]] ; then
        kubectl config set clusters.$cluster.certificate-authority $certificate_authority  1>/dev/null
    fi

    if ! [[ -z $certificate_authority_data ]]; then
        kubectl config set clusters.$cluster.certificate-authority-data $certificate_authority_data 1>/dev/null
    fi
}

set_cluster "clusterpedia" $pediaserver/v1beta1/resources

for cluster in $(kubectl get pediaclusters -o name)
do
    cluster=${cluster#*/}
    cmd="kubectl config view -o jsonpath='{.clusters[?(@.name == \"$cluster\")].cluster.server}' --raw"
    clusterserver=`eval $cmd`

    if ! [[ -z $clusterserver ]] && [[ $clusterserver != $pediaserver* ]]; then
        echo "$cluster has existed, cluster server is $clusterserver, not pedia cluster"
        continue
    fi

    clusterserver=$pediaserver/v1beta1/resources/clusters/$cluster
    set_cluster $cluster $clusterserver
done
