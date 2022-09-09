package watchcache

import (
	"encoding/base64"
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"
)

// ClusterResourceVersion holds the pediaCluster name and its latest resourceVersion
type ClusterResourceVersion struct {
	rvmap map[string]string
}

func NewClusterResourceVersion(cluster string) *ClusterResourceVersion {
	return &ClusterResourceVersion{
		rvmap: map[string]string{cluster: "0"},
	}
}

func NewClusterResourceVersionFromString(rv string) (*ClusterResourceVersion, error) {
	result := &ClusterResourceVersion{
		rvmap: map[string]string{},
	}
	if rv == "0" || rv == "" {
		return result, nil
	}

	decoded, err := base64.RawURLEncoding.DecodeString(rv)
	if err != nil {
		return nil, fmt.Errorf("base64 decode failed: %v", err)
	}

	err = json.Unmarshal(decoded, &result.rvmap)
	if err != nil {
		return nil, fmt.Errorf("json decode failed: %v", err)
	}

	return result, nil
}

// GetClusterResourceVersion return a base64 encode string of ClusterResourceVersion
func (crv *ClusterResourceVersion) GetClusterResourceVersion() string {
	bytes, err := json.Marshal(crv.rvmap)
	if err != nil {
		klog.Errorf("base64 encode failed: %v", err)
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(bytes)
}

// GetClusterResourceVersionFromEvent return a ClusterResourceVersion from watch event
func GetClusterResourceVersionFromEvent(event *watch.Event) (*ClusterResourceVersion, error) {
	accessor, err := meta.Accessor(event.Object)
	if err != nil {
		return nil, fmt.Errorf("unable to understand watch event %#v", event)
	}
	return NewClusterResourceVersionFromString(accessor.GetResourceVersion())
}

func (crv *ClusterResourceVersion) IsEqual(another *ClusterResourceVersion) bool {
	if len(crv.rvmap) != len(another.rvmap) {
		return false
	}
	for key, value := range crv.rvmap {
		if another.rvmap[key] != value {
			return false
		}
	}
	return true
}

func (crv *ClusterResourceVersion) IsEmpty() bool {
	return len(crv.rvmap) == 0
}

type ClusterResourceVersionSynchro struct {
	crv *ClusterResourceVersion

	sync.RWMutex
}

func NewClusterResourceVersionSynchro(cluster string) *ClusterResourceVersionSynchro {
	return &ClusterResourceVersionSynchro{
		crv: NewClusterResourceVersion(cluster),
	}
}

// UpdateClusterResourceVersion update the resourceVersion in ClusterResourceVersionSynchro to the latest
func (crvs *ClusterResourceVersionSynchro) UpdateClusterResourceVersion(obj runtime.Object, cluster string) (*ClusterResourceVersion, error) {
	crvs.Lock()
	defer crvs.Unlock()

	crv := crvs.crv
	accessor := meta.NewAccessor()
	rv, _ := accessor.ResourceVersion(obj)
	crv.rvmap[cluster] = rv

	bytes, err := json.Marshal(crv.rvmap)
	if err != nil {
		return nil, fmt.Errorf("base64 encode failed: %v", err)
	}

	version := base64.RawURLEncoding.EncodeToString(bytes)
	err = accessor.SetResourceVersion(obj, version)
	if err != nil {
		return nil, fmt.Errorf("set resourceVersion failed: %v, may be it's a clear watch cache order event", err)
	}

	return NewClusterResourceVersionFromString(version)
}

func (crvs *ClusterResourceVersionSynchro) SetClusterResourceVersion(clusterName string, resourceVersion string) {
	crvs.Lock()
	defer crvs.Unlock()

	if _, ok := crvs.crv.rvmap[clusterName]; !ok {
		crvs.crv.rvmap[clusterName] = resourceVersion
	}
}

func (crvs *ClusterResourceVersionSynchro) RemoveCluster(clusterName string) {
	crvs.Lock()
	defer crvs.Unlock()

	delete(crvs.crv.rvmap, clusterName)
}
