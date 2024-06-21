package discovery

import (
	"reflect"
	"sort"
	"strings"
	"sync"

	apiextensionshelpers "k8s.io/apiextensions-apiserver/pkg/apihelpers"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"
	apiregistrationv1api "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	apiregistrationv1helper "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1/helper"

	clusterv1alpha2 "github.com/clusterpedia-io/api/cluster/v1alpha2"
)

type ResourcesInterface interface {
	GetGroupType(group string) GroupType
	GetAPIResourceAndVersions(resource schema.GroupResource) (*metav1.APIResource, []string)

	GetAllResourcesAsSyncResources() []clusterv1alpha2.ClusterGroupResources
	GetGroupResourcesAsSyncResources(group string) *clusterv1alpha2.ClusterGroupResources

	AttachAllCustomResourcesToSyncResources(resources []clusterv1alpha2.ClusterGroupResources) []clusterv1alpha2.ClusterGroupResources
}

func (c *DynamicDiscoveryManager) enableMutationHandler() {
	c.enabledMutationHandler.Store(true)
}

func (c *DynamicDiscoveryManager) disableMutationHandler() {
	c.enabledMutationHandler.Store(false)
}

func (c *DynamicDiscoveryManager) doMutationHandler() {
	if c.enabledMutationHandler.Load() {
		c.resourceMutationHandler()
	} else {
		c.dirty.Store(true)
	}
}

func (c *DynamicDiscoveryManager) reconcileAPIServices(apiservices []*apiregistrationv1api.APIService) {
	groupVersions := make(map[string][]string)
	aggregatorGroups := sets.Set[string]{}

	apiServicesByGroup := apiregistrationv1helper.SortedByGroupAndVersion(apiservices)
	for _, groupServices := range apiServicesByGroup {
		apiServicesByGroup := apiregistrationv1helper.SortedByGroupAndVersion(groupServices)[0]
		group := apiServicesByGroup[0].Spec.Group

		var versions []string
		var isLocal, isAggregator bool
		for _, apiService := range apiServicesByGroup {
			if apiService.Spec.Service != nil {
				isAggregator = true
				if !apiregistrationv1helper.IsAPIServiceConditionTrue(apiService, apiregistrationv1api.Available) {
					// ignore unavailable aggregator version
					continue
				}
			} else {
				isLocal = true
			}
			versions = append(versions, apiService.Spec.Version)
		}

		if isLocal && isAggregator {
			// the versions of the group contain Local and Aggregator, ignoring such group
			continue
		}

		if len(versions) == 0 {
			continue
		}

		groupVersions[group] = versions
		if isAggregator {
			aggregatorGroups.Insert(group)
		}
	}

	// The kube-aggregator does not register `apiregistration.k8s.io` as an APIService resource, we need to add `apiregistration.k8s.io/v1` to the **groupVersions**
	groupVersions[apiregistrationv1api.SchemeGroupVersion.Group] = []string{apiregistrationv1api.SchemeGroupVersion.Version}

	// full update, ensuring version order
	c.setGroupVersions(groupVersions, aggregatorGroups)
}

func (c *DynamicDiscoveryManager) setGroupVersions(groupVersions map[string][]string, aggregatorGroups sets.Set[string]) {
	var updated bool
	defer func() {
		if updated {
			c.doMutationHandler()
		}
	}()

	c.cacheLock.Lock()
	defer c.cacheLock.Unlock()

	if reflect.DeepEqual(c.groupVersions, groupVersions) {
		c.aggregatorGroups = aggregatorGroups
		return
	}

	deletedGroups := sets.Set[string]{}
	changedGroups := sets.Set[string]{}
	for group := range groupVersions {
		changedGroups.Insert(group)
	}
	for group, versions := range c.groupVersions {
		curVersions := groupVersions[group]
		if len(curVersions) == 0 {
			deletedGroups.Insert(group)
			continue
		}

		if len(versions) == len(curVersions) && reflect.DeepEqual(versions, curVersions) {
			changedGroups.Delete(group)
		}
	}

	c.groupVersions = groupVersions
	c.aggregatorGroups = aggregatorGroups

	// The apiservices for the custom resource are deleted and recreated as the `kube-apiserver` is started,
	// which means that changes to the group information for the custom resource may only be temporary,
	// so the custom resource information is controlled by CustomResourceCache
	for group := range c.customResourceGroups {
		deletedGroups.Delete(group)
		changedGroups.Delete(group)
	}

	deletedResources := make(map[schema.GroupResource]struct{})
	for gr := range c.resourceVersions {
		if deletedGroups.Has(gr.Group) {
			deletedResources[gr] = struct{}{}
		}
	}

	for gr := range deletedResources {
		updated = true
		c.removeResourceLocked(gr)
	}

	if len(changedGroups) != 0 {
		if ok, _ := c.refetchGroupsLocked(changedGroups); ok {
			updated = true
		}
	}
}

