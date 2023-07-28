# Clusterpedia Roadmap
Currently, it is only a tentative roadmap and the specific schedule depends on the community needs.

> **About some features not added to Roadmap, we can discuss in [issues](https://github.com/clusterpedia-io/clusterpedia/issues).**

## 2023
* Expose resource metrices for multiple clusters like [kube-state-metrics](https://github.com/kubernetes/kube-state-metrics)
* Clusterpedia APIServer supports log query for pods
* MQTT-based Agent Resource Collection Model
* ClusterSynchro manager exposes cluster state and resource synchronization status related metrics
* Supports resource watching for multiple clusters

## Others
* Support **Graph Database** Storage Layer
* Support for requests with relevant resources
* Support for custom resource columns when accepting data in Table format
* Support for [more control over cluster resources](/README.md#complicated), such as watch/create/update/delete operations
* Build-in multi-cluster metrics server in the clusterpedia apiserver
* Provide multi-cluster metrics server via OpenTelemery in Agent mode
* The `Default Storage Layer` supports for Custom Collection Resource
* Support filter namespaces when sync resources [#272](https://github.com/clusterpedia-io/clusterpedia/issues/272)
