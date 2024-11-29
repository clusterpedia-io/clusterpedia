# Memory Storage Layer v2 (Alpha)
Due to significant updates and modifications required for the memory storage layer, version 2 (v2) has been introduced. Unless otherwise necessary, memory v1 will only be supported in Clusterpedia 0.x.

⚠️ The current memory v2 is in the alpha stage, has not undergone rigorous testing, and the foundational storage layer functionalities will be gradually improved and implemented.

This version draws inspiration from the [apiserver/storage/cacher](https://github.com/kubernetes/kubernetes/tree/master/staging/src/k8s.io/apiserver/pkg/storage/cacher) library but does not necessarily follow all its design principles.

### Major Changes Compared to v1
#### ResourceVersion Format Changes
In v1, the resource version used a base64 encoded JSON format to merge the resource versions of each cluster into the resource's resource version field.

In v2, the resource version format is `<prefix>.<increase int>.<original resource version>`:
* An incrementing integer is used to represent the sequential order of resources, for version operations during List and Watch.
* The original resource version is retained.
* The prefix is used to identify the validity of the incrementing integer when requests switch between instances.

For Watch requests, a JSON formatted `ListOptions.ResourceVersion = {"<cluster>": "<resource version>"}` is supported to maintain continuity of Watch requests when switching instances.
> The clusterpedia version of Informer is required to replace the k8s.io/client-go Informer.

#### Using a Dedicated Resource Synchro
Due to the unique nature of the memory storage layer, it is unnecessary to use the default resource synchronizer of ClusterSynchro to maintain the informer store.

Memory v2 will directly use the native k8s informer as the resource synchronizer. Memory v2 will act as the Store for the k8s Informer, saving data directly into storage, avoiding intermediate operations and memory usage.

#### Supporting Dual Data Source Updates
In addition to the resource synchronizer saving resources in memory v2, external active additions, deletions, and modifications of memory v2 resources are also supported.

This ensures consistency of requests when supporting write operations at the apiserver layer through dual-write operations.