func (c *DynamicDiscoveryManager) refetchGroupsLocked(groups sets.Set[string]) (updated bool, failedResources map[schema.GroupVersion]error) {
	if len(groups) == 0 {
		return false, nil
	}

	apiGroups := make([]metav1.APIGroup, 0, len(groups))
	for group := range groups {
		versions := c.groupVersions[group]
		apiVersions := make([]metav1.GroupVersionForDiscovery, 0, len(versions))
		for _, version := range versions {
			apiVersions = append(apiVersions, metav1.GroupVersionForDiscovery{
				GroupVersion: schema.GroupVersion{Group: group, Version: version}.String(),
				Version:      version,
			})
		}

		apiGroups = append(apiGroups, metav1.APIGroup{
			Name:     group,
			Versions: apiVersions,
		})
	}

	apiGroupList := &metav1.APIGroupList{Groups: apiGroups}
	resources, failedResources := fetchGroupVersionResources(c.discovery, apiGroupList)

	apiResources := make(map[schema.GroupVersionResource]metav1.APIResource)
	resourceVersions := make(map[schema.GroupResource]sets.Set[string])
	for groupVersion, apiResourceList := range resources {
		for _, apiResource := range apiResourceList.APIResources {
			if strings.Contains(apiResource.Name, "/") {
				continue
			}

			gvr := groupVersion.WithResource(apiResource.Name)
			apiResources[gvr] = apiResource
			if versions := resourceVersions[gvr.GroupResource()]; versions == nil {
				resourceVersions[gvr.GroupResource()] = sets.New(gvr.Version)
			} else {
				versions.Insert(gvr.Version)
			}
		}
	}

	// TODO(iceber): We may not be able to delete resources corresponding to `failed resources`
	// to prevent clustersynchro from frequently deleting and adding resources,
	// and should only delete the corresponding resource after the GroupVersion(APIService)
	// has been explicitly removed.

	deletedResources := make(map[schema.GroupResource]struct{})
	for gr := range c.resourceVersions {
		if !groups.Has(gr.Group) {
			continue
		}
		if versions := resourceVersions[gr]; len(versions) == 0 {
			deletedResources[gr] = struct{}{}
		}
	}

	for gr := range deletedResources {
		c.removeResourceLocked(gr)
	}

	for gr, versions := range resourceVersions {
		sortedVersions := make([]string, 0, len(versions))
		for _, version := range c.groupVersions[gr.Group] {
			if versions.Has(version) {
				sortedVersions = append(sortedVersions, version)
			}
		}
		apiResource := apiResources[gr.WithVersion(sortedVersions[0])]
		if c.updateResourceLocked(gr, apiResource, sortedVersions) {
			updated = true
		}
	}
	return len(deletedResources) != 0 || updated, failedResources
}

func (c *DynamicDiscoveryManager) refetchAggregatorGroups() map[schema.GroupVersion]error {
	var updated bool
	defer func() {
		if updated {
			c.doMutationHandler()
		}
	}()

	c.cacheLock.Lock()
	defer c.cacheLock.Unlock()

	updated, failedResources := c.refetchGroupsLocked(c.aggregatorGroups)
	return failedResources
}

