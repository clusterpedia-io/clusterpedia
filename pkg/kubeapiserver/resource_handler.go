package kubeapiserver

import (
	"context"
	"fmt"
	"net/http"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/endpoints/handlers"
	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"
	genericrequest "k8s.io/apiserver/pkg/endpoints/request"
	registryrest "k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/warning"
	"k8s.io/klog/v2"

	clusterv1alpha2 "github.com/clusterpedia-io/api/cluster/v1alpha2"
	clusterlister "github.com/clusterpedia-io/clusterpedia/pkg/generated/listers/cluster/v1alpha2"
	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/discovery"
	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/resourcerest"
	"github.com/clusterpedia-io/clusterpedia/pkg/utils/request"
)

type ResourceHandler struct {
	minRequestTimeout time.Duration
	delegate          http.Handler

	rest          *RESTManager
	discovery     *discovery.DiscoveryManager
	clusterLister clusterlister.PediaClusterLister
}

func (r *ResourceHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	requestInfo, ok := genericrequest.RequestInfoFrom(req.Context())
	if !ok {
		responsewriters.ErrorNegotiated(
			apierrors.NewInternalError(fmt.Errorf("no RequestInfo found in the context")),
			Codecs, schema.GroupVersion{}, w, req,
		)
		return
	}

	// handle discovery request
	if !requestInfo.IsResourceRequest {
		r.discovery.ServeHTTP(w, req)
		return
	}

	gvr := schema.GroupVersionResource{Group: requestInfo.APIGroup, Version: requestInfo.APIVersion, Resource: requestInfo.Resource}

	var (
		cluster *clusterv1alpha2.PediaCluster
		err     error
	)
	// When clusterName not empty, first check cluster whether exist
	clusterName := request.ClusterNameValue(req.Context())
	if clusterName != "" {
		if cluster, err = r.clusterLister.Get(clusterName); err != nil {
			if !apierrors.IsNotFound(err) {
				klog.ErrorS(err, "Failed to handle resource request, not get cluster from cache", "cluster", clusterName, "resource", gvr)
				responsewriters.ErrorNegotiated(
					apierrors.NewInternalError(err),
					Codecs, gvr.GroupVersion(), w, req,
				)
				return
			}
			responsewriters.ErrorNegotiated(
				apierrors.NewBadRequest("the server could not find the requested cluster"),
				Codecs, gvr.GroupVersion(), w, req,
			)
			return
		}
	}
	if !r.discovery.ResourceEnabled(clusterName, gvr) {
		r.delegate.ServeHTTP(w, req)
		return
	}

	resource, reqScope, storage, existed := r.rest.GetResourceREST(gvr, requestInfo.Subresource)
	if !existed {
		// TODO(iceber): Add the specialized error for subresources
		err := fmt.Errorf("not found request scope or resource storage")
		klog.ErrorS(err, "Failed to handle resource request", "resource", gvr)
		responsewriters.ErrorNegotiated(
			apierrors.NewInternalError(err),
			Codecs, gvr.GroupVersion(), w, req,
		)
		return
	}

	if requestInfo.Namespace != "" && !resource.Namespaced {
		r.delegate.ServeHTTP(w, req)
		return
	}

	// Check the health of the cluster
	checkClusterAndWarning(req.Context(), cluster)

	switch storage := storage.(type) {
	case *resourcerest.RESTStorage:
		var handler http.Handler
		switch requestInfo.Verb {
		case "get":
			if clusterName == "" {
				responsewriters.ErrorNegotiated(
					apierrors.NewBadRequest("please specify the cluster name when using the resource name to get a specific resource."),
					Codecs, gvr.GroupVersion(), w, req,
				)
				return
			}
			handler = handlers.GetResource(storage, reqScope)
		case "list":
			handler = handlers.ListResource(storage, nil, reqScope, false, r.minRequestTimeout)
		case "watch":
			handler = handlers.ListResource(storage, storage, reqScope, true, r.minRequestTimeout)
		default:
			responsewriters.ErrorNegotiated(
				apierrors.NewMethodNotSupported(gvr.GroupResource(), requestInfo.Verb),
				Codecs, gvr.GroupVersion(), w, req,
			)
			return
		}
		handler.ServeHTTP(w, req)

	case registryrest.Connecter:
		var supported bool
		for _, method := range storage.ConnectMethods() {
			if req.Method == method {
				supported = true
			}
		}
		if !supported {
			responsewriters.ErrorNegotiated(
				apierrors.NewMethodNotSupported(gvr.GroupResource(), requestInfo.Verb),
				Codecs, gvr.GroupVersion(), w, req,
			)
			return
		}

		handlers.ConnectResource(storage, reqScope, nil, "", true).ServeHTTP(w, req)
	}
}

func checkClusterAndWarning(ctx context.Context, cluster *clusterv1alpha2.PediaCluster) {
	if cluster == nil {
		return
	}
	var msg string
	healthyCondition := meta.FindStatusCondition(cluster.Status.Conditions, clusterv1alpha2.ClusterHealthyCondition)
	switch {
	case healthyCondition == nil:
		msg = fmt.Sprintf("%s is not ready and the resources obtained may be inaccurate.", cluster.Name)
	case healthyCondition.Status != metav1.ConditionTrue:
		msg = fmt.Sprintf("%s is not ready and the resources obtained may be inaccurate, reason: %s", cluster.Name, healthyCondition.Reason)
	}
	/*
		TODO(scyda): Determine the synchronization status of a specific resource

		for _, resource := range c.Status.Resources {
		}
	*/

	if msg != "" {
		warning.AddWarning(ctx, "", msg)
	}
}
