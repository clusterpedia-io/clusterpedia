<span id="top"></span>
<h1 align="center">
  Clusterpedia
</h1>

<p align="center">
  <b>The Encyclopedia of Kubernetes clusters</b>
</p>
This name Clusterpedia is inspired by Wikipedia. It is an encyclopedia of multi-cluster to synchronize, search for, and simply control multi-cluster resources. 

Clusterpedia can synchronize resources with multiple clusters and provide more powerful search features on the basis of compatibility with Kubernetes OpenAPI to help you effectively get any multi-cluster resource that you are looking for in a quick and easy way.  


> The capability of Clusterpedia is not only to search for and view but also simply control resources in the future, just like Wikipedia that supports for editing entries.


This document introduces the following topics about Clusterpedia:
- [Architecture](#design)
- [Features](#features)
- [Deployment](#deployment)
- [Synchronize resources](#get)
- [Search for resources](#view)
- [Roadmap](#roadmap)
- [Contact](#contact)
# Architecture <span id="design"></span>
The architecture diagram of Clusterpedia is as follows:
<div align="center"><img src="./docs/images/arch.png" style="width:900px;" /></div>
The architecture consists of four parts:

* **Clusterpedia APIServer**: Register to Kube APIServer by the means of Aggregated API and provide services through a unified entrance
* **ClusterSynchro Manager**: Manage the Cluster Synchro that is used to synchronize cluster resources
* **Storage Layer**: Connect with a specific storage component and then register to Clusterpedia APIServer and ClusterSynchro Manager via a storage interface
* **Storage component**: A specific storage facility such as MySQL, postgres, redis or other graph databases

In addition, Clusterpedia will use the custom resource *PediaCluster* to implement cluster authentication and synchronize the resource configuration.

Clusterpedia also provides a default storage layer that can connect with MySQL and postgres.
> Clusterpedia does not care about the specific storage settings used by users,
> you can choose or implement the storage layer according to your own needs and then register the storage layer in Clusterpedia as a plug-in


[Go to top](#top)
# Features<span id="features"></span>
- [x] Support for complex search, filters, sorting, paging, and more
- [ ] Support for requesting relevant resources when you query resources
- [x] Unify the search entry for master clusters and multi-cluster resources
- [x] Compatible with kubernetes OpenAPI, where you can directly use kubectl for multi-cluster search without any third-party plug-ins or tools
- [x] Compatible with synchronizing different versions of cluster resources, not restricted by the version of master cluster
- [x] High performance and low memory consumption for resource synchronization
- [x] Automatically start/stop resource synchronization according to the current health status of the cluster
- [ ] Support for plug-in storage layer. You can use other storage components to customize the storage layer according to your needs.
- [x] High availability
> The above unimplemented features are already in the [Roadmap](#roadmap)


[Go to top](#top)
# Deployment<span id="deployment"></span>
Clusterpedia is in a very early stage and the deployment process is not good enough currently.

Therefore, you need to manually modify the yaml file in the process of deployment. The folder structure after cloning to your local machine is as follows:
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
The deployment process is divided into three steps:
1. Deploy a storage component that is MySQL 8.0 by default
2. Deploy crd yaml
3. Deploy Clusterpedia

## Deploy a storage component
By default, Clusterpedia provides MySQL 8.0 as a storage component and uses local pv to store data.

When deploying MySQL, you need to manually specify the node where the local pv is located and create the */var/local/clusterpedia/internalstorage/mysql* directory on the node.  
> You can also choose to use your own storage components. The storage layer supports MySQL and postgres by default.
```sh
$ export STORAGE_NODE_NAME=<selected node name>
$ cd ./deploy/internalstorage/mysql
$ sed "s|__NODE_NAME__|$STORAGE_NODE_NAME|g" `grep __NODE_NAME__ -rl ./templates` > clusterpedia_internalstorage_pv.yaml


$ # Log in to the selected node host: mkdir -p /var/local/clusterpedia/internalstorage/mysql

$ # deploy mysql
$ kubectl apply -f .
namespace/clusterpedia-system created
service/clusterpedia-internalstorage-mysql created
persistentvolumeclaim/internalstorage-mysql created
secret/internalstorage-mysql created
deployment.apps/clusterpedia-internalstorage-mysql created
persistentvolume/clusterpedia-internalstorage-mysql created

$ # To provide convenience for subsequent operations, go back to the root directory
$ cd ../../..
```
## Deploy CRD
Run the following command to apply the yaml file and deploy CRD:
```sh
$ kubectl apply -f ./deploy/crds
customresourcedefinition.apiextensions.k8s.io/pediaclusters.clusters.clusterpedia.io created
```
## Deploy Clusterpedia
Run the following command to apply the yaml file and deploy Clusterpedia:  
> If you choose to connect with your own database, you need to modify the storage layer configuration `./deploy/clusterpedia_internalstorage_configmap.yaml`
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
When all pods are running, you can run the following command to synchronize and search for cluster resources:
```sh
$ kubectl -n clusterpedia-system get pods
NAME                                                   READY   STATUS    RESTARTS   AGE
clusterpedia-apiserver-55ff787656-bwfqb                1/1     Running   0          36s
clusterpedia-clustersynchro-manager-5f55dc5887-x26bg   1/1     Running   0          35s
clusterpedia-internalstorage-mysql-6ffbc5f4c8-kxnh7    1/1     Running   0          2m30s
```
[Go to top](#top)
# Synchronize cluster resources<span id="get"></span>
After deploying clusterpedia crds, you can use kubectl to operate *PediaCluster* resources.
```sh
$ kubectl get pediaclusters
```
In the examples directory, you can check examples of *PediaCluster*:
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
The configuration of *PediaCluster* can be divided into two parts:
* **Cluster authentication**
* **Synchronize a specific resource `.spec.resources`**
## Cluster authentication
The fields of `caData`, `tokenData`, `certData`, and `keyData` can be used for cluster verification.
> Currently it does not support for getting the relevant verification information from ConfigMap or Secret.
> However, the information is already in the [Roadmap](#roadmap).

When setting the verification field, you shall use the strings encoded by base64.

The `. /examples` directory provides the rbac yaml `clusterpedia_synchro_rbac.yaml`, which can be used to easily obtain the permission token for a subcluster.

Deploy the yaml in the subcluster and get the proper token and CA certificate.

```sh
$ # Switch to the sub-cluster to create rbac related resources
$ kubectl apply -f examples/clusterpedia_synchro_rbac.yaml
$ SYNCHRO_TOKEN=$(kubectl get secret $(kubectl get serviceaccount clusterpedia-synchro -o jsonpath='{.secrets[0].name}') -o jsonpath='{.data.token}')
$ SYNCHRO_CA=$(kubectl get secret $(kubectl get serviceaccount clusterpedia-synchro -o jsonpath='{.secrets[0].name}') -o jsonpath='{.data.ca\.crt}')
```
Copy *./examples/pediacluster.yaml*, modify `.spec.apiserverURL` and `.metadata.name` fields, and fill `$SYNCHRO_TOKEN` and `$SYNCHRO_CA` into `tokenData` and `caData`.
```sh
$ kubectl apply -f cluster-1.yaml
pediacluster.clusters.clusterpedia.io/cluster-1 created
```
## Synchronize resources
You can specify the synchronized resources by setting `group` in the `spec.resources` field and the `resources` section under `group`.

You can also view the resource synchronization status in the `status` section:
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
[Go to top](#top)
# Search for resources<span id="view"></span>
After configuring the resources to be synchronized, you can search for the cluster resources. Clusterpedia supports two types of resource search:
* Search for resources that are compatible with Kubernetes OpenAPI
* Search for `Collection Resource`
```sh
$ kubectl api-resources | grep pedia.clusterpedia.io
collectionresources     pedia.clusterpedia.io/v1alpha1  false   CollectionResource
resources               pedia.clusterpedia.io/v1alpha1  false   Resources
```

In order to facilitate and well use kubectl for searching, you'd better create a 'shortcut' for searching the sub-cluster through `make gen-clusterconfig`:
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
Use the `kubectl config get-clusters` command to view the currently supported clusters.

In this case, Clusterpedia is a special cluster used to search for multi-clusters by using `kubectl --cluster clusterpedia`.

## Multi-cluster resource search
First check which resources are synchronized. You cannot find a resource until it is properly synchronized:
```sh
$ kubectl --cluster clusterpedia api-resources
NAME          SHORTNAMES   APIVERSION   NAMESPACED   KIND
pods          po           v1           true         Pod
deployments   deploy       apps/v1      true         Deployment
```
You can check the currently synchronized resources including pods and deployments.apps.

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
default       cluster-2   keycloak                  1/1     1            1           52d
default       cluster-2   keycloak-02               1/1     1            1           41d
default       cluster-2   my-nginx                  1/1     1            1           40d
default       cluster-2   nginx-dev                 1/1     1            1           15d
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
default       cluster-2   keycloak                  1/1     1            1           52d
default       cluster-2   keycloak-02               1/1     1            1           41d
default       cluster-2   my-nginx                  1/1     1            1           40d
default       cluster-2   nginx-dev                 1/1     1            1           15d
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
[Go to top](#top)
## Search a specific cluster
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
## Complex search
Clusterpedia supports for the following complex search:
* Specify one or more `cluster names`
* Specify one or more `namespaces`
* Specify one or more `resource names`
* Specify how to `sort` multiple fields
* `Paging` function, by which you can specify its size and offset
* `filter by labels`

The actual effect of field sorting depends on the storage layer. By default, the storage layer supports for sorting according to `cluster`, `name`, `namespace`, `created_at`, and `resource_version` in a normal or reverse order.
## How search conditions are applied
The above example demonstrates how you can use kubectl to search for resources. Where, complex search conditions are applied via a `label`. Clusterpedia also supports for using these search conditions directly through `url query`.

|role|label key|url query|example|
|---|---|---|---|
|Specified resource name|search.clusterpedia.io/names|names|`?names=pod-1,pod-2`
|Specified namespace|search.clusterpedia.io/namespaces|namespaces|`?namespaces=kube-system,default`
|Specified cluster name|search.clusterpedia.io/clusters|clusters|`?clusters=cluster-1,cluster-2`
|Sort by specified fileds|search.clusterpedia.io/orderby|orderby|`?orderby=name desc,namespace`
|Specified limit |search.clusterpedia.io/limit|limit|`?limit=100`
|Specified offset |search.clusterpedia.io/offset|continue|`?continue=10`

The operators of `label key` include ==, =, !=, in, not in. 

> For the `limit` condition, kubectl can only specify a size by `--chunk-size` instead of the `label key`.

[Go to top](#top)
## Collection Resource
Clusterpedia can also perform more advanced aggregation of resources. For example, you can use `Collection Resource` to get a set of different resources at once.

Let's first check which `Collection Resource` currently Clusterpedia supports:
```sh
$ kubectl get collectionresources
NAME        RESOURCES
workloads   deployments.apps,daemonsets.apps,statefulsets.apps
```

By getting workloads, you can get a set of resources aggregated by deployment, daemonset, and statefulset, and `Collection Resource` also supports for all complex queries.

**`kubectl get collectionresources workloads` will get the corresponding resources of all namespaces in all clusters by default:**
```sh
$ kubectl get collectionresources workloads
CLUSTER     GROUP   VERSION   KIND         NAMESPACE                     NAME                                          AGE
cluster-1   apps    v1        DaemonSet    kube-system                   vsphere-cloud-controller-manager              63d
cluster-2   apps    v1        Deployment   kube-system                   calico-kube-controllers                       109d
cluster-2   apps    v1        Deployment   kube-system                   coredns-coredns                               109d
```
> Add the collection of Daemonset in cluster-1 and some of the above output is cut out

Due to the limitation of kubectl, you cannot use complex queries in kubectl and can only be queried by `url query`.

## Perform more complex control over resources<span id="complicated"></span>
In addition to resource search, similar to Wikipedia, Clusterpedia should also have simple capability of resource control, such as watch, create, delete, update, and more.

In fact, a write action is implemented by double write + warning response.

**We will discuss this feature and decide whether we should implement it according to the community needs**

## Automatic discovery and resource synchronization<span id="discovery"></span>
The resource used to represent the cluster in Clusterpedia is called *PediaCluster*, not a simple Cluster.

**This is because Clusterpedia was originally designed to build on the existing multi-cluster management platform.**

In order to keep the original intention, the first issue is that Clusterpedia should not conflict with the resources in the existing multi-cluster platform. Cluster is a very common resource name that represents a cluster.

In addition, in order to better connect with the existing multi-cluster platform and enable the connected clusters automatically complete resource synchronization, we need a new mechanism to discover clusters. This discovery mechanism needs to solve the following issues:
* Get the authentication info to access the cluster
* Configure conditions that trigger the lifecycle of PediaCluster
* Set the default policy and prefix name for resource synchronization

This feature will be discussed and implemented in detail in Q1 or Q2 2022.

[Go to top](#top)
# Roadmap<span id="roadmap"></span>
Currently, it is only a tentative roadmap and the specific schedule depends on the community needs.

**About some features not added to Roadmap, you can discuss in [issues](https://github.com/clusterpedia-io/clusterpedia/issues).**

## Q4 2021
* [Support for pruning field](https://github.com/clusterpedia-io/clusterpedia/issues/4)
* Synchronize custom resources

## Q1 2022
* Support for the plug-in storage layer
* Implement [automatic discovery and resource synchronization](#discovery)

## Q2 2022
* Support for [more control over cluster resources](#complicated), such as watch/create/update/delete operations
* The storage layer supports for custom Collection Resource by default
* Support for requests with relevant resources


[Go to top](#top)

# Remarks
## Multi-cluster network connectivity
Clusterpedia does not actually solve the problem of network connectivity in a multi-cluster environment. You can use tools such as [tower](https://github.com/kubesphere/tower) to connect and access sub-clusters, or use [submariner](https://github.com/submariner-io/submariner) or [skupper](https://github.com/skupperproject/skupper) to solve cross-cluster network problems.

# Contact <span id="contact"></span>
If you have any question, feel free to reach out to us in the following ways:
* [Slack](https://join.slack.com/t/clusterpedia/shared_invite/zt-zokhiijn-geVyvgFaAxsSZGOS_YgZ6g)
