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

	pediav1alpha1 "github.com/clusterpedia-io/clusterpedia/pkg/apis/pedia/v1alpha1"
	"github.com/clusterpedia-io/clusterpedia/pkg/utils/request"
)

// REST implements RESTStorage for Resources API
type REST struct {
	server http.Handler
}

func (r *REST) NamespaceScoped() bool {
	return false
}

func (r *REST) New() runtime.Object {
	return &pediav1alpha1.Resources{}
}

func NewREST(resourceHandler http.Handler) *REST {
	return &REST{
		server: resourceHandler,
	}
}

func (r *REST) ConnectMethods() []string {
	return []string{"GET"}
}

func (r *REST) NewConnectOptions() (runtime.Object, bool, string) {
	return nil, true, ""
}

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