func (c *DynamicDiscoveryManager) refetchAllGroups() map[schema.GroupVersion]error {
	var updated bool
	defer func() {
		if updated {
			c.doMutationHandler()
		}
	}()

	c.cacheLock.Lock()
	defer c.cacheLock.Unlock()
	updated, failedResources := c.refetchAllGroupsLocked()
	return failedResources
}

func (c *DynamicDiscoveryManager) refetchAllGroupsLocked() (bool, map[schema.GroupVersion]error) {
	groups := sets.Set[string](make(map[string]sets.Empty, len(c.groupVersions)))
	for group := range c.groupVersions {
		// custom resources are controllerd by CustomResourceCache
		if c.customResourceGroups.Has(group) {
			continue
		}
		groups.Insert(group)
	}
	return c.refetchGroupsLocked(groups)
}

func (c *DynamicDiscoveryManager) updateCustomResource(crd *apiextensionsv1.CustomResourceDefinition) {
	if !apiextensionshelpers.IsCRDConditionTrue(crd, apiextensionsv1.Established) {
		// keep the resource and versions already set
		return
	}

	groupResource := schema.GroupResource{Group: crd.Spec.Group, Resource: crd.Status.AcceptedNames.Plural}
	apiResource := metav1.APIResource{
		Name:         crd.Status.AcceptedNames.Plural,
		SingularName: crd.Status.AcceptedNames.Singular,
		Kind:         crd.Status.AcceptedNames.Kind,
		ShortNames:   crd.Spec.Names.ShortNames,
		Categories:   crd.Spec.Names.Categories,
		Verbs:        metav1.Verbs([]string{"get", "list", "watch"}),
		Namespaced:   crd.Spec.Scope == apiextensionsv1.NamespaceScoped,
	}

	versions := make([]string, 0, len(crd.Spec.Versions))
	for _, version := range crd.Spec.Versions {
		if version.Served {
			versions = append(versions, version.Name)
		}
	}
	sortVersionByKubeAwareVersion(versions)

	var updated bool
	defer func() {
		if updated {
			c.doMutationHandler()
		}
	}()

	c.cacheLock.Lock()
	defer c.cacheLock.Unlock()

	if !c.customResourceGroups.Has(groupResource.Group) {
		updated = true
	}
	c.customResourceGroups.Insert(groupResource.Group)

	if c.updateResourceLocked(groupResource, apiResource, versions) {
		updated = true
	}
}

func (c *DynamicDiscoveryManager) removeCustomResource(crd *apiextensionsv1.CustomResourceDefinition) {
	groupResource := schema.GroupResource{Group: crd.Spec.Group, Resource: crd.Status.AcceptedNames.Plural}
	defer c.doMutationHandler()

	c.cacheLock.Lock()
	defer c.cacheLock.Unlock()

	c.removeResourceLocked(groupResource)
	for gr := range c.resourceVersions {
		if gr.Group == groupResource.Group {
			return
		}
	}
	delete(c.customResourceGroups, groupResource.Group)
}

func (c *DynamicDiscoveryManager) updateResourceLocked(resource schema.GroupResource, apiResource metav1.APIResource, sortedVersions []string) bool {
	if len(sortedVersions) == 0 {
		c.removeResourceLocked(resource)
		return true
	}

	if apiResource.SingularName == "" {
		_, singular := meta.UnsafeGuessKindToResource(schema.GroupVersionKind{Group: resource.Group, Kind: apiResource.Kind})
		apiResource.SingularName = singular.Resource
	}

	if reflect.DeepEqual(c.apiResources[resource], apiResource) && reflect.DeepEqual(c.resourceVersions[resource], sortedVersions) {
		return false
	}

	c.resourceVersions[resource] = sortedVersions
	c.apiResources[resource] = apiResource

	singular := schema.GroupResource{Group: resource.Group, Resource: apiResource.SingularName}
	c.pluralToSingular[resource] = singular
	c.singularToPlural[singular] = resource
	return true
}

