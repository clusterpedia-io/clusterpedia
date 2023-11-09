package components

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/validation/path"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	genericstorage "k8s.io/apiserver/pkg/storage"

	internal "github.com/clusterpedia-io/api/clusterpedia"
	clusterpediaV1beta1 "github.com/clusterpedia-io/api/clusterpedia/v1beta1"
)

// apiserver\pkg\registry\generic\registry\store.go:239
// NamespaceKeyRootFunc is the default function for constructing storage paths
// to resource directories enforcing namespace rules.
func NamespaceKeyRootFunc(ctx context.Context, prefix string) string {
	key := prefix
	ns, ok := genericapirequest.NamespaceFrom(ctx)
	if ok && len(ns) > 0 {
		key = key + "/" + ns
	}
	return key
}

// apiserver\pkg\registry\generic\registry\store.go:251
// NamespaceKeyFunc is the default function for constructing storage paths to
// a resource relative to the given prefix enforcing namespace rules. If the
// context does not contain a namespace, it errors.
func NamespaceKeyFunc(ctx context.Context, prefix string, name string) (string, error) {
	key := NamespaceKeyRootFunc(ctx, prefix)
	ns, ok := genericapirequest.NamespaceFrom(ctx)
	if !ok || len(ns) == 0 {
		return "", apierrors.NewBadRequest("Namespace parameter required.")
	}
	if len(name) == 0 {
		return "", apierrors.NewBadRequest("Name parameter required.")
	}
	if msgs := path.IsValidPathSegmentName(name); len(msgs) != 0 {
		return "", apierrors.NewBadRequest(fmt.Sprintf("Name parameter invalid: %q: %s", name, strings.Join(msgs, ";")))
	}
	key = key + "/" + name
	return key, nil
}

// NoNamespaceKeyFunc is the default function for constructing storage paths
// to a resource relative to the given prefix without a namespace.
func NoNamespaceKeyFunc(ctx context.Context, prefix string, name string) (string, error) {
	if len(name) == 0 {
		return "", apierrors.NewBadRequest("Name parameter required.")
	}
	if msgs := path.IsValidPathSegmentName(name); len(msgs) != 0 {
		return "", apierrors.NewBadRequest(fmt.Sprintf("Name parameter invalid: %q: %s", name, strings.Join(msgs, ";")))
	}
	key := prefix + "/" + name
	return key, nil
}

// hasPathPrefix returns true if the string matches pathPrefix exactly, or if is prefixed with pathPrefix at a path segment boundary
func hasPathPrefix(s, pathPrefix string) bool {
	// Short circuit if s doesn't contain the prefix at all
	if !strings.HasPrefix(s, pathPrefix) {
		return false
	}

	pathPrefixLength := len(pathPrefix)

	if len(s) == pathPrefixLength {
		// Exact match
		return true
	}
	if strings.HasSuffix(pathPrefix, "/") {
		// pathPrefix already ensured a path segment boundary
		return true
	}
	if s[pathPrefixLength:pathPrefixLength+1] == "/" {
		// The next character in s is a path segment boundary
		// Check this instead of normalizing pathPrefix to avoid allocating on every call
		return true
	}
	return false
}

func PredicateFunc(label labels.Selector, field fields.Selector, isNamespaced bool) genericstorage.SelectionPredicate {
	attrFunc := genericstorage.DefaultClusterScopedAttr
	if isNamespaced {
		attrFunc = genericstorage.DefaultNamespaceScopedAttr
	}
	return genericstorage.SelectionPredicate{
		Label:    label,
		Field:    field,
		GetAttrs: attrFunc,
	}
}

func filterWithAttrsFunction(key string, p genericstorage.SelectionPredicate) FilterWithAttrsFunc {
	filterFunc := func(objKey string, label labels.Set, field fields.Set) bool {
		if !hasPathPrefix(objKey, key) {
			return false
		}
		return p.MatchesObjectAttributes(label, field)
	}
	return filterFunc
}

// NewPredicateWatch implemetes watcher.interface enbled fieldSelector and labelSelector
func NewPredicateWatch(ctx context.Context, options *internal.ListOptions, gvk schema.GroupVersionKind, isNamespaced bool) (*MultiClusterWatcher, error) {
	resourceVersion := ""

	label := labels.Everything()
	if options != nil && options.LabelSelector != nil {
		label = options.LabelSelector
	}
	field := fields.Everything()
	if options != nil && options.EnhancedFieldSelector != nil {
		fieldSelectorPointer, err := clusterpediaV1beta1.Convert_EnhancedFieldSelector_To_FieldSelector(&options.EnhancedFieldSelector)
		if err != nil {
			return nil, err
		}
		field = *fieldSelectorPointer
	}

	predicate := PredicateFunc(label, field, isNamespaced)

	if options != nil {
		resourceVersion = options.ResourceVersion
		predicate.AllowWatchBookmarks = options.AllowWatchBookmarks
	}

	// ResourcePrefix must come from the underlying factory
	prefix := options.ResourcePrefix
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	if prefix == "/" {
		// return fmt.Errorf("store for %s has an invalid prefix %q", e.DefaultQualifiedResource.String(), opts.ResourcePrefix)
		return nil, fmt.Errorf("resource prefix an invalid prefix: /")
	}

	storageOpts := genericstorage.ListOptions{
		ResourceVersion: resourceVersion,
		Predicate:       predicate,
		Recursive:       true,
	}

	// KeyRootFunc returns the root etcd key for this resource; should not
	// include trailing "/".  This is used for operations that work on the
	// entire collection (listing and watching).
	//
	// KeyRootFunc and KeyFunc must be supplied together or not at all.
	var KeyRootFunc func(ctx context.Context) string

	// KeyFunc returns the key for a specific object in the collection.
	// KeyFunc is called for Create/Update/Get/Delete. Note that 'namespace'
	// can be gotten from ctx.
	//
	// KeyFunc and KeyRootFunc must be supplied together or not at all.
	var KeyFunc func(ctx context.Context, name string) (string, error)

	if isNamespaced {
		KeyRootFunc = func(ctx context.Context) string {
			return NamespaceKeyRootFunc(ctx, prefix)
		}
		KeyFunc = func(ctx context.Context, name string) (string, error) {
			return NamespaceKeyFunc(ctx, prefix, name)
		}
	} else {
		KeyRootFunc = func(ctx context.Context) string {
			return prefix
		}

		KeyFunc = func(ctx context.Context, name string) (string, error) {
			return NoNamespaceKeyFunc(ctx, prefix, name)
		}
	}

	key := KeyRootFunc(ctx)
	if name, ok := predicate.MatchesSingle(); ok {
		if k, err := KeyFunc(ctx, name); err == nil {
			key = k
			storageOpts.Recursive = false
		}
		// if we cannot extract a key based on the current context, the
		// optimization is skipped
	}

	// We adapt the store's keyFunc so that we can use it with the StorageDecorator
	// without making any assumptions about where objects are stored in etcd
	keyFunc := func(obj runtime.Object) (string, error) {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			return "", err
		}

		if isNamespaced {
			return KeyFunc(genericapirequest.WithNamespace(genericapirequest.NewContext(), accessor.GetNamespace()), accessor.GetName())
		}

		return KeyFunc(genericapirequest.NewContext(), accessor.GetName())
	}

	return NewMultiClusterWatcher(100, filterWithAttrsFunction(key, storageOpts.Predicate), keyFunc, gvk), nil
}
