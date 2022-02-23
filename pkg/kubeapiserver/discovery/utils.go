package discovery

import (
	"fmt"
	"sort"
	"strings"
	_ "unsafe"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/version"
	_ "k8s.io/apiserver/pkg/endpoints/discovery"
)

//go:linkname keepUnversioned k8s.io/apiserver/pkg/endpoints/discovery.keepUnversioned
func keepUnversioned(group string) bool

//go:linkname newStripVersionEncoder k8s.io/apiserver/pkg/endpoints/discovery.newStripVersionEncoder
func newStripVersionEncoder(e runtime.Encoder, s runtime.Serializer) runtime.Encoder

type stripVersionNegotiatedSerializer struct {
	runtime.NegotiatedSerializer
}

func (n stripVersionNegotiatedSerializer) EncoderForVersion(encoder runtime.Encoder, gv runtime.GroupVersioner) runtime.Encoder {
	serializer, ok := encoder.(runtime.Serializer)
	if !ok {
		// The stripVersionEncoder needs both an encoder and decoder, but is called from a context that doesn't have access to the
		// decoder. We do a best effort cast here (since this code path is only for backwards compatibility) to get access to the caller's
		// decoder.
		panic(fmt.Sprintf("Unable to extract serializer from %#v", encoder))
	}
	versioned := n.NegotiatedSerializer.EncoderForVersion(encoder, gv)
	return newStripVersionEncoder(versioned, serializer)
}

// splitPath returns the segments for a URL path.
func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return []string{}
	}
	return strings.Split(path, "/")
}

func sortGroupDiscoveryByKubeAwareVersion(gd []metav1.GroupVersionForDiscovery) {
	sort.Slice(gd, func(i, j int) bool {
		return version.CompareKubeAwareVersionStrings(gd[i].Version, gd[j].Version) > 0
	})
}

func sortAPIGroupByName(groups []metav1.APIGroup) {
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Name < groups[j].Name
	})
}