func (c *DynamicDiscoveryManager) removeResourceLocked(resource schema.GroupResource) {
	delete(c.resourceVersions, resource)
	delete(c.apiResources, resource)
	if singular := c.pluralToSingular[resource]; !singular.Empty() {
		delete(c.singularToPlural, singular)
	}
	delete(c.pluralToSingular, resource)
}

func (c *DynamicDiscoveryManager) getAPIResourceAndVersionsLocked(gr schema.GroupResource) (metav1.APIResource, []string) {
	// k8s.io/apimachinery/pkg/api/meta/restmapper.go#coerceResourceForMatching
	gr.Resource = strings.ToLower(gr.Resource)
	if plural := c.singularToPlural[gr]; !plural.Empty() {
		gr = plural
	}

	return c.apiResources[gr], c.resourceVersions[gr]
}

type GroupType int

const (
	KubeResource GroupType = iota
	CustomResource
	AggregatorResource
	UnknownResource
)

func (c *DynamicDiscoveryManager) GetGroupType(group string) GroupType {
	c.cacheLock.RLock()
	defer c.cacheLock.RUnlock()

	if c.customResourceGroups.Has(group) {
		return CustomResource
	} else if c.aggregatorGroups.Has(group) {
		return AggregatorResource
	} else if _, ok := c.groupVersions[group]; ok {
		return KubeResource
	}
	return UnknownResource
}

func (c *DynamicDiscoveryManager) GetAPIResourceAndVersions(resource schema.GroupResource) (*metav1.APIResource, []string) {
	c.cacheLock.Lock()
	defer c.cacheLock.Unlock()

	var refetchFunc func() (updated bool)
	if c.customResourceGroups.Has(resource.Group) {
		// not refresh custom resource
	} else if c.aggregatorGroups.Has(resource.Group) {
		refetchFunc = func() bool {
			// TODO(iceber): Add limit on fetching aggreegator groups
			// For example, the same group can only be fetched once in 3 minutes and will be fetched again after 3 minutes
			updated, _ := c.refetchGroupsLocked(sets.New(resource.Group))
			return updated
		}
	} else if _, ok := c.groupVersions[resource.Group]; ok {
		refetchFunc = func() bool {
			// TODO(iceber): Add limit on fetching server version
			updated, err := c.fetchServerVersion()
			if err != nil {
				utilruntime.HandleError(err)
			}
			if !updated {
				return false
			}

			updated, _ = c.refetchAllGroupsLocked()
			return updated
		}
	} else {
		return nil, nil
	}

	if apiResource, versions := c.getAPIResourceAndVersionsLocked(resource); len(versions) != 0 {
		return &apiResource, versions
	}

	if refetchFunc != nil && !refetchFunc() {
		return nil, nil
	}

	if apiResource, versions := c.getAPIResourceAndVersionsLocked(resource); len(versions) != 0 {
		return &apiResource, versions
	}
	return nil, nil
}

func (c *DynamicDiscoveryManager) GetAllResourcesAsSyncResources() []clusterv1alpha2.ClusterGroupResources {
	c.cacheLock.RLock()
	defer c.cacheLock.RUnlock()

	sortedResources := make([]schema.GroupResource, 0, len(c.resourceVersions))
	for gr := range c.resourceVersions {
		sortedResources = append(sortedResources, gr)
	}
	sortGroupResources(sortedResources)

	syncResources := make([]clusterv1alpha2.ClusterGroupResources, 0, len(sortedResources))
	for _, gr := range sortedResources {
		resource := clusterv1alpha2.ClusterGroupResources{
			Group:     gr.Group,
			Resources: []string{gr.Resource},
			Versions:  []string{"*"},
		}
		syncResources = append(syncResources, resource)
	}
	return syncResources
}

