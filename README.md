Clusterpedia
---
## Usage
### 多集群单资源查询

```bash
$ export KUBEAPISERVER="127.0.0.1:8001"
$ # 获取所有集群所有命名空间下的 deployments
$ curl $KUBEAPISERVER/apis/pedia.clusterpedia.io/v1alpha1/resources/apis/apps/v1/deployments

$ # 获取所有集群所有命名空间下的 deployments
$ kubectl get deployment -A
NAMESPACE       CLUSTER          NAME                         READY   UP-TO-DATE   AVAILABLE   AGE
default         cluster-1        dao-2048                     1/1     1            1           23d
default         cluster-1        nginx                        1/1     1            1           23d
kube-system     cluster-1        calico-kube-controllers      1/1     1            1           34d
kube-system     cluster-1        calico-node                  1/1     1            1           34d
default         cluster-2        dao-2048                     1/1     1            1           23d
default         cluster-2        nginx                        1/1     1            1           23d
kube-system     cluster-2        calico-kube-controllers      1/1     1            1           66d
kube-system     cluster-2        coredns-coredns              2/2     2            2           66d
kube-system     cluster-2        calico-node                  1/1     1            1           66d
kube-system     cluster-2        metrics-server               1/1     1            1           66d

$ # 获取 kube-system 命名空间下的所有集群的 deployment
$ kubectl -n kube-system get deployment
NAMESPACE       CLUSTER          NAME                         READY   UP-TO-DATE   AVAILABLE   AGE
kube-system     cluster-1        calico-kube-controllers      1/1     1            1           34d
kube-system     cluster-1        calico-node                  1/1     1            1           34d
kube-system     cluster-2        calico-kube-controllers      1/1     1            1           66d
kube-system     cluster-2        coredns-coredns              2/2     2            2           66d
kube-system     cluster-2        calico-node                  1/1     1            1           66d
kube-system     cluster-2        metrics-server               1/1     1            1           66d
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
apps    v1        Deployment   cluster-1        default         wordpress                    41d
apps    v1        Deployment   cluster-1        default         nginx                        41d
apps    v1        Deployment   cluster-1        default         stolon-proxy                 41d
apps    v1        Deployment   cluster-1        default         stolon-sentinel              41d
apps    v1        Deployment   cluster-1        default         default-ui                   41d
```

### 单集群单资源收集
```bash
$ curl $KUBEAPISERVER/apis/pedia.clusterpedia.io/v1alpha1/resources/apis/apps/v1/clusters/<clusters>/namespaces/<namespace>/deployments/<deployment name>
```
