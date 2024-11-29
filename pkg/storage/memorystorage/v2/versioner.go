package memorystorage

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/util/rand"
)

var versioner atomic.Uint64

var prefix string

func init() {
	prefix = os.Getenv("MEMORY_STORAGE_RESOURCE_VERSION_PREFIX")
	if prefix == "" {
		prefix = rand.String(8)
	}
}

func ConvertResourceVersionToMemoryResourceVersion(rv string) (uint64, string) {
	indexRV := versioner.Add(1)
	return indexRV, fmt.Sprintf("%s.%d.%s", prefix, indexRV, rv)
}

func ConvertMemoryResourceVersionToResourceVersion(rv string) (uint64, string, error) {
	s := strings.Split(rv, ".")
	if len(s) != 3 {
		return 0, "", fmt.Errorf("invalid resource version: %s", s)
	}
	if s[0] != prefix {
		return 0, "", fmt.Errorf("invalid resource version, prefix is not match: %s", s)
	}
	i, err := strconv.ParseUint(s[1], 10, 0)
	if err != nil {
		return 0, "", fmt.Errorf("invalid resource version, %v: %s", err, s)
	}
	return i, s[2], nil
}

func ConvertToMemoryResourceVersionForList(indexRV uint64) string {
	return fmt.Sprintf("%s.%d", prefix, indexRV)
}

func ConvertMemoryResourceVersionToResourceVersionForList(rv string) (uint64, error) {
	s := strings.Split(rv, ".")
	if len(s) != 2 {
		return 0, fmt.Errorf("invalid resource version: %s", s)
	}
	if s[0] != prefix {
		return 0, fmt.Errorf("invalid resource version, prefix is not match: %s", s)
	}
	i, err := strconv.ParseUint(s[1], 10, 0)
	if err != nil {
		return 0, fmt.Errorf("invalid resource version, %v: %s", err, s)
	}
	return i, nil
}
