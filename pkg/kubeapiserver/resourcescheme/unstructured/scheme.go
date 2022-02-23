package unstructured

import (
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type Scheme struct{}

func NewScheme() *Scheme {
	return &Scheme{}
}

func (s *Scheme) New(kind schema.GroupVersionKind) (runtime.Object, error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(kind)
	return obj, nil
}

func (s *Scheme) Default(_ runtime.Object) {}

// ObjectKinds returns a slice of one element with the group,version,kind of the
// provided object, or an error if the object is not runtime.Unstructured or
// has no group,version,kind information. unversionedType will always be false
// because runtime.Unstructured object should always have group,version,kind
// information set.
//
// reference from
// https://github.com/kubernetes/apiextensions-apiserver/blob/b0680ddb99b88a5978a43fe4f2508dce81be1ec9/pkg/crdserverscheme/unstructured.go#L46
func (s *Scheme) ObjectKinds(obj runtime.Object) (gvks []schema.GroupVersionKind, unversionedType bool, err error) {
	if _, ok := obj.(runtime.Unstructured); !ok {
		return nil, false, runtime.NewNotRegisteredErrForType("unstructured.Scheme", reflect.TypeOf(obj))
	}

	gvk := obj.GetObjectKind().GroupVersionKind()
	if gvk.Kind == "" {
		return nil, false, runtime.NewMissingKindErr("object has no kind field ")
	}

	if gvk.Version == "" {
		return nil, false, runtime.NewMissingVersionErr("object has no apiVersion field")
	}
	return []schema.GroupVersionKind{gvk}, false, nil
}

// Recognizes does not delegate the Recognizes check, needs to be wrapped by
// the caller to check the specific gvk
func (s *Scheme) Recognizes(gvk schema.GroupVersionKind) bool {
	return false
}

func (s *Scheme) ConvertFieldLabel(gvk schema.GroupVersionKind, label, value string) (string, string, error) {
	return runtime.DefaultMetaV1FieldSelectorConversion(label, value)
}

// Convert reference from
// https://github.com/kubernetes/apiextensions-apiserver/blob/b0680ddb99b88a5978a43fe4f2508dce81be1ec9/pkg/apiserver/conversion/converter.go#L104
func (s *Scheme) Convert(in, out, context interface{}) error {
	obj, ok := in.(runtime.Object)
	if !ok {
		return fmt.Errorf("input type %T in not valid for object conversion", in)
	}
	return s.UnsafeConvert(obj.DeepCopyObject(), out, context)
}

func (s *Scheme) UnsafeConvert(in, out, context interface{}) error {
	unstructIn, ok := in.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("input type %T in not Valid for unstructed conversion to %T", in, out)
	}

	unstructOut, ok := out.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("out type %T in not Valid for unstructured conversion from %T", out, in)
	}

	fromGVK := unstructIn.GroupVersionKind()
	toGVK := unstructOut.GroupVersionKind()
	if fromGVK.GroupKind() != toGVK.GroupKind() {
		return fmt.Errorf("not supported to convert from %s to %s", fromGVK.GroupKind(), toGVK.GroupKind())
	}

	unstructOut.SetUnstructuredContent(unstructIn.UnstructuredContent())
	unstructOut.SetGroupVersionKind(toGVK)
	return nil
}

// ConvertToVersion converts in object to the given gvk in place and returns the same `in` object.
// The in object can be a single object or a UnstructuredList. CRD storage implementation creates an
// UnstructuredList with the request's GV, populates it from storage, then calls conversion to convert
// the individual items. This function assumes it never gets a v1.List.
func (s *Scheme) ConvertToVersion(in runtime.Object, target runtime.GroupVersioner) (runtime.Object, error) {
	return s.UnsafeConvertToVersion(in.DeepCopyObject(), target)
}

func (s *Scheme) UnsafeConvertToVersion(in runtime.Object, target runtime.GroupVersioner) (runtime.Object, error) {
	fromGVK := in.GetObjectKind().GroupVersionKind()

	toGVK, ok := target.KindForGroupVersionKinds([]schema.GroupVersionKind{fromGVK})
	if !ok {
		return nil, fmt.Errorf("%s is unstructured and is not suitable for converting to %q", fromGVK, target)
	}

	if fromGVK.GroupKind() != toGVK.GroupKind() {
		return nil, fmt.Errorf("not supported to convert from %s to %s", fromGVK.GroupKind(), toGVK.GroupKind())
	}

	if list, ok := in.(*unstructured.UnstructuredList); ok {
		for i := range list.Items {
			itemKind := list.Items[i].GroupVersionKind().Kind
			list.Items[i].SetGroupVersionKind(toGVK.GroupVersion().WithKind(itemKind))
		}
	}
	in.GetObjectKind().SetGroupVersionKind(toGVK)
	return in, nil
}

var _ runtime.ObjectCreater = &Scheme{}
var _ runtime.ObjectConvertor = &Scheme{}
var _ runtime.ObjectDefaulter = &Scheme{}
var _ runtime.ObjectTyper = &Scheme{}

// unsafeObjectConvertor implements ObjectConvertor using the unsafe conversion path.
type unsafeObjectConvertor struct {
	*Scheme
}

var _ runtime.ObjectConvertor = unsafeObjectConvertor{}

func (c unsafeObjectConvertor) Convert(in, out, context interface{}) error {
	return c.Scheme.UnsafeConvert(in, out, context)
}

func (c unsafeObjectConvertor) ConvertToVersion(in runtime.Object, outVersion runtime.GroupVersioner) (runtime.Object, error) {
	return c.Scheme.UnsafeConvertToVersion(in, outVersion)
}

func UnsafeObjectConvertor(scheme *Scheme) runtime.ObjectConvertor {
	return unsafeObjectConvertor{scheme}
}
