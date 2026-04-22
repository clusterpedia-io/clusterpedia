package v1beta1

import (
	internal "github.com/clusterpedia-io/api/clusterpedia"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

var SchemeGroupVersion = schema.GroupVersion{Group: internal.GroupName, Version: "v1beta1"}

var (
	SchemeBuilder      runtime.SchemeBuilder
	localSchemeBuilder = &SchemeBuilder
	Install            = localSchemeBuilder.AddToScheme
)

func init() {
	localSchemeBuilder.Register(addKnownTypes)
}

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&CollectionResource{},
		&CollectionResourceList{},
		&Resources{},
		&ListOptions{},

		&metav1.GetOptions{},
		&metav1.DeleteOptions{},
		&metav1.CreateOptions{},
		&metav1.UpdateOptions{},
		&metav1.PatchOptions{},
	)

	scheme.AddKnownTypeWithName(SchemeGroupVersion.WithKind(metav1.WatchEventKind), &metav1.WatchEvent{})
	scheme.AddKnownTypeWithName(
		schema.GroupVersion{Group: SchemeGroupVersion.Group, Version: runtime.APIVersionInternal}.WithKind(metav1.WatchEventKind),
		&metav1.InternalEvent{},
	)

	scheme.AddUnversionedTypes(metav1.Unversioned,
		&metav1.Status{},
		&metav1.APIVersions{},
		&metav1.APIGroupList{},
		&metav1.APIGroup{},
		&metav1.APIResourceList{},
	)

	utilruntime.Must(metav1.RegisterConversions(scheme))
	utilruntime.Must(metav1.RegisterDefaults(scheme))

	//	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
	return nil
}
