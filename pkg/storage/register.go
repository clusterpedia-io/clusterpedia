package storage

import "fmt"

type NewStorageFactoryFunc func(configPath string) (StorageFactory, error)

var storageFactoryFuncs = make(map[string]NewStorageFactoryFunc)

func RegisterStorageFactoryFunc(name string, f NewStorageFactoryFunc) {
	if _, ok := storageFactoryFuncs[name]; ok {
		panic(fmt.Sprintf("storage %s has been registered", name))
	}
	storageFactoryFuncs[name] = f
}

func NewStorageFactory(name, configPath string) (StorageFactory, error) {
	provider, ok := storageFactoryFuncs[name]
	if !ok {
		return nil, fmt.Errorf("storage %s is unregistered", name)
	}

	storagefactory, err := provider(configPath)
	if err != nil {
		return nil, fmt.Errorf("Failed to init storage: %w", err)
	}
	return storagefactory, nil
}
