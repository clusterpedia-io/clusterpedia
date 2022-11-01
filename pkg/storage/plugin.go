package storage

import (
	"os"
	"path"
	"plugin"
)

func LoadPlugins(dir string) error {
	if dir == "" {
		return nil
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, f := range files {
		if _, err := plugin.Open(path.Join(dir, f.Name())); err != nil {
			return err
		}
	}
	return nil
}
