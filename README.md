<div align="center"><img src="./docs/images/clusterpedia.png" style="width:900px;" /></div>
<p align="center">
  <b>The Encyclopedia of Kubernetes clusters</b>
</p>

# Clusterpedia
This name Clusterpedia is inspired by Wikipedia. It is an encyclopedia of multi-cluster to synchronize, search for, and simply control multi-cluster resources. 

Clusterpedia can synchronize resources with multiple clusters and provide more powerful search features on the basis of compatibility with Kubernetes OpenAPI to help you effectively get any multi-cluster resource that you are looking for in a quick and easy way.  

> The capability of Clusterpedia is not only to search for and view but also simply control resources in the future, just like Wikipedia that supports for editing entries.

## Architecture <span id="design"></span>
<div align="center"><img src="./docs/images/arch.png" style="width:900px;" /></div>
The architecture consists of four parts:

* **Clusterpedia APIServer**: Register to `Kubernetes APIServer` by the means of [Aggregated API](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/apiserver-aggregation/) and provide services through a unified entrance
* **ClusterSynchro Manager**: Manage the cluster synchro that is used to synchronize cluster resources
* **Storage Layer**: Connect with a specific storage component and then register to Clusterpedia APIServer and ClusterSynchro Manager via a storage layer interface
* **Storage Component**: A specific storage facility such as **MySQL**, **PostgreSQL**, **Redis** or other **Graph Databases**

In addition, Clusterpedia will use the Custom Resource - *PediaCluster* to implement cluster authentication and configure resources for synchronization.

Clusterpedia also provides a `Default Storage Layer` that can connect with **MySQL** and **PostgreSQL**.
> Clusterpedia does not care about the specific storage components used by users,
> you can choose or implement the storage layer according to your own needs,
> and then register the storage layer in Clusterpedia as a plug-in

## Features
- [x] Support for complex search, filters, sorting, paging, and more
- [ ] Support for requesting relevant resources when you query resources
- [x] Unify the search entry for master clusters and multi-cluster resources
- [x] Compatible with kubernetes OpenAPI, where you can directly use kubectl for multi-cluster search without any third-party plug-ins or tools
- [x] Compatible with synchronizing different versions of cluster resources, not restricted by the version of master cluster
- [x] High performance and low memory consumption for resource synchronization
- [x] Automatically start/stop resource synchronization according to the current health status of the cluster
- [ ] Support for plug-in storage layer. You can use other storage components to customize the storage layer according to your needs.
- [x] High availability
> The above unimplemented features are already in the [Roadmap](/ROADMAP.md)

