package options

import (
	"fmt"
	"os"

	"github.com/spf13/pflag"

	_ "github.com/clusterpedia-io/clusterpedia/pkg/storage/internalstorage"
	_ "github.com/clusterpedia-io/clusterpedia/pkg/storage/memorystorage"
)

type StorageOptions struct {
	Name       string
	ConfigPath string
}

func NewStorageOptions() *StorageOptions {
	return &StorageOptions{Name: "internal"}
}

func (o *StorageOptions) Validate() []error {
	if o == nil {
		return nil
	}

	if o.ConfigPath == "" {
		return nil
	}

	var errors []error
	if info, err := os.Stat(o.ConfigPath); err != nil {
		errors = append(errors, err)
	} else if info.IsDir() {
		errors = append(errors, fmt.Errorf("%s is a directory", o.ConfigPath))
	}
	return errors
}

func (o *StorageOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.Name, "storage-name", o.Name, "storage name")
	fs.StringVar(&o.ConfigPath, "storage-config", o.ConfigPath, "storage config path")
}
