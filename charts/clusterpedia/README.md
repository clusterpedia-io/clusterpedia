# Clusterpedia

This name Clusterpedia is inspired by Wikipedia. It is an encyclopedia of multi-cluster to synchronize, search for, and
simply control multi-cluster resources.

Clusterpedia can synchronize resources with multiple clusters and provide more powerful search features on the basis of
compatibility with Kubernetes OpenAPI to help you effectively get any multi-cluster resource that you are looking for in
a quick and easy way.

## Prerequisites

* [Install Helm version 3 or later](https://helm.sh/docs/intro/install/)

### Local Installation

Pull the Clusterpedia repository.

```bash
git clone https://github.com/clusterpedia-io/clusterpedia.git
cd clusterpedia/charts/clusterpedia
```

Since Clusterpedia uses `bitnami/postgresql` and `bitnami/mysql` as subcharts of storage components, it is necessary to
add the bitnami repository and update the dependencies of the clusterpedia chart.

```bash
helm repo add bitnami https://charts.bitnami.com/bitnami
helm dependency build
```
### Remote Installation

First, add the Clusterpedia chart repo to your local repository.

```bash
$ helm repo add clusterpedia https://clusterpedia-io.github.io/clusterpedia/charts
$ helm repo list
NAME          	URL
clusterpedia  	https://clusterpedia-io.github.io/clusterpedia/charts
```

With the repo added, available charts and versions can be viewed.

```bash
helm search repo clusterpedia
```

## Install

### Choose storage components

The Clusterpedia chart provides two storage components such as `bitnami/postgresql` and `bitnami/mysql` to choose from
as sub-charts.

`postgresql` is the default storage component. IF you want to use MySQL, you can
add `--set postgresql.enabled=false --set mysql.enabled=true` in the subsequent installation command.

For specific configuration about storage components,
see [bitnami/postgresql](https://github.com/bitnami/charts/tree/master/bitnami/postgresql)
and [bitnami/mysql](https://github.com/bitnami/charts/tree/master/bitnami/mysql).

**You can also choose not to install any storage component, but use external components. It is already in the charts
directory, so you can directly use [values.yaml](./values.yaml)**

### Choose a installation or management mode for CRDs

Clusterpedia requires proper CRD resources to be created in the retrieval environment. You can choose to manually deploy
CRDs by using YAML, or you can manage it with Helm.

### Manage manually

```bash
kubectl apply -f ./crds
```

### Manage with Helm

Manually add `--set installCRDs=true` or `--set installCRDs=false --skip-crds` in the subsequent installation command.

### Check if you need to create a local PV

Through the Clusterpedia chart, you can create storage components to use a local PV.

**You need to specify the node where the local PV is located through `--set persistenceMatchNode=<selected node name>`
during installation.**

If you need not create the local PV, you can use `--set persistenceMatchNode=None` to declare it explicitly.

### Install Clusterpedia

After the above procedure is completed, you can run the following command to install Clusterpedia:

- local installation

```bash
helm install clusterpedia . \
--namespace clusterpedia-system \
--create-namespace \
--set persistenceMatchNode={{ LOCAL_PV_NODE }} \
--set installCRDs=true
```

- remote installation

> If you want to specify the version, you can install the chart with the `--version` argument.

```bash
helm install clusterpedia clusterpedia/clusterpedia \
--namespace clusterpedia-system \
--create-namespace \
--set persistenceMatchNode={{ LOCAL_PV_NODE }} \
--set installCRDs=true
```

### Create Cluster Auto Import Policy —— ClusterImportPolicy

After 0.4.0, Clusterpedia provides a more friendly way to interface to multi-cloud platforms.

Users can create `ClusterImportPolicy` to automatically discover managed clusters in the multi-cloud platform and
automatically synchronize them as `PediaCluster`,
so you don't need to maintain `PediaCluster` manually based on the managed clusters.

We maintain `PediaCluster` for each multi-cloud platform in
the [Clusterpedia repository](https://github.com/clusterpedia-io/clusterpedia/tree/main/deploy/clusterimportpolicy).
ClusterImportPolicy` for each multi-cloud platform.
**People also submit ClusterImportPolicy to Clusterpedia for interfacing to other multi-cloud platforms.**

After installing Clusterpedia, you can create the appropriate `ClusterImportPolicy`,
or [create a new `ClusterImportPolicy`](https://clusterpedia.io/docs/usage/interfacing-to-multi-cloud-platforms/#new-clusterimportpolicy)
according to your needs (multi-cloud platform).

For details, please refer
to [Interfacing to Multi-Cloud Platforms](https://clusterpedia.io/docs/usage/interfacing-to-multi-cloud-platforms)

```bash
kubectl get clusterimportpolicy
```

## Uninstall

If you have deployed `ClusterImportPolicy` then you need to clean up the `ClusterImportPolicy` resources first.

```bash
kubectl get clusterimportpolicy
```

Before uninstallation, you shall manually clear all `PediaCluster` resources.

```bash
kubectl get pediacluster
```

You can run the command to uninstall it after the `PediaCluster` resources are cleared.

```bash
helm -n clusterpedia-system uninstall clusterpedia
```

If you use any CRD resource that is manually created, you also need to manually clear the CRDs.

```bash
kubectl delete -f ./crds
```

**Note that PVC and PV will not be deleted. You need to manually delete them.**

If you created a local PV, you need log in to the node and remove all remained data about the local PV.

```bash
# Log in to the node with Local PV
rm /var/local/clusterpedia/internalstorage/<storage type>
```
