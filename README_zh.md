<span id="top"></span>
# Clusterpedia
Clusterpedia 这个名称借鉴自 Wikipedia，是多集群的百科全书，其核心理念是收集、检索和简单控制多集群资源。

通过聚合收集多集群资源，在兼容 Kubernetes OpenAPI 的基础上额外提供更加强大的检索功能，让用户更方便快捷地在多集群中获取想要的任何资源。
> Clusterpedia 的能力并不仅仅是检索查看，未来还会支持对资源的简单控制，就像 wiki 同样支持编辑词条一样。  

本文讲述了 Clusterpedia 的以下内容：
- [架构设计](#design)
- [特性和功能](#functions)
- [部署](#deployment)
- [收集资源](#get)
- [资源检索](#view)
- [Roadmap](#roadmap)
# 架构设计<span id="design"></span>
Clusterpedia 的架构设计图如下所示：
<div align="center"><img src="./docs/images/arch.png" style="width:900px;" /></div>
从架构上分为四个部分：  

* **Clusterpedia APIServer**：以 Aggregated API 的方式注册到 Kube APIServer，通过统一的入口来提供服务
* **ClusterSynchro Manager**：管理用于同步集群资源的 Cluster Synchro
* **Storage Layer**：这是存储层，用来连接具体的存储组件，然后通过存储层接口注册到 Clusterpedia APIServer 和 ClusterSynchro Manager
* **存储组件**：这是具体的存储设施，例如 MySQL、postgres、redis 或其他图数据库

另外，Clusterpedia 会使用 *PediaCluster* 这个自定义资源来实现集群认证和资源收集配置。

Clusterpedia 还提供了可以接入 MySQL 和 postgres 的默认存储层。
> Clusterpedia 并不关心用户所使用的具体存储设置是什么，
> 用户可以根据自己的需求选择或者实现存储层，然后将存储层以插件的形式注册到 Clusterpedia 中


[回到页首](#top)
# 特性和功能<span id="functions"></span>
- [x] 支持复杂的检索条件、过滤条件、排序、分页等等
- [ ] 支持查询资源时请求附带关系资源
- [x] 统一主集群和多集群资源检索入口
- [x] 兼容 kubernetes OpenAPI，可以直接使用 kubectl 进行多集群检索，而无需第三方插件或者工具
- [x] 兼容收集不同版本的集群资源，不受主集群版本约束
- [x] 资源收集高性能，低内存
- [x] 根据集群当前的健康状态，自动开始/停止资源收集
- [ ] 插件化存储层，用户可以根据自己需求使用其他存储组件自定义存储层
- [x] 高可用
> 上述未实现的功能已经在 Roadmap 中


[回到页首](#top)
# 部署<span id="deployment"></span>
Clusterpedia 当前还处于非常早期的阶段，在部署流程上还不够完善。

所以部署时还需要对 yaml 进行一点点手动修改，克隆到本地后的文件夹结构如下：
```sh
$ git clone https://github.com/clusterpedia-io/clusterpedia.git
$ cd clusterpedia
$ ll
total 288
-rw-r--r--   1 icebergu  staff    91B 12  2 10:44 Dockerfile
-rw-r--r--   1 icebergu  staff    11K 12  2 10:44 LICENSE
-rw-r--r--   1 icebergu  staff   3.2K 12  2 10:44 Makefile
drwxr-xr-x   4 icebergu  staff   128B 12  2 10:44 cmd
drwxr-xr-x  10 icebergu  staff   320B 12  2 10:44 deploy
drwxr-xr-x   4 icebergu  staff   128B 12  2 10:44 examples
-rw-r--r--   1 icebergu  staff   3.5K 12  2 10:44 go.mod
-rw-r--r--   1 icebergu  staff   131K 12  2 10:44 go.sum
drwxr-xr-x   8 icebergu  staff   256B 12  2 10:44 hack
drwxr-xr-x  10 icebergu  staff   320B 12  2 10:44 pkg
drwxr-xr-x  13 icebergu  staff   416B 12  2 10:44 vendor
```
部署过程分为三个步骤:
1. 部署存储组件，默认为 MySQL 8.0
2. 部署 crd yaml
3. 部署 Clusterpedia

## 部署存储组件
Clusterpedia 默认提供了 MySQL 8.0 作为存储组件，并使用 local pv 的方式存储数据。

在部署 MySQL 时，需要手动指定 local pv 所在的节点，并在该节点上创建 */var/local/clusterpedia/internalstorage/mysql* 目录。
> 用户也可以选择使用自己的存储组件，默认存储层支持 MySQL 和 postgres
```sh
$ export STORAGE_NODE_NAME=<挑选的节点名称>
$ cd ./deploy/internalstorage
$ sed "s|__NODE_NAME__|$STORAGE_NODE_NAME|g" \
    ./templates/clusterpedia_internalstorage_local_pv.yaml > clusterpedia_internalstorage_local_pv.yaml

$ # 登录节点主机： mkdir -p /var/local/clusterpedia/internalstorage/mysql

$ # 部署 mysql
$ kubectl apply -f .
namespace/clusterpedia-system created
service/clusterpedia-internalstorage-mysql created
persistentvolumeclaim/internalstorage-mysql created
secret/internalstorage-mysql created
deployment.apps/clusterpedia-internalstorage-mysql created
persistentvolume/clusterpedia-internalstorage-mysql created

$ # 为方便后续操作，跳回到项目根目录
$ cd ../..
```
## 部署 CRD
运行以下命令 apply yaml 文件即可部署 CRD：
```sh
$ kubectl apply -f ./deploy/crds
customresourcedefinition.apiextensions.k8s.io/pediaclusters.clusters.clusterpedia.io created
```
## 部署 Clusterpedia
运行以下命令 apply yaml 文件即可部署 Clusterpedia：
> 如果选择连接自己的数据库，需要修改存储层配置 `./deploy/clusterpedia_internalstorage_configmap.yaml`
```sh
$ kubectl apply -f ./deploy
piservice.apiregistration.k8s.io/v1alpha1.pedia.clusterpedia.io created
serviceaccount/clusterpedia-apiserver created
service/clusterpedia-apiserver created
deployment.apps/clusterpedia-apiserver created
clusterrole.rbac.authorization.k8s.io/clusterpedia created
clusterrolebinding.rbac.authorization.k8s.io/clusterpedia created
serviceaccount/clusterpedia-clustersynchro-manager created
deployment.apps/clusterpedia-clustersynchro-manager created
configmap/clusterpedia-internalstorage created
namespace/clusterpedia-system unchanged
```
当所有 pod 都起来后，就可以运行以下命令收集检索集群资源：
```sh
$ kubectl -n clusterpedia-system get pods
NAME                                                   READY   STATUS    RESTARTS   AGE
clusterpedia-apiserver-55ff787656-bwfqb                1/1     Running   0          36s
clusterpedia-clustersynchro-manager-5f55dc5887-x26bg   1/1     Running   0          35s
clusterpedia-internalstorage-mysql-6ffbc5f4c8-kxnh7    1/1     Running   0          2m30s
```
[回到页首](#top)
# 收集集群资源<span id="get"></span>
部署 clusterpedia crds 后，可以通过 kubectl 来操作 *PediaCluster* 资源。
```sh
$ kubectl get pediaclusters
```
在 examples 目录下，可以看到 *PediaCluster* 的示例：
```yaml
apiVersion: clusters.clusterpedia.io/v1alpha1
kind: PediaCluster
metadata:
  name: cluster-example
spec:
  apiserverURL: "https://10.30.43.43:6443"
  caData:
  tokenData:
  certData:
  keyData:
  resources:
  - group: apps
    resources:
     - deployments
  - group: ""
    resources:
     - pods
```
*PediaCluster* 在配置上可以分成两部分：
* **集群认证**
* **指定资源收集 `.spec.resources`**
## 集群认证
`caData`、`tokenData`、`certData`、`keyData` 字段可以用于集群的验证。
> 当前暂时不支持从 ConfigMap 或者 Secret 中获取验证相关的信息，
> 不过该信息已经在 Roadmap 中了

在设置验证字段时，注意要使用 base64 编码后的字符串。

在 `./examples` 目录下提供了用于子集群的 rbac yaml `clusterpedia_synchro_rbac.yaml`，可以用来方便地获取子集群的权限 token。

在子集群中部署该 yaml，然后获取对应的 token 和 ca 证书。

```sh
$ # 切换到子集群创建 rbac 相关资源
$ kubectl apply -f examples/clusterpedia_synchro_rbac.yaml
$ SYNCHRO_TOKEN=$(kubectl get secret $(kubectl get serviceaccount clusterpedia-synchro -o jsonpath='{.secrets[0].name}') -o jsonpath='{.data.token}')
$ SYNCHRO_CA=$(kubectl get secret $(kubectl get serviceaccount clusterpedia-synchro -o jsonpath='{.secrets[0].name}') -o jsonpath='{.data.ca\.crt}')
```
复制 *./examples/pediacluster.yaml*，修改 `.spec.apiserverURL` 和 `.metadata.name` 字段，并将 `$SYNCHRO_TOKEN` 和 `$SYNCHRO_CA` 填写到 `tokenData` 和 `caData` 中。
```sh
$ kubectl apply -f cluster-1.yaml
pediacluster.clusters.clusterpedia.io/cluster-1 created
```
## 资源收集
可以通过设置 `spec.resources` 字段的 `group` 和 `group` 下的 `resources` 来指定收集的资源。

在 status 中也可以看到资源的收集状态：
```sh
status:
  conditions:
  - lastTransitionTime: "2021-12-02T04:00:45Z"
    message: ""
    reason: Healthy
    status: "True"
    type: Ready
  resources:
  - group: ""
    resources:
    - kind: Pod
      namespaced: true
      resource: pods
      syncConditions:
      - lastTransitionTime: "2021-12-02T04:00:45Z"
        status: Syncing
        storageVersion: v1
        version: v1
  - group: apps
    resources:
    - kind: Deployment
      namespaced: true
      resource: deployments
      syncConditions:
      - lastTransitionTime: "2021-12-02T04:00:45Z"
        status: Syncing
        storageVersion: v1
        version: v1
  version: v1.22.2
```
[回到页首](#top)
# 资源检索<span id="view"></span>
配置好需要收集的资源后，就可以检索集群资源了。Clusterpedia 支持两种资源检索：
* 兼容 Kubernetes OpenAPI 的资源检索
* `集合资源 (Collection Resource)`的检索
```sh
$ kubectl api-resources | grep pedia.clusterpedia.io
collectionresources     pedia.clusterpedia.io/v1alpha1  false   CollectionResource
resources               pedia.clusterpedia.io/v1alpha1  false   Resources
```

为了方便更好地使用 kubectl 进行检索，可以先通过 `make gen-clusterconfig` 为子集群创建用于检索的'快捷方式'：
```sh
$ make gen-clusterconfigs
./hack/gen-clusterconfigs.sh
Current Context: kubernetes-admin@kubernetes
Current Cluster: kubernetes
        Server: https://10.6.11.11:6443
        TLS Server Name:
        Insecure Skip TLS Verify:
        Certificate Authority:
        Certificate Authority Data: ***
Cluster "clusterpedia" set.
Cluster "cluster-1" set.
```
使用 `kubectl config get-clusters` 命令可以查看当前支持的集群。

其中 clusterpedia 是一个特殊的 cluster，用于多集群检索，以 `kubectl --cluster clusterpedia` 的方式来检索多个集群的资源。

## 多集群资源检索
先看一下收集了哪些资源，只有被收集的资源才可以进行检索：
```sh
$ kubectl --cluster clusterpedia api-resources
NAME          SHORTNAMES   APIVERSION   NAMESPACED   KIND
pods          po           v1           true         Pod
deployments   deploy       apps/v1      true         Deployment
```
可以看到当前收集的资源，支持 pods 和 deployments.apps 两种资源。

**查看所有集群的 `kube-system` 命名空间下的 deployments：**
```sh
$ kubectl --cluster clusterpedia get deployments -n kube-system
CLUSTER     NAME                      READY   UP-TO-DATE   AVAILABLE   AGE
cluster-1   coredns                   2/2     2            2           68d
cluster-2   calico-kube-controllers   1/1     1            1           64d
cluster-2   coredns                   2/2     2            2           64d
```
**查看所有集群的 `kube-system`、`default` 两个命名空间下的 deployments：**
```sh
$ kubectl --cluster clusterpedia get deployments -A -l "search.clusterpedia.io/namespaces in (kube-system, default)"
NAMESPACE     CLUSTER     NAME                      READY   UP-TO-DATE   AVAILABLE   AGE
kube-system   cluster-1   coredns                   2/2     2            2           68d
kube-system   cluster-2   calico-kube-controllers   1/1     1            1           64d
kube-system   cluster-2   coredns                   2/2     2            2           64d
default       cluster-2   dd-airflow-scheduler      0/1     1            0           54d
default       cluster-2   dd-airflow-web            0/1     1            0           54d
default       cluster-2   hello-world-server        1/1     1            1           27d
default       cluster-2   keycloak                  1/1     1            1           52d
default       cluster-2   keycloak-02               1/1     1            1           41d
default       cluster-2   my-nginx                  1/1     1            1           40d
default       cluster-2   nginx-dev                 1/1     1            1           15d
default       cluster-2   openldap                  1/1     1            1           41d
default       cluster-2   phpldapadmin              1/1     1            1           41d
```

**查看 cluster-1、cluster-2 两个集群下的 `kube-system`、`default` 命名空间下的 deployments：**
```sh
$ kubectl --cluster clusterpedia get deployments -A -l "search.clusterpedia.io/clusters in (cluster-1, cluster-2),\
        search.clusterpedia.io/namespaces in (kube-system,default)"
NAMESPACE     CLUSTER     NAME                      READY   UP-TO-DATE   AVAILABLE   AGE
kube-system   cluster-1   coredns                   2/2     2            2           68d
kube-system   cluster-2   calico-kube-controllers   1/1     1            1           64d
kube-system   cluster-2   coredns                   2/2     2            2           64d
default       cluster-2   dd-airflow-scheduler      0/1     1            0           54d
default       cluster-2   dd-airflow-web            0/1     1            0           54d
default       cluster-2   hello-world-server        1/1     1            1           27d
default       cluster-2   keycloak                  1/1     1            1           52d
default       cluster-2   keycloak-02               1/1     1            1           41d
default       cluster-2   my-nginx                  1/1     1            1           40d
default       cluster-2   nginx-dev                 1/1     1            1           15d
default       cluster-2   openldap                  1/1     1            1           41d
default       cluster-2   phpldapadmin              1/1     1            1           41d
```

**查看 cluster-1、cluster-2 两个集群下的 `kube-system`、`default` 命名空间下的 deployments，并根据资源名称排序：**
```sh
$ kubectl --cluster clusterpedia get deployments -A -l "search.clusterpedia.io/clusters in (cluster-1, cluster-2),\
        search.clusterpedia.io/namespaces in (kube-system,default),\
        search.clusterpedia.io/orderby=name"
NAMESPACE     CLUSTER     NAME                      READY   UP-TO-DATE   AVAILABLE   AGE
kube-system   cluster-2   calico-kube-controllers   1/1     1            1           64d
kube-system   cluster-1   coredns                   2/2     2            2           68d
kube-system   cluster-2   coredns                   2/2     2            2           64d
default       cluster-2   dao-2048-2048             1/1     1            1           21d
default       cluster-2   dd-airflow-scheduler      0/1     1            0           54d
default       cluster-2   dd-airflow-web            0/1     1            0           54d
default       cluster-2   hello-world-server        1/1     1            1           27d
default       cluster-2   keycloak                  1/1     1            1           52d
default       cluster-2   keycloak-02               1/1     1            1           41d
default       cluster-2   my-nginx                  1/1     1            1           40d
default       cluster-2   nginx-dev                 1/1     1            1           15d
default       cluster-2   openldap                  1/1     1            1           41d
default       cluster-2   phpldapadmin              1/1     1            1           41d
```
[回到页首](#top)
## 指定集群检索
**如果想要检索指定集群的资源，可以使用 --cluster 指定具体的集群名称：**
```sh
$ kubectl --cluster cluster-1 get deployments -A
NAMESPACE                           CLUSTER     NAME                                            READY   UP-TO-DATE   AVAILABLE   AGE
calico-apiserver                    cluster-1   calico-apiserver                                1/1     1            1           68d
calico-system                       cluster-1   calico-kube-controllers                         1/1     1            1           68d
calico-system                       cluster-1   calico-typha                                    1/1     1            1           68d
capi-system                         cluster-1   capi-controller-manager                         1/1     1            1           42d
capi-kubeadm-bootstrap-system       cluster-1   capi-kubeadm-bootstrap-controller-manager       1/1     1            1           42d
capi-kubeadm-control-plane-system   cluster-1   capi-kubeadm-control-plane-controller-manager   1/1     1            1           42d
capv-system                         cluster-1   capv-controller-manager                         1/1     1            1           42d
cert-manager                        cluster-1   cert-manager                                    1/1     1            1           42d
cert-manager                        cluster-1   cert-manager-cainjector                         1/1     1            1           42d
cert-manager                        cluster-1   cert-manager-webhook                            1/1     1            1           42d
clusterpedia-system                 cluster-1   clusterpedia-apiserver                          1/1     1            1           27m
clusterpedia-system                 cluster-1   clusterpedia-clustersynchro-manager             1/1     1            1           27m
clusterpedia-system                 cluster-1   clusterpedia-internalstorage-mysql              1/1     1            1           29m
kube-system                         cluster-1   coredns                                         2/2     2            2           68d
tigera-operator                     cluster-1   tigera-operator                                 1/1     1            1           68d
```
除了 `search.clusterpedia.io/clusters` 外其余复杂查询的支持与多集群检索相同。

如果要获取一个资源的详情，则需要指定是哪个集群：
```sh
$ kubectl --cluster cluster-1 -n kube-system get deployments coredns -o wide
CLUSTER     NAME      READY   UP-TO-DATE   AVAILABLE   AGE   CONTAINERS   IMAGES                                                   SELECTOR
cluster-1   coredns   2/2     2            2           68d   coredns      registry.aliyuncs.com/google_containers/coredns:v1.8.4   k8s-app=kube-dns
```
## 复杂检索
Clusterpedia 支持以下复杂检索：
* 指定一个或者多个`集群名称`
* 指定一个或者多个`命名空间`
* 指定一个或者多个`资源名称`
* 指定多个字段的`排序`
* `分页`功能，可以指定 size 和 offset
* `labels 过滤`

字段排序的实际效果取决于存储层。默认存储层支持根据 `cluster`、`name`、`namespace`、`created_at`、`resource_version` 进行正序或者倒序的排序。
## 检索条件的传递方式
上面的示例演示了使用 kubectl 进行检索，其中复杂的检索条件通过 `label` 进行传递。Clusterpedia 还支持直接通过 `url query` 传递这些检索条件。

|作用|label key|url query|example|
|---|---|---|---|
|指定资源名字|search.clusterpedia.io/names|names|`?names=pod-1,pod-2`
|指定命名空间|search.clusterpedia.io/namespaces|namespaces|`?namespaces=kube-system,default`
|指定集群名称|search.clusterpedia.io/clusters|clusters|`?clusters=cluster-1,cluster-2`
|指定排序字段|search.clusterpedia.io/orderby|orderby|`?orderby=name desc,namespace`
|指定 size |search.clusterpedia.io/size|limit|`?limit=100`
|指定 offset |search.clusterpedia.io/offset|continue|`?continue=10`

`label key` 的操作符支持 ==、=、!=、in、not in。  

> 对于 limit 这个条件，kubectl 只能通过 `--chunk-size` 来指定，而不能通过 label key。

[回到页首](#top)
## 集合资源 (Collection Resource)
Clusterpedia 还能对资源进行更高级的聚合，例如使用 `Collection Resource` 可以一次性获取到一组不同类型的资源。

我们先查看一下当前 Clusterpedia 支持哪些 `Collection Resource`：
```sh
$ kubectl get collectionresources
NAME        RESOURCES
workloads   deployments.apps,daemonsets.apps,statefulsets.apps
```

通过获取 workloads 便可获取到一组 deployment、daemonset、statefulset 聚合在一起的资源，而且 `Collection Resource` 同样支持所有的复杂查询。

**kubectl get collectionresources workloads 会默认获取所有集群下所有命名空间的相应资源：**
```sh
$ kubectl get collectionresources workloads
CLUSTER     GROUP   VERSION   KIND         NAMESPACE                     NAME                                          AGE
cluster-1   apps    v1        DaemonSet    kube-system                   vsphere-cloud-controller-manager              63d
cluster-2   apps    v1        Deployment   kube-system                   calico-kube-controllers                       109d
cluster-2   apps    v1        Deployment   kube-system                   coredns-coredns                               109d
```
> 在 cluster-1 中增加收集 Daemonset，输出有删减

由于 kubectl 的限制，所以无法在 kubectl 使用复杂查询，只能通过 `url query` 的方式来查询。

## 对资源进行更复杂的操作<span id="complicated"></span>
Clusterpedia 不仅能做资源检索，与 wiki 一样，它也应该具有对资源简单的控制能力，例如 watch、create、delete、update 等操作。

对于写操作，实际会采用双写 + 响应 warning 的方式来完成。

**该功能会进行讨论，根据社区需求来决定是否实现**

## 集群的自动发现和收集<span id="discovery"></span>
Clusterpedia 中用来表示集群的资源叫做 *PediaCluster*, 而不是简单的 Cluster。

**这是因为 Clusterpedia 设计初衷是建立在已有的多集群管理平台之上。**

为了遵循初衷，第一个问题是不能与已有的多集群平台中的资源冲突，Cluster 便是一个非常通用的代表集群的资源名称。

另外为了更好地接入到已有的多集群平台上，让已经接入的集群可以自动完成资源收集，我们需要另外的一个集群发现机制。这个发现机制需要解决以下问题：
* 能够获取到访问集群的认证信息
* 可以配置触发 PediaCluster 生命周期的 Condition 条件
* 设置默认的资源收集策略以及名称前缀等

这个功能会在 2022 Q1 或者 Q2 中开始详细讨论并实现。

[回到页首](#top)
# Roadmap<span id="roadmap"></span>
当前只是暂定的 Roadmap，具体的排期还要看社区的需求程度。

**关于一些未加入到 Roadmap 中的特性，可以在 [issues](https://github.com/clusterpedia-io/clusterpedia/issues) 中讨论**

## 2021 Q4
* [支持字段裁剪](https://github.com/clusterpedia-io/clusterpedia/issues/4)
* 自定义资源的收集

## 2022 Q1
* 支持插件化存储层
* 实现[集群的自动发现和收集](#discovery)

## 2022 Q2
* 支持对[集群资源更多的控制](#complicated)，例如 watch/create/update/delete 等操作
* 默认存储层支持自定义 Collection Resource
* 支持请求附带关系资源


[回到页首](#top)

# 使用注意
## 多集群网络连通性
Clusterpedia 实际并不会解决多集群环境下的网络连通问题，用户可以使用 [tower](https://github.com/kubesphere/tower) 等工具来连接访问子集群，也可以借助 [submariner](https://github.com/submariner-io/submariner) 或者 [skupper](https://github.com/skupperproject/skupper) 来解决跨集群网络问题。
