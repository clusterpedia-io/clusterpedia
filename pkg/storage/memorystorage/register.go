package memorystorage

import (
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
)

const (
	StorageName = "memory"
)

func init() {
	storage.RegisterStorageFactoryFunc(StorageName, NewStorageFactory)
}

func NewStorageFactory(_ string) (storage.StorageFactory, error) {
	storageFactory := &StorageFactory{
		clusters: make(map[string]bool),
	}
	return storageFactory, nil
}
