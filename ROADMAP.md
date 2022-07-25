# Clusterpedia Roadmap
Currently, it is only a tentative roadmap and the specific schedule depends on the community needs.

> **About some features not added to Roadmap, we can discuss in [issues](https://github.com/clusterpedia-io/clusterpedia/issues).**

## Q1 2022
* Build the official website and provide documentation in English and Chinese
* Support for more complex field selector filtering [#40](https://github.com/clusterpedia-io/clusterpedia/issues/40)
* Support for searching resources by owner [#49](https://github.com/clusterpedia-io/clusterpedia/issues/49)
* Migrate and sync sub repo to a separate repo [#47](https://github.com/clusterpedia-io/clusterpedia/issues/47)
* `PediaCluster` supports authentication of cluster via kubeconfig
* Add CI workloads for pr and pull
* Deploying with helm

## Q2 2022
* E2E tests
* Support synchronization of all CRD resources [#111](https://github.com/clusterpedia-io/clusterpedia/issues/111)
* Use the template to configure the pediaCluster resource [#150](https://github.com/clusterpedia-io/clusterpedia/issues/150)
* The internalstorage allows passing a piece of SQL to support more flexible query requirements [#151](https://github.com/clusterpedia-io/clusterpedia/issues/151)

## Q3 2022
* Support for automatic discovery and synchronization (Create, Delete, Update cluster authentication information) of other resources that represent `Cluster`(other multi-cloud projects) to `PediaCluster` [#185](https://github.com/clusterpedia-io/clusterpedia/issues/185)
* Build-in multi-cluster metrics server in the clusterpedia apiserver
* The `Default Storage Layer` supports for Custom Collection Resource
* Support filter namespaces when sync resources [#272](https://github.com/clusterpedia-io/clusterpedia/issues/272)

## Q4 2022
* Add Agent Mode to collect the cluster resources.
* Support for custom resource columns when accepting data in Table format
* Provide multi-cluster metrics server via OpenTelemery in Agent mode

## Others
* Support **ES** and **Graph Database** Storage Layer
* Support for the plug-in storage layer
* Support for requests with relevant resources
* Support for [more control over cluster resources](/README.md#complicated), such as watch/create/update/delete operations
