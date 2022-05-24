package discovery

import (
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/klog/v2"
)

func (c *DynamicDiscoveryManager) StorageVersion() version.Info {
	return c.version.Load().(version.Info)
}

func (c *DynamicDiscoveryManager) GetAndFetchServerVersion() (version.Info, error) {
	updated, err := c.fetchServerVersion()
	if err != nil {
		return version.Info{}, err
	}

	if updated {
		klog.InfoS("server version is updated", "cluster", c.name, "version", c.StorageVersion().GitCommit)

		go func() {
			_ = c.refetchAllGroups()
		}()
	}
	return c.version.Load().(version.Info), nil
}

func (c *DynamicDiscoveryManager) fetchServerVersion() (bool, error) {
	serverVersion, err := c.discovery.ServerVersion()
	if err != nil {
		return false, err
	}

	oldVersion := c.version.Swap(*serverVersion).(version.Info)
	return oldVersion.GitCommit != serverVersion.GitCommit, nil
}
