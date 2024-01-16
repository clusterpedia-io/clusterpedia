#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

function usage() {
    cat <<EOF

Build components: 
    $0 <apiserver|binding-apiserver|clustersynchro-manager|controller-manager>

Build plugins:
    $0 plugins <plugin_name>

ENV:
    CLUSTERPEDIA_REPO: clusterpedia repo path, required if the pwd is not in the clusterpedia repository.
            eg. CLUSTERPEDIA_REPO=/clusterpedia

    PLUGIN_REPO: plugin repo path, required if the pwd is not the plugin repository.
            eg. CLUSTERPEDIA_REPO=/plugins/sample-storage

    OUTPUT_DIR: default is the root of the repository or pwd,
                the component binary will be output in \$OUTPUT_DIR/bin, the plugins will be output in \$OUTPUT_DIR/plugins.
            eg. OUTPUT_DIR=.
EOF
}

set +e; REPO_ROOT=$(git rev-parse --show-toplevel 2>/dev/null);set -e
if [ -z $REPO_ROOT ]; then
    if [ -z $CLUSTERPEDIA_REPO ]; then
        echo "the current directory is not in the clusterpedia repo, please set CLUSTERPEDIA_REPO=<clusterpedia repo path>"
        usage
        exit 1
    fi
    REPO_ROOT=$(pwd)
else
    CLUSTERPEDIA_REPO=${CLUSTERPEDIA_REPO:-$REPO_ROOT}
fi
OUTPUT_DIR=${OUTPUT_DIR:-$REPO_ROOT}

CLUSTERPEDIA_PACKAGE="github.com/clusterpedia-io/clusterpedia"
if [ $CLUSTERPEDIA_PACKAGE != "$(sed -n '1p' ${CLUSTERPEDIA_REPO}/go.mod | awk '{print $2}')" ]; then
    echo "CLUSTERPEDIA_REPO is invalid"
    usage
    exit 1
fi


TMP_GOPATH=/tmp/clusterpedia
function cleanup() {
    rm -rf $TMP_GOPATH
}

TMP_CLUSTERPEDIA=$TMP_GOPATH/src/$CLUSTERPEDIA_PACKAGE
function copy_clusterpedia_repo() {
    mkdir -p $TMP_CLUSTERPEDIA && cd $TMP_CLUSTERPEDIA
    cp -rf $CLUSTERPEDIA_REPO/* .

    for file in $(ls staging/src/github.com/clusterpedia-io); do
        rm -rf vendor/github.com/clusterpedia-io/$file
    done

    cp -rf staging/src/github.com/clusterpedia-io/* $TMP_GOPATH/src/github.com/clusterpedia-io
    cp -rf vendor/* $TMP_GOPATH/src
    rm -rf go.mod go.sum vendor

    rm $TMP_GOPATH/src/modules.txt
}

GOOS=${GOOS:-$(go env GOOS)}
GOARCH=${GOARCH:-$(go env GOARCH)}
HOSTARCH=$(go env GOHOSTARCH)

CC_FOR_TARGET=${BUILD_CC_FOR_TARGET:-""}
CC=${BUILD_CC:-""}
if [[ "${GOOS}" == "linux" && "${GOARCH}" != "${HOSTARCH}" ]]; then
    case ${GOARCH} in
        amd64)
            CC_FOR_TARGET=${CC_FOR_TARGET:-"gcc-x86-64-linux-gnu"}
            CC=${CC:-"x86_64-linux-gnu-gcc"}
        ;;
        arm64)
            CC_FOR_TARGET=${CC_FOR_TARGET:-"gcc-aarch64-linux-gnu"}
            CC=${CC:-"aarch64-linux-gnu-gcc"}
        ;;
    esac
fi

function build_component() {
    local LDFLAGS=${BUILD_LDFLAGS:-""}
    if [ -f ${CLUSTERPEDIA_REPO}/ldflags.sh ]; then
        cd ${CLUSTERPEDIA_REPO} && source ./ldflags.sh
        LDFLAGS+=" $(extra_ldflags)"
    fi

    if [[ "${GOOS}" == "linux" ]]; then
        # https://github.com/mattn/go-sqlite3#linux
        LDFLAGS+=" -extldflags -static"
    fi

    set -x
    cd $TMP_CLUSTERPEDIA
    GOPATH=$TMP_GOPATH GO111MODULE=off CGO_ENABLED=1 CC_FOR_TARGET=$CC_FOR_TARGET CC=$CC \
        go build -tags "json1 $GOOS" -ldflags "${LDFLAGS}" -o $OUTPUT_DIR/bin/$1 ./cmd/$1
    set +x
}

cleanup
trap cleanup EXIT

mkdir -p $TMP_GOPATH

case ${1:-""} in
    plugins)
        if [ -z ${2:-""} ]; then
            echo "please set plugin name"
            usage
            exit 1
        fi
        ;;
    "")
        usage
        exit
        ;;
    *)
        copy_clusterpedia_repo
        build_component $1
        exit
        ;;
esac

PLUGIN_REPO=${PLUGIN_REPO:-$REPO_ROOT}
if [ $CLUSTERPEDIA_REPO == $PLUGIN_REPO ]; then
    echo "please set CLUSTERPEDIA_REPO or PLUGIN_REPO"
    usage
    exit 1
fi

PLUGIN_PACKAGE="$(sed -n '1p' go.mod | awk '{print $2}')"
TMP_PLUGIN=$TMP_GOPATH/src/$PLUGIN_PACKAGE
function copy_plugin_repo() {
    mkdir -p $TMP_PLUGIN && cd $TMP_PLUGIN
    cp -r $PLUGIN_REPO/* .

    [ -f go.mod ] && [ ! -d ./vendor ] && GO111MODULE=on go mod vendor

    rm -rf vendor/$CLUSTERPEDIA_PACKAGE
    for file in $CLUSTERPEDIA_REPO/staging/src/github.com/clusterpedia-io/*; do
        if [ -d file ]; then
            rm -rf vendor/github.com/clusterpedia-io/$file
        fi
    done

    cp -rf vendor/* $TMP_GOPATH/src
    rm -rf go.mod go.sum vendor

    rm $TMP_GOPATH/src/modules.txt
}

function build_plugin() {
    local LDFLAGS=${BUILD_LDFLAGS:-""}
    if [ -f ${PLUGIN_REPO}/ldflags.sh ]; then
        cd ${PLUGIN_REPO} && source ./ldflags.sh
    fi

    set -x
    cd $TMP_PLUGIN
    GOPATH=$TMP_GOPATH GO111MODULE=off CGO_ENABLED=1 CC_FOR_TARGET=$CC_FOR_TARGET CC=$CC \
        go build -ldflags "${LDFLAGS}" -buildmode=plugin -o $OUTPUT_DIR/plugins/$1
    set +x
}

copy_plugin_repo

copy_clusterpedia_repo

build_plugin $2
