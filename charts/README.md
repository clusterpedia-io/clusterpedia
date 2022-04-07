# Clusterpedia
This name Clusterpedia is inspired by Wikipedia. It is an encyclopedia of multi-cluster to synchronize, search for, and simply control multi-cluster resources.

Clusterpedia can synchronize resources with multiple clusters and provide more powerful search features on the basis of compatibility with Kubernetes OpenAPI to help you effectively get any multi-cluster resource that you are looking for in a quick and easy way.

# Prerequisites
* [Insall Helm version 3 or later](https://helm.sh/docs/intro/install/)

# Install
## Install CRDs
clusterpedia requires CRD resources, which can be installed manually using kubectl, or using the `installCRDs` option when installing the Helm Chart
> This way references the [cert-manager](https://cert-manager.io/docs/installation/helm/)

**Option 1: install CRDs with `kubectl`**
```bash
$ kubectl apply -f ./_crds
```

**Option 2: install CRDs as part of the Helm release**
To automatically install and manage the CRDs as part of your Helm release, you must add the `--set installCRDs=true` flag to your Helm installation command.

Uncomment the relevant line in the next steps to enable this.


## Select a storage component
Clusterpedia uses sub charts to install [postgresql](https://github.com/bitnami/charts/tree/master/bitnami/postgresql) or [mysql](https://github.com/bitnami/charts/tree/master/bitnami/postgresql), and `postgresql` is installed by default.

If you need to select `mysql` as the storage component, then you need to add the `--set postgresql.enabled=false --set mysql.enabled=true` to you Helm installation command.

More configurations of the storage components can be found in [bitnami/postgresql](https://github.com/bitnami/charts/tree/master/bitnami/postgresql) and [bitnami/mysql](https://github.com/bitnami/charts/tree/master/bitnami/mysql).

### Create Local PV
This chart creates a local pv for the storage component, but you need to specify the node using the `persistenceMatchNode` option, eg. `--set persistenceMatchNode=master-1`.

If you don't need to create a local pv, add the `--set persistenceMatchNode=None` flag.

## Install Clusterpedia

```bash
$ helm install clusterpedia . \
  --namespace clusterpedia-system \
  --create-namespace \
  --set persistenceMatchNode={{ LOCAL_PV_NODE }} \
  # --set installCRDs=true
```

# Uninstall
Before continuing, ensure that all clusterpedia resources that have been created by users have been deleted.
You can check for any existing resources with the following command:
```bash
$ kubectl get pediaclusters
```
Once all these resources have been deleted you are ready to unisntall clusterpedia.

```bash
$ helm --namespace clusterpedia-system uinstall clusterpedia
```

If the CRDs is not managed through Helm, then you need to delete the crd manually:
```bash
$ kubectl delete -f ./_crds
```
