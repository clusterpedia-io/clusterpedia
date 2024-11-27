package memorystorage

import (
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
)

const (
	StorageName = "memory.v2/alpha"
)

func init() {
	storage.RegisterStorageFactoryFunc(StorageName, NewStorageFactory)
}

func NewStorageFactory(_ string) (storage.StorageFactory, error) {
	storageFactory := &StorageFactory{}
	return storageFactory, nil
}
