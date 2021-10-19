Clusterpedia
---
## Usgae
### 多集群单资源查询

```bash
$ export KUBEAPISERVER="127.0.0.1:8001"
$ # 获取所有集群所有命名空间下的 deployments
$ curl $KUBEAPISERVER/apis/pedia.clusterpedia.io/v1alpha1/resources/apis/apps/v1/deployments

$ # 获取所有集群所有命名空间下的 deployments
$ kubectl get deployment -A
NAMESPACE       CLUSTER          NAME                         READY   UP-TO-DATE   AVAILABLE   AGE
dx-arch         dce-10-6-165-2   dx-arch-keycloak             1/1     1            1           66d
dx-arch         dce-10-6-165-2   dx-arch-ram                  1/1     1            1           66d
dx-arch         dce-10-6-165-2   dx-arch-stolon-proxy         2/2     2            2           66d
dx-arch         dce-10-6-165-2   dx-arch-stolon-sentinel      2/2     2            2           66d
dx-arch         dce-10-6-165-2   dx-arch-ui                   3/3     3            3           66d
hnc-system      dce-10-6-165-2   hnc-controller-manager       1/1     1            1           4d
kube-system     dce-10-6-165-2   calico-kube-controllers      1/1     1            1           66d
kube-system     dce-10-6-165-2   coredns-coredns              2/2     2            2           66d
kube-system     dce-10-6-165-2   dce-chart-manager            1/1     1            1           66d
kube-system     dce-10-6-165-2   dce-clair                    1/1     1            1           66d
kube-system     dce-10-6-165-2   dce-controller               1/2     2            1           66d
kube-system     dce-10-6-165-2   dce-core-keepalived          1/2     2            1           66d
kube-system     dce-10-6-165-2   dce-prometheus               1/2     2            1           66d
kube-system     dce-10-6-165-2   dce-registry                 1/1     1            1           66d
kube-system     dce-10-6-165-2   dce-uds-failover-assistant   1/2     2            1           66d
kube-system     dce-10-6-165-2   dce-uds-policy-controller    1/1     1            1           66d
kube-system     dce-10-6-165-2   dce-uds-storage-server       1/1     1            1           66d
kube-system     dce-10-6-165-2   metrics-server               1/1     1            1           66d

$ # 获取 kube-system 命名空间下的所有集群的 deployment
$ kubectl -n kube-system get deployment
NAMESPACE       CLUSTER          NAME                         READY   UP-TO-DATE   AVAILABLE   AGE
dce-acm         dce-10-6-165-2   dce-acm-apiserver            1/1     1            1           41d
dce-acm         dce-10-6-165-2   dce-acm-nginx                1/1     1            1           41d
dce-acm         dce-10-6-165-2   dce-acm-stolon-proxy         2/2     2            2           41d
dce-acm         dce-10-6-165-2   dce-acm-stolon-sentinel      2/2     2            2           41d
dce-acm         dce-10-6-165-2   dce-acm-synchromanager       1/1     1            1           41d
dce-acm         dce-10-6-165-2   dce-acm-ui                   1/1     1            1           41d
dce-acm-agent   dce-10-6-165-2   dce-acm-agent                1/1     1            1           41d
dce-system      dce-10-6-165-2   dce-system-dnsservice        1/1     1            1           66d
dce-system      dce-10-6-165-2   dce-system-uds               1/1     1            1           66d
dx-arch         dce-10-6-165-2   dx-arch-keycloak             1/1     1            1           66d
dx-arch         dce-10-6-165-2   dx-arch-ram                  1/1     1            1           66d
dx-arch         dce-10-6-165-2   dx-arch-stolon-proxy         2/2     2            2           66d
dx-arch         dce-10-6-165-2   dx-arch-stolon-sentinel      2/2     2            2           66d
dx-arch         dce-10-6-165-2   dx-arch-ui                   3/3     3            3           66d
hnc-system      dce-10-6-165-2   hnc-controller-manager       1/1     1            1           4d
kube-system     dce-10-6-165-2   calico-kube-controllers      1/1     1            1           66d
kube-system     dce-10-6-165-2   coredns-coredns              2/2     2            2           66d
kube-system     dce-10-6-165-2   dce-chart-manager            1/1     1            1           66d
kube-system     dce-10-6-165-2   dce-clair                    1/1     1            1           66d
kube-system     dce-10-6-165-2   dce-controller               1/2     2            1           66d
kube-system     dce-10-6-165-2   dce-core-keepalived          1/2     2            1           66d
kube-system     dce-10-6-165-2   dce-prometheus               1/2     2            1           66d
kube-system     dce-10-6-165-2   dce-registry                 1/1     1            1           66d
kube-system     dce-10-6-165-2   dce-uds-failover-assistant   1/2     2            1           66d
kube-system     dce-10-6-165-2   dce-uds-policy-controller    1/1     1            1           66d
kube-system     dce-10-6-165-2   dce-uds-storage-server       1/1     1            1           66d
kube-system     dce-10-6-165-2   metrics-server               1/1     1            1           66d

$ # 通过 label 来指定复杂查询的参数
$ kubectl get deployment -A -l "search.clusterpedia.io/clusters=test"
```

### 多集群 Collection Resource 查询搜索
```bash
$ curl $KUBEAPISERVER/apis/pedia.clusterpedia.io/v1alpha1/collectionresources/workloads?<query>

$ # 获取支持的集合资源
$ kubectl get collectionresources
NAME        KINDS
workloads   Deployment.apps,DaemonSet.apps,StatefulSet.apps

$ kubectl get collectionresources workloads
GROUP   VERSION   KIND         CLUSTER          NAMESPACE       NAME                         AGE
apps    v1        Deployment   dce-10-6-165-2   dce-acm         dce-acm-apiserver            41d
apps    v1        Deployment   dce-10-6-165-2   dce-acm         dce-acm-nginx                41d
apps    v1        Deployment   dce-10-6-165-2   dce-acm         dce-acm-stolon-proxy         41d
apps    v1        Deployment   dce-10-6-165-2   dce-acm         dce-acm-stolon-sentinel      41d
apps    v1        Deployment   dce-10-6-165-2   dce-acm         dce-acm-synchromanager       41d
apps    v1        Deployment   dce-10-6-165-2   dce-acm         dce-acm-ui                   41d
```

### 单集群单资源收集
```bash
$ curl $KUBEAPISERVER/apis/pedia.clusterpedia.io/v1alpha1/resources/apis/apps/v1/clusters/<clusters>/namespaces/<namespace>/deployments/<deployment name>
```
