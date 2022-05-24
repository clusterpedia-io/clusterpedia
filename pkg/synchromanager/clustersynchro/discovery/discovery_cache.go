package discovery

import (
	"reflect"
	"sort"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/discovery"

	clusterv1alpha2 "github.com/clusterpedia-io/api/cluster/v1alpha2"
)

type GroupVersionCache interface {
	SetGroupVersions(groupVersions map[string][]string, aggregatorGroups sets.String)
}

type CustomResourceCache interface {
	UpdateCustomResource(resource schema.GroupResource, apiResource metav1.APIResource, versions []string)
	RemoveCustomResource(resource schema.GroupResource)
}

func (c *DynamicDiscoveryManager) SetResourceMutationHandler(handler func()) {
	// TODO: race
	c.resourceMutationHandler = handler
}

func (c *DynamicDiscoveryManager) SetGroupVersions(groupVersions map[string][]string, aggregatorGroups sets.String) {
	var updated bool
	defer func() {
		if updated {
			c.resourceMutationHandler()
		}
	}()

	c.lock.Lock()
	if reflect.DeepEqual(c.groupVersions, groupVersions) {
		c.aggregatorGroups = aggregatorGroups
		c.lock.Unlock()
		return
	}

	defer c.lock.Unlock()

	deletedGroups := sets.NewString()
	changedGroups := sets.NewString()
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

func (c *DynamicDiscoveryManager) refetchGroupsLocked(groups sets.String) (updated bool, failedResources map[schema.GroupVersion]error) {
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
	resourceVersions := make(map[schema.GroupResource]sets.String)
	for groupVersion, apiResourceList := range resources {
		for _, apiResource := range apiResourceList.APIResources {
			if strings.Contains(apiResource.Name, "/") {
				continue
			}

			gvr := groupVersion.WithResource(apiResource.Name)
			apiResources[gvr] = apiResource
			if versions := resourceVersions[gvr.GroupResource()]; versions == nil {
				resourceVersions[gvr.GroupResource()] = sets.NewString(gvr.Version)
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
			c.resourceMutationHandler()
		}
	}()

	c.lock.Lock()
	defer c.lock.Unlock()

	updated, failedResources := c.refetchGroupsLocked(c.aggregatorGroups)
	return failedResources
}

func (c *DynamicDiscoveryManager) refetchAllGroups() map[schema.GroupVersion]error {
	var updated bool
	defer func() {
		if updated {
			c.resourceMutationHandler()
		}
	}()

	c.lock.Lock()
	defer c.lock.Unlock()
	updated, failedResources := c.refetchAllGroupsLocked()
	return failedResources
}

func (c *DynamicDiscoveryManager) refetchAllGroupsLocked() (bool, map[schema.GroupVersion]error) {
	groups := sets.String(make(map[string]sets.Empty, len(c.groupVersions)))
	for group := range c.groupVersions {
		// custom resources are controllerd by CustomResourceCache
		if c.customResourceGroups.Has(group) {
			continue
		}
		groups.Insert(group)
	}
	return c.refetchGroupsLocked(groups)
}

func (c *DynamicDiscoveryManager) UpdateCustomResource(resource schema.GroupResource, apiResource metav1.APIResource, versions []string) {
	var updated bool
	defer func() {
		if updated {
			c.resourceMutationHandler()
		}
	}()

	c.lock.Lock()
	defer c.lock.Unlock()

	if !c.customResourceGroups.Has(resource.Group) {
		updated = true
	}
	c.customResourceGroups.Insert(resource.Group)

	if c.updateResourceLocked(resource, apiResource, versions) {
		updated = true
	}
}

func (c *DynamicDiscoveryManager) RemoveCustomResource(resource schema.GroupResource) {
	defer c.resourceMutationHandler()

	c.lock.Lock()
	defer c.lock.Unlock()

	c.removeResourceLocked(resource)
	for gr := range c.resourceVersions {
		if gr.Group == resource.Group {
			return
		}
	}
	delete(c.customResourceGroups, resource.Group)
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

func (c *DynamicDiscoveryManager) GetAPIResourceAndVersions(resource schema.GroupResource) (*metav1.APIResource, []string) {
	c.lock.Lock()
	defer c.lock.Unlock()

	var refetchFunc func() (updated bool)
	if c.customResourceGroups.Has(resource.Group) {
		// not refresh custom resource
	} else if c.aggregatorGroups.Has(resource.Group) {
		refetchFunc = func() bool {
			// TODO(iceber): Add limit on fetching aggreegator groups
			// For example, the same group can only be fetched once in 3 minutes and will be fetched again after 3 minutes
			updated, _ := c.refetchGroupsLocked(sets.NewString(resource.Group))
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
	c.lock.RLock()
	defer c.lock.RUnlock()

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
			Versions:  c.resourceVersions[gr],
		}
		syncResources = append(syncResources, resource)
	}
	return syncResources
}

func (c *DynamicDiscoveryManager) AttachAllCustomResourcesToSyncResources(resources []clusterv1alpha2.ClusterGroupResources) []clusterv1alpha2.ClusterGroupResources {
	c.lock.RLock()
	defer c.lock.RUnlock()

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
			Versions:  c.resourceVersions[gr],
			Resources: []string{gr.Resource},
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

func HasListAndWatchVerbs(apiResource metav1.APIResource) bool {
	verbs := sets.NewString(apiResource.Verbs...)
	return verbs.HasAll("list", "watch")
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
