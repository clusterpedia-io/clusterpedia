# Clusterpedia
This name Clusterpedia is inspired by Wikipedia. It is an encyclopedia of multi-cluster to synchronize, search for, and simply control multi-cluster resources.

Clusterpedia can synchronize resources with multiple clusters and provide more powerful search features on the basis of compatibility with Kubernetes OpenAPI to help you effectively get any multi-cluster resource that you are looking for in a quick and easy way.

# Prerequisites
* [Insall Helm version 3 or later](https://helm.sh/docs/intro/install/)

Pull the Clusterpedia repository.

> Currently, the chart has not been uploaded to the public charts repository.

```bash
git clone https://github.com/clusterpedia-io/clusterpedia.git
cd clusterpedia/charts
```

Since Clusterpedia uses `bitnami/postgresql` and `bitnami/mysql` as subcharts of storage components, it is necessary to add the bitnami repository and update the dependencies of the clusterpedia chart.

```bash
helm repo add bitnami https://charts.bitnami.com/bitnami
helm dependency build
```

# Choose storage components

The Clusterpedia chart provides two storage components such as `bitnami/postgresql` and `bitnami/mysql` to choose from as sub-charts.

`postgresql` is the default storage component. IF you want to use MySQL, you can add `--set postgresql.enabled=false --set mysql.enabled=true` in the subsequent installation command.

For specific configuration about storage components, see [bitnami/postgresql](https://github.com/bitnami/charts/tree/master/bitnami/postgresql) and [bitnami/mysql](https://github.com/bitnami/charts/tree/master/bitnami/mysql).

**You can also choose not to install any storage component, but use external components. For related settings, see charts/values.yaml**

# Choose a installation or management mode for CRDs

Clusterpedia requires proper CRD resources to be created in the retrieval environment. You can choose to manually deploy CRDs by using YAML, or you can manage it with Helm.

## Manage manually

```bash
kubectl apply -f ./_crds
```

## Manage with Helm

Manually add `--set installCRDs=true` in the subsequent installation command.


# Check if you need to create a local PV

Through the Clusterpedia chart, you can create storage components to use a local PV.

**You need to specify the node where the local PV is located through `--set persistenceMatchNode=<selected node name>` during installation.**

If you need not to create the local PV, you can use `--set persistenceMatchNode=None` to declare it explicitly.

## Install Clusterpedia

After the above procedure is completed, you can run the following command to install Clusterpedia:

```bash
helm install clusterpedia . \
--namespace clusterpedia-system \
--create-namespace \
--set persistenceMatchNode={{ LOCAL_PV_NODE }} \
--set installCRDs=true
```

## Uninstall Clusterpedia

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
kubectl delete -f ./_crds
```

**Note that PVC and PV will not be deleted. You need to manually delete them.**

If you created a local PV, you need log in to the node and remove all remained data about the local PV.

```bash
# Log in to the node with Local PV
rm /var/local/clusterpedia/internalstorage/<storage type>
```