func (c *DynamicDiscoveryManager) GetGroupResourcesAsSyncResources(group string) *clusterv1alpha2.ClusterGroupResources {
	c.cacheLock.RLock()
	defer c.cacheLock.RUnlock()
	sortedResources := make([]schema.GroupResource, 0)
	for gr := range c.resourceVersions {
		if gr.Group == group {
			sortedResources = append(sortedResources, gr)
		}
	}
	if len(sortedResources) == 0 {
		return nil
	}

	// not set syncResources.Versions, the caller determines the version of the resource to be synchronized.
	syncResources := clusterv1alpha2.ClusterGroupResources{
		Group:     group,
		Resources: make([]string, 0, len(sortedResources)),
	}
	sortGroupResources(sortedResources)
	for _, gr := range sortedResources {
		syncResources.Resources = append(syncResources.Resources, gr.Resource)
	}
	return &syncResources
}

func (c *DynamicDiscoveryManager) AttachAllCustomResourcesToSyncResources(resources []clusterv1alpha2.ClusterGroupResources) []clusterv1alpha2.ClusterGroupResources {
	c.cacheLock.RLock()
	defer c.cacheLock.RUnlock()

	sortedCustomResources := make([]schema.GroupResource, 0, len(c.customResourceGroups))
	for gr := range c.resourceVersions {
		if c.customResourceGroups.Has(gr.Group) {
			sortedCustomResources = append(sortedCustomResources, gr)
		}
	}
	sortGroupResources(sortedCustomResources)

	syncResources := make([]clusterv1alpha2.ClusterGroupResources, 0, len(resources)+len(sortedCustomResources))
	for _, resource := range resources {
		if !c.customResourceGroups.Has(resource.Group) {
			syncResources = append(syncResources, resource)
		}
	}

	for _, gr := range sortedCustomResources {
		resource := clusterv1alpha2.ClusterGroupResources{
			Group:     gr.Group,
			Resources: []string{gr.Resource},
			Versions:  []string{"*"},
		}
		syncResources = append(syncResources, resource)
	}
	return syncResources
}

func sortGroupResources(resources []schema.GroupResource) {
	sort.Slice(resources, func(i, j int) bool {
		left, right := resources[i], resources[j]
		if left.Group == right.Group {
			return left.Resource < right.Resource
		}
		return left.Group < right.Group
	})
}

func sortVersionByKubeAwareVersion(versions []string) {
	sort.Slice(versions, func(i, j int) bool {
		return version.CompareKubeAwareVersionStrings(versions[i], versions[j]) > 0
	})
}

// fetchServerResourcesForGroupVersions uses the discovery client to fetch the resources for the specified groups in parallel.
// https://github.com/kubernetes/kubernetes/blob/cae22d8b8a78985f438c232357aa2b6c60d83f9b/staging/src/k8s.io/client-go/discovery/discovery_client.go#L335
func fetchGroupVersionResources(d discovery.DiscoveryInterface, apiGroups *metav1.APIGroupList) (map[schema.GroupVersion]*metav1.APIResourceList, map[schema.GroupVersion]error) {
	groupVersionResources := make(map[schema.GroupVersion]*metav1.APIResourceList)
	failedGroups := make(map[schema.GroupVersion]error)

	wg := &sync.WaitGroup{}
	resultLock := &sync.Mutex{}
	for _, apiGroup := range apiGroups.Groups {
		for _, version := range apiGroup.Versions {
			groupVersion := schema.GroupVersion{Group: apiGroup.Name, Version: version.Version}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer utilruntime.HandleCrash()

				apiResourceList, err := d.ServerResourcesForGroupVersion(groupVersion.String())

				// lock to record results
				resultLock.Lock()
				defer resultLock.Unlock()

				if err != nil {
					// TODO: maybe restrict this to NotFound errors
					failedGroups[groupVersion] = err
				}
				if apiResourceList != nil {
					// even in case of error, some fallback might have been returned
					groupVersionResources[groupVersion] = apiResourceList
				}
			}()
		}
	}
	wg.Wait()

	return groupVersionResources, failedGroups
}

func HasListAndWatchVerbs(apiResource metav1.APIResource) bool {
	verbs := sets.New(apiResource.Verbs...)
	return verbs.HasAll("list", "watch")
}
