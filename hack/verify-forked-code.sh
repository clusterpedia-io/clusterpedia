#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

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