---
[Installation](https://clusterpedia.io/docs/installation/) | [Import Clusters](https://clusterpedia.io/docs/usage/import-clusters/) | [Sync Cluster Resources](https://clusterpedia.io/docs/usage/sync-resources/)
---

## Usage Samples
We can search for resources configured in *PediaCluster*, Clusterpedia supports two types of resource search:
* Search for resources that are compatible with Kubernetes OpenAPI
* Search for [`Collection Resource`](https://clusterpedia.io/docs/concepts/collection-resource/)
```sh
$ kubectl api-resources | grep clusterpedia.io
collectionresources     clusterpedia.io/v1beta1  false   CollectionResource
resources               clusterpedia.io/v1beta1  false   Resources
```

### Search Label and URL Query
|Role| search label key|url query|
| -- | --------------- | ------- |
|Filter cluster names|`search.clusterpedia.io/clusters`|`clusters`|
|Filter namespaces|`search.clusterpedia.io/namespaces`|`namespaces`|
|Filter resource names|`search.clusterpedia.io/names`|`names`|
|Since creation time|`search.clusterpedia.io/since`|`since`|
|Before creation time|`search.clusterpedia.io/before`|`before`|
|Specified Owner UID|`search.clusterpedia.io/owner-uid`|`ownerUID`|
|Specified Owner Seniority|`search.clusterpedia.io/owner-seniority`|`ownerSeniority`|
|Specified Owner Name|`search.clusterpedia.io/owner-name`|`ownerName`|
|Specified Owner Group Resource|`search.clusterpedia.io/owner-gr`|`ownerGR`|
|Order by fields|`search.clusterpedia.io/orderby`|`orderby`|
|Set page size|`search.clusterpedia.io/size`|`limit`|
|Set page offset|`search.clusterpedia.io/offset`|`continue`|
|Response include Continue|`search.clusterpedia.io/with-continue`|`withContinue`
|Response include remaining count|`search.clusterpedia.io/with-remaining-count`|`withRemainingCount`

**Both Search Labels and URL Query support same operators as Label Selector:**
* `exist`, `not exist`
* `=`, `==`, `!=`
* `in`, `notin`

More information about [Search Conditions](https://clusterpedia.io/docs/usage/search/),
about [Label Selector](https://clusterpedia.io/docs/usage/search/#label-selector) and [Field Selector](https://clusterpedia.io/docs/usage/search/#field-selector)

### Search in Kubernetes OpenAPI Compatible Way
Although we can use URL to search resources, if we want to use `kubectl` to query more conveniently, 
we need to [configure the cluster shortcut for `kubectl`](https://clusterpedia.io/docs/usage/access-clusterpedia/#configure-the-cluster-shortcut-for-kubectl).

We can use `kubectl --cluster <cluster name>` to specify the cluster, if `<cluster name>` is `clusterpedia`,
it meas it is a multi-cluster search operation.

First check which resources are synchronized. We cannot find a resource until it is properly synchronized:
```sh
$ kubectl --cluster clusterpedia api-resources
NAME                  SHORTNAMES   APIVERSION                     NAMESPACED   KIND
configmaps            cm           v1                             true         ConfigMap
events                ev           v1                             true         Event
namespaces            ns           v1                             false        Namespace
nodes                 no           v1                             false        Node
pods                  po           v1                             true         Pod
services              svc          v1                             true         Service
daemonsets            ds           apps/v1                        true         DaemonSet
deployments           deploy       apps/v1                        true         Deployment
replicasets           rs           apps/v1                        true         ReplicaSet
statefulsets          sts          apps/v1                        true         StatefulSet
cronjobs              cj           batch/v1                       true         CronJob
jobs                               batch/v1                       true         Job
clusters                           cluster.kpanda.io/v1alpha1     false        Cluster
ingressclasses                     networking.k8s.io/v1           false        IngressClass
ingresses             ing          networking.k8s.io/v1           true         Ingress
clusterrolebindings                rbac.authorization.k8s.io/v1   false        ClusterRoleBinding
clusterroles                       rbac.authorization.k8s.io/v1   false        ClusterRole
roles                              rbac.authorization.k8s.io/v1   true         Role

$ kubectl --cluster cluster-1 api-resources
...
```

#### Search in Multiple Clusters
> Usage of [multi-cluster search](https://clusterpedia.io/docs/usage/search/multi-cluster/) in documents

**Get deployments in the `kube-system` namespace of all clusters:**
```sh
$ kubectl --cluster clusterpedia get deployments -n kube-system
CLUSTER     NAME                      READY   UP-TO-DATE   AVAILABLE   AGE
cluster-1   coredns                   2/2     2            2           68d
cluster-2   calico-kube-controllers   1/1     1            1           64d
cluster-2   coredns                   2/2     2            2           64d
```

**Get deployments in the two namespaces `kube-system` and `default` of all clusters:**
```sh
$ kubectl --cluster clusterpedia get deployments -A -l "search.clusterpedia.io/namespaces in (kube-system, default)"
NAMESPACE     CLUSTER     NAME                      READY   UP-TO-DATE   AVAILABLE   AGE
kube-system   cluster-1   coredns                   2/2     2            2           68d
kube-system   cluster-2   calico-kube-controllers   1/1     1            1           64d
kube-system   cluster-2   coredns                   2/2     2            2           64d
default       cluster-2   dd-airflow-scheduler      0/1     1            0           54d
default       cluster-2   dd-airflow-web            0/1     1            0           54d
default       cluster-2   hello-world-server        1/1     1            1           27d
default       cluster-2   openldap                  1/1     1            1           41d
default       cluster-2   phpldapadmin              1/1     1            1           41d
```

**Get deployments in the `kube-system` and `default` namespaces in cluster-1 and cluster-2:**
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
default       cluster-2   openldap                  1/1     1            1           41d
default       cluster-2   phpldapadmin              1/1     1            1           41d
```

**Get deployments in the `kube-system` and `default` namespaces in cluster-1 and cluster-2:**
```sh
$ kubectl --cluster clusterpedia get deployments -A -l "search.clusterpedia.io/clusters in (cluster-1, cluster-2),\
    search.clusterpedia.io/namespaces in (kube-system,default),\
    search.clusterpedia.io/orderby=name"
NAMESPACE     CLUSTER     NAME                      READY   UP-TO-DATE   AVAILABLE   AGE
kube-system   cluster-2   calico-kube-controllers   1/1     1            1           64d
kube-system   cluster-1   coredns                   2/2     2            2           68d
kube-system   cluster-2   coredns                   2/2     2            2           64d
default       cluster-2   dd-airflow-scheduler      0/1     1            0           54d
default       cluster-2   dd-airflow-web            0/1     1            0           54d
default       cluster-2   hello-world-server        1/1     1            1           27d
default       cluster-2   openldap                  1/1     1            1           41d
default       cluster-2   phpldapadmin              1/1     1            1           41d
```

#### Search a specific cluster
> Usage of [specified cluster search](https://clusterpedia.io/docs/usage/search/specified-cluster/) in documents

**If you want to search a specific cluster for any resource therein, you can add --cluster to specify the cluster name:**
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
Except for `search.clusterpedia.io/clusters`, the support for other complex queries is same as that for multi-cluster search.

If you want to learn about the details of a resource, you need to specify which cluster it is:
```sh
$ kubectl --cluster cluster-1 -n kube-system get deployments coredns -o wide
CLUSTER     NAME      READY   UP-TO-DATE   AVAILABLE   AGE   CONTAINERS   IMAGES                                                   SELECTOR
cluster-1   coredns   2/2     2            2           68d   coredns      registry.aliyuncs.com/google_containers/coredns:v1.8.4   k8s-app=kube-dns
```

**Find the related pods by the name of the deployment**
First view the deployments in default namespace
```sh
$ kubectl --cluster cluster-1 get deployments
NAME                      READY   UP-TO-DATE   AVAILABLE   AGE
fake-pod                  3/3     3            3           104d
test-controller-manager   0/0     0            0           7d21h
```

Use `owner-name` to specify Owner Name and use `owner-seniority` to promote the Owner's seniority.
```
$ kubectl --cluster cluster-1 get pods -l "search.clusterpedia.io/owner-name=fake-pod,search.clusterpedia.io/owner-seniority=1" 
NAME                                                 READY   STATUS      RESTARTS         AGE
fake-pod-698dfbbd5b-74cjx                            1/1     Running     0                12d
fake-pod-698dfbbd5b-tmcw7                            1/1     Running     0                3s
fake-pod-698dfbbd5b-wvtvw                            1/1     Running     0                3s
```

Lean More About [Search by Parent or Ancestor Owner](https://clusterpedia.io/docs/usage/search/specified-cluster/#search-by-parent-or-ancestor-owner)

### Search for [Collection Resource](https://clusterpedia.io/docs/concepts/collection-resource/)
Clusterpedia can also perform more advanced aggregation of resources. For example, you can use `Collection Resource` to get a set of different resources at once.

Let's first check which `Collection Resource` currently Clusterpedia supports:
```sh
$ kubectl get collectionresources
NAME        RESOURCES
workloads   deployments.apps,daemonsets.apps,statefulsets.apps
```

By getting workloads, you can get a set of resources aggregated by `deployments`, `daemonsets`, and `statefulsets`, and `Collection Resource` also supports for all complex queries.

**`kubectl get collectionresources workloads` will get the corresponding resources of all namespaces in all clusters by default:**
```sh
$ kubectl get collectionresources workloads
CLUSTER     GROUP   VERSION   KIND         NAMESPACE                     NAME                                          AGE
cluster-1   apps    v1        DaemonSet    kube-system                   vsphere-cloud-controller-manager              63d
cluster-2   apps    v1        Deployment   kube-system                   calico-kube-controllers                       109d
cluster-2   apps    v1        Deployment   kube-system                   coredns-coredns                               109d
```
> Add the collection of Daemonset in cluster-1 and some of the above output is cut out

Due to the limitation of kubectl, you cannot use complex queries in kubectl and can only be queried by `URL Query`.

[Lean More](https://clusterpedia.io/docs/usage/search/collection-resource/)

## Proposals
### Automatic discovery and sync cluster resource<span id="discovery"></span>
The resource used to represent the cluster in Clusterpedia is called *PediaCluster*, not a simple *Cluster*.

**This is because Clusterpedia was originally designed to build on the existing multi-cluster management platform.**

In order to keep the original intention, the first issue is that Clusterpedia should not conflict with the resources in the existing multi-cluster platform. Cluster is a very common resource name that represents a cluster.

In addition, in order to better connect with the existing multi-cluster platform and enable the connected clusters automatically complete resource synchronization, we need a new mechanism to discover clusters. This discovery mechanism needs to solve the following issues:
* Get the authentication info to access the cluster
* Configure conditions that trigger the lifecycle of PediaCluster
* Set the default policy and prefix name for resource synchronization

This feature will be discussed and implemented in detail in Q1 or Q2 2022.

### Perform more complex control over resources<span id="complicated"></span>
In addition to resource search, similar to Wikipedia, Clusterpedia should also have simple capability of resource control, such as watch, create, delete, update, and more.

In fact, a write action is implemented by double write + warning response.

**We will discuss this feature and decide whether we should implement it according to the community needs**

## Notes
### Multi-cluster network connectivity
Clusterpedia does not actually solve the problem of network connectivity in a multi-cluster environment. You can use tools such as [tower](https://github.com/kubesphere/tower) to connect and access sub-clusters, or use [submariner](https://github.com/submariner-io/submariner) or [skupper](https://github.com/skupperproject/skupper) to solve cross-cluster network problems.

## Contact <span id="contact"></span>
If you have any question, feel free to reach out to us in the following ways:
* [Slack](https://join.slack.com/t/clusterpedia/shared_invite/zt-11smmdntu-Im1YjOahqvHhJFqNzLELOA)

## Contributors

<a href="https://github.com/clusterpedia-io/clusterpedia/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=clusterpedia-io/clusterpedia" />
</a>

Made with [contrib.rocks](https://contrib.rocks).

## License
Copyright 2022 the Clusterpedia Authors. All rights reserved.

Licensed under the Apache License, Version 2.0.
