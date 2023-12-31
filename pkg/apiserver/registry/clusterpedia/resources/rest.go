package resources

import (
	"context"
	"fmt"
	"net/http"
	"path"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	genericrequest "k8s.io/apiserver/pkg/endpoints/request"
	genericrest "k8s.io/apiserver/pkg/registry/rest"

	"github.com/clusterpedia-io/api/clusterpedia/v1beta1"
	"github.com/clusterpedia-io/clusterpedia/pkg/utils/request"
)

// REST implements RESTStorage for Resources API
type REST struct {
	server http.Handler
}

var _ genericrest.Scoper = &REST{}
var _ genericrest.Storage = &REST{}
var _ genericrest.Connecter = &REST{}
var _ genericrest.SingularNameProvider = &REST{}

// NewREST returns a RESTStorage object that will work against API services
func NewREST(resourceHandler http.Handler) *REST {
	return &REST{
		server: resourceHandler,
	}
}

// New implements rest.Storage
func (r *REST) New() runtime.Object {
	return &v1beta1.Resources{}
}

// Destroy implements rest.Storage
func (r *REST) Destroy() {
}

// NamespaceScoped returns false because Resources is not namespaced
func (r *REST) NamespaceScoped() bool {
	return false
}

// GetSingularName implements rest.SingularNameProvider interface
func (s *REST) GetSingularName() string {
	return "resources"
}

// ConnectMethods returns the list of HTTP methods handled by Connect
func (r *REST) ConnectMethods() []string {
	return []string{"GET"}
}

// NewConnectOptions returns an empty options object that will be used to pass options to the Connect method.
func (r *REST) NewConnectOptions() (runtime.Object, bool, string) {
	return nil, true, ""
}

// Connect returns an http.Handler that will handle the request/response for a given API invocation.
func (r *REST) Connect(ctx context.Context, prefixPath string, _ runtime.Object, responder genericrest.Responder) (http.Handler, error) {
	info, ok := genericrequest.RequestInfoFrom(ctx)
	if !ok {
		return nil, fmt.Errorf("missing RequestInfo")
	}

	return http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		// not copy request context
		req = req.Clone(req.Context())

		paths := []string{info.APIPrefix, info.APIGroup, info.APIVersion, info.Resource}
		if prefixPath == "clusters" {
			// match /resources/clusters/<cluster name>/*
			if len(info.Parts) < 4 {
				err := apierrors.NewNotFound(schema.GroupResource{}, "")
				err.ErrStatus.Message = "the server could not find the requested resource"
				responder.Error(err)
				return
			}

			clustername := info.Parts[2]
			paths = append(paths, "clusters", clustername)
			req = req.WithContext(request.WithClusterName(ctx, clustername))
		}

		serverPrefix := "/" + path.Join(paths...)
		http.StripPrefix(serverPrefix, r.server).ServeHTTP(writer, req)
	}), nil
}
