package pediaclusterlifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	clusterv1alpha2 "github.com/clusterpedia-io/api/cluster/v1alpha2"
	policyv1alpha1 "github.com/clusterpedia-io/api/policy/v1alpha1"
	"github.com/clusterpedia-io/clusterpedia/pkg/controller"
	clientset "github.com/clusterpedia-io/clusterpedia/pkg/generated/clientset/versioned"
	"github.com/clusterpedia-io/clusterpedia/pkg/generated/clientset/versioned/scheme"
	clusterinformers "github.com/clusterpedia-io/clusterpedia/pkg/generated/informers/externalversions/cluster/v1alpha2"
	policyinformers "github.com/clusterpedia-io/clusterpedia/pkg/generated/informers/externalversions/policy/v1alpha1"
	clusterlister "github.com/clusterpedia-io/clusterpedia/pkg/generated/listers/cluster/v1alpha2"
	policylister "github.com/clusterpedia-io/clusterpedia/pkg/generated/listers/policy/v1alpha1"
)

const LifecycleControllerFinalizer = "clusterpedia.io/pediacluster-lifecycle-controller"

type Controller struct {
	runLock sync.Mutex
	stopCh  <-chan struct{}

	client clientset.Interface

	lifecycleLister    policylister.PediaClusterLifecycleLister
	pediaclusterLister clusterlister.PediaClusterLister

	queue               workqueue.RateLimitingInterface
	dependentManager    *controller.DependentResourceManager
	pediaClusterDecoder runtime.Decoder
}

func NewController(
	client clientset.Interface,
	lifecycleInformer policyinformers.PediaClusterLifecycleInformer,
	pediaclusterInformer clusterinformers.PediaClusterInformer,
	queue workqueue.RateLimitingInterface,
	dependentManager *controller.DependentResourceManager,
) (*Controller, error) {
	controller := &Controller{
		client: client,

		lifecycleLister:    lifecycleInformer.Lister(),
		pediaclusterLister: pediaclusterInformer.Lister(),

		queue:               queue,
		dependentManager:    dependentManager,
		pediaClusterDecoder: scheme.Codecs.UniversalDecoder(clusterv1alpha2.SchemeGroupVersion),
	}

	if _, err := lifecycleInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: controller.enqueue,
			UpdateFunc: func(older, newer interface{}) {
				oldObj := older.(*policyv1alpha1.PediaClusterLifecycle)
				newObj := newer.(*policyv1alpha1.PediaClusterLifecycle)
				if newObj.DeletionTimestamp.IsZero() && equality.Semantic.DeepEqual(oldObj.Spec, newObj.Spec) {
					return
				}

				controller.enqueue(newer)
			},

			DeleteFunc: controller.enqueue,
		},
	); err != nil {
		return nil, err
	}

	if _, err := pediaclusterInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: controller.enqueueByPediaCluster,
			UpdateFunc: func(older, newer interface{}) {
				oldObj := older.(*clusterv1alpha2.PediaCluster)
				newObj := newer.(*clusterv1alpha2.PediaCluster)
				if newObj.DeletionTimestamp.IsZero() &&
					oldObj.Spec.APIServer == newObj.Spec.APIServer &&
					bytes.Equal(oldObj.Spec.Kubeconfig, newObj.Spec.Kubeconfig) &&
					bytes.Equal(oldObj.Spec.CAData, newObj.Spec.CAData) &&
					bytes.Equal(oldObj.Spec.TokenData, newObj.Spec.TokenData) &&
					bytes.Equal(oldObj.Spec.CertData, newObj.Spec.CertData) &&
					bytes.Equal(oldObj.Spec.KeyData, newObj.Spec.KeyData) {
					return
				}

				controller.enqueueByPediaCluster(newer)
			},

			DeleteFunc: controller.enqueueByPediaCluster,
		},
	); err != nil {
		klog.ErrorS(err, "error when adding event handler to informer")
	}

	return controller, nil
}

func (c *Controller) enqueue(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		return
	}
	c.queue.Add(key)
}

func (c *Controller) enqueueByPediaCluster(obj interface{}) {
	// lifecycle.Name == pediacluster.Name
	if d, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		if d.Key != "" {
			c.queue.Add(d.Key)
		}
		return
	}

	metaobj, err := meta.Accessor(obj)
	if err != nil {
		klog.Warningf("meta.Accessor pediacluster<%T> failed: %v", obj, err)
		return
	}
	c.queue.Add(metaobj.GetName())
}

func (c *Controller) Run(workers int, stopCh <-chan struct{}) {
	c.runLock.Lock()
	defer c.runLock.Unlock()
	if c.stopCh != nil {
		return
	}

	klog.InfoS("PediaClusterLifecycle controller is running", "workers", workers)
	c.stopCh = stopCh

	var waitGroup sync.WaitGroup
	for i := 0; i < workers; i++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			c.worker()
		}()
	}

	<-c.stopCh

	c.queue.ShutDown()
	waitGroup.Wait()
}

func (c *Controller) worker() {
	for c.processNextCluster() {
		select {
		case <-c.stopCh:
			return
		default:
		}
	}
}

func (c *Controller) processNextCluster() (continued bool) {
	key, shutdown := c.queue.Get()
	if shutdown {
		return false
	}
	defer c.queue.Done(key)
	continued = true

	_, name, err := cache.SplitMetaNamespaceKey(key.(string))
	if err != nil {
		klog.ErrorS(err, "failed to split lifecycle key", "key", key)
		return
	}

	lifecycle, err := c.lifecycleLister.Get(name)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			klog.ErrorS(err, "failed to get lifecycle from lister", "lifecycle", name)
			return
		}
		c.dependentManager.RemoveLifecycle(name)
		return
	}

	lifecycle = lifecycle.DeepCopy()
	klog.InfoS("reconcile lifecycle", "lifecycle", lifecycle.Name, "retry", c.queue.NumRequeues(key))
	result := c.reconcileLifecycle(lifecycle)
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		newer, err := c.lifecycleLister.Get(name)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		if equality.Semantic.DeepEqual(newer.Status, lifecycle.Status) {
			return nil
		}

		newer = newer.DeepCopy()
		newer.Status = lifecycle.Status
		_, err = c.client.PolicyV1alpha1().PediaClusterLifecycles().UpdateStatus(context.TODO(), newer, metav1.UpdateOptions{})
		return err
	}); err != nil {
		klog.ErrorS(err, "failed to update lifecycle status", "lifecycle", lifecycle.Name)
	}

	if result.Requeue() {
		if c.queue.NumRequeues(key) <= result.MaxRetryCount() {
			c.queue.AddRateLimited(key)
		}
	} else {
		c.queue.Forget(key)
	}
	return
}

var NoRequeueResult = controller.NoRequeueResult

func (c *Controller) reconcileLifecycle(lifecycle *policyv1alpha1.PediaClusterLifecycle) controller.Result {
	if !lifecycle.DeletionTimestamp.IsZero() {
		c.dependentManager.RemoveLifecycle(lifecycle.Name)

		if !controllerutil.ContainsFinalizer(lifecycle, LifecycleControllerFinalizer) {
			return NoRequeueResult
		}

		// remove finalizer
		controllerutil.RemoveFinalizer(lifecycle, LifecycleControllerFinalizer)
		if _, err := c.client.PolicyV1alpha1().PediaClusterLifecycles().Update(context.TODO(), lifecycle, metav1.UpdateOptions{}); err != nil {
			klog.ErrorS(err, "failed to update lifecycle for removing finalizer", "lifecycle", lifecycle.Name)
			return NoRequeueResult
		}
		return NoRequeueResult
	}

	// ensure finalizer
	if !controllerutil.ContainsFinalizer(lifecycle, LifecycleControllerFinalizer) {
		controllerutil.AddFinalizer(lifecycle, LifecycleControllerFinalizer)

		if _, err := c.client.PolicyV1alpha1().PediaClusterLifecycles().Update(context.TODO(), lifecycle, metav1.UpdateOptions{}); err != nil {
			klog.ErrorS(err, "failed to update lifecycle for adding finalizer", "lifecycle", lifecycle.Name)
			return NoRequeueResult
		}
	}

	var condition *metav1.Condition
	current, err := c.pediaclusterLister.Get(lifecycle.Name)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			klog.ErrorS(err, "failed to get lifecycle", "lifecycle", lifecycle.Name)
			return NoRequeueResult
		}

		meta.RemoveStatusCondition(&lifecycle.Status.Conditions, policyv1alpha1.LifecycleUpdatingCondition)
		condition = meta.FindStatusCondition(lifecycle.Status.Conditions, policyv1alpha1.LifecycleCreatedCondition)
		if condition == nil {
			condition = &metav1.Condition{
				Type:   policyv1alpha1.LifecycleCreatedCondition,
				Status: metav1.ConditionFalse,
			}
		}
	} else {
		// pediacluster is existed
		if !meta.IsStatusConditionTrue(lifecycle.Status.Conditions, policyv1alpha1.LifecycleCreatedCondition) {
			meta.SetStatusCondition(&lifecycle.Status.Conditions, metav1.Condition{
				Type:   policyv1alpha1.LifecycleCreatedCondition,
				Reason: "PediaClusterExisted",
				Status: metav1.ConditionTrue,
			})
		}

		condition = meta.FindStatusCondition(lifecycle.Status.Conditions, policyv1alpha1.LifecycleUpdatingCondition)
		if condition == nil {
			condition = &metav1.Condition{
				Type:   policyv1alpha1.LifecycleUpdatingCondition,
				Status: metav1.ConditionFalse,
			}
		}
	}
	defer func() {
		meta.SetStatusCondition(&lifecycle.Status.Conditions, *condition)
	}()

	ref, err := c.checkOwnerController(lifecycle)
	if err != nil || ref == nil {
		condition.Reason = "OrphanLifecycle"
		condition.Message = err.Error()

		c.dependentManager.SetLifecycleDependentResources(lifecycle.Name, map[policyv1alpha1.DependentResource]struct{}{})
		return NoRequeueResult
	}

	synced, err := c.dependentManager.HasSyncedPolicyDependentResources(ref.Name)
	if err != nil {
		condition.Reason = "WaitDependentResourceSynced"
		condition.Message = err.Error()
		return NoRequeueResult
	}
	if !synced {
		condition.Reason = "WaitDependentResourceSynced"
		condition.Message = ""

		c.dependentManager.SetLifecycleDependentResources(lifecycle.Name, map[policyv1alpha1.DependentResource]struct{}{})
		return controller.RequeueResult(math.MaxInt)
	}

	source := policyv1alpha1.DependentResource{Group: lifecycle.Spec.Source.Group, Version: lifecycle.Spec.Source.Version, Resource: lifecycle.Spec.Source.Resource,
		Namespace: lifecycle.Spec.Source.Namespace, Name: lifecycle.Spec.Source.Name}
	sourceObj, err := c.dependentManager.Get(source)
	if err != nil {
		condition.Reason = "SourceResourceNotFound"
		condition.Message = err.Error()

		klog.ErrorS(err, "failed to get source resource", "lifecycle", lifecycle.Name, "source", source)
		return NoRequeueResult
	}

	referencesTemplateData := make(map[string]interface{}, len(lifecycle.Spec.References))
	templateData := map[string]interface{}{
		"source":     sourceObj.UnstructuredContent(),
		"references": referencesTemplateData,
	}

	var writer bytes.Buffer
	if reason, message := func() (reason, message string) {
		references := make([]policyv1alpha1.DependentResource, 0, len(lifecycle.Spec.References))
		dependents := make(map[policyv1alpha1.DependentResource]struct{}, len(references)+1)
		dependents[source] = struct{}{}
		defer func() {
			lifecycle.Status.References = references
			c.dependentManager.SetLifecycleDependentResources(lifecycle.Name, dependents)
		}()

		// resolve and get reference resource
		for _, ref := range lifecycle.Spec.References {
			reference, err := ref.Resolve(&writer, templateData)
			if err != nil {
				klog.ErrorS(err, "failed to resolve reference namespacce and name", "lifecycle", lifecycle.Name, "reference", ref)
				return "FailedReferenceResourceParse", fmt.Sprintf("failed to ressolve <%s> namespace and name: %v", ref, err)
			}
			if reference.Name == "" {
				klog.ErrorS(fmt.Errorf("reference resource name is empty"), "invalid reference resource", "lifecycle", lifecycle.Name, "reference", ref)
				return "FailedReferenceResourceParse", fmt.Sprintf("<%s> resource name is empty", ref)
			}

			dependents[reference] = struct{}{}
			references = append(references, reference)

			refObject, err := c.dependentManager.Get(reference)
			if err != nil {
				klog.ErrorS(err, "failed to get reference resource", "lifecycle", lifecycle.Name, "ref", ref)
				return "ReferenceResourceNotFound", fmt.Sprintf("<%v>: %v", reference, err)
			}

			referencesTemplateData[ref.Key] = refObject.UnstructuredContent()
		}
		return
	}(); reason != "" {
		condition.Reason, condition.Message = reason, message
		return NoRequeueResult
	}

	if current == nil {
		couldCreate, err := lifecycle.Spec.CouldCreate(&writer, templateData)
		if err != nil {
			condition.Reason = "WaitCreationCondition"
			condition.Message = err.Error()

			klog.ErrorS(err, "failed to check creation condition", "lifecycle", lifecycle.Name)
			return NoRequeueResult
		}
		if !couldCreate {
			condition.Reason = "WaitCreationCondition"
			condition.Message = ""
			return NoRequeueResult
		}

		pediacluster, err := c.resolveAndDecodePediaCluster(lifecycle, &writer, templateData)
		if err != nil {
			condition.Reason = "FailedResolveAndDecodePediaCluster"
			condition.Message = err.Error()

			klog.ErrorS(err, "failed to resolve and decode pediacluster", "lifecycle", lifecycle.Name)
			return NoRequeueResult
		}

		pediacluster.Name = lifecycle.Name
		if err := controllerutil.SetOwnerReference(lifecycle, pediacluster, scheme.Scheme); err != nil {
			condition.Reason = "FailedSetOwner"
			condition.Message = err.Error()

			klog.ErrorS(err, "failed to set owner reference", "lifecycle", lifecycle.Name)
			return NoRequeueResult
		}

		if _, err := c.client.ClusterV1alpha2().PediaClusters().Create(context.TODO(), pediacluster, metav1.CreateOptions{}); err != nil {
			condition.Reason = "CreationFailure"
			condition.Message = err.Error()

			klog.ErrorS(err, "failed to create pediacluster", "lifecycle", lifecycle.Name)
			return NoRequeueResult
		}

		condition.Reason = "PediaClusterCreated"
		condition.Message = ""
		condition.Status = metav1.ConditionTrue
		klog.InfoS("pediacluster is created", "lifecycle", lifecycle.Name, "pediacluster", pediacluster.Name)
		return NoRequeueResult
	}

	pediacluster, err := c.resolveAndDecodePediaCluster(lifecycle, &writer, templateData)
	if err != nil {
		condition.Reason = "FailedResolveAndDecodePediaCluster"
		condition.Message = err.Error()

		klog.ErrorS(err, "failed to resolve and decode pediacluster", "lifecycle", lifecycle.Name)
		return NoRequeueResult
	}

	if pediacluster.Spec.APIServer == current.Spec.APIServer &&
		bytes.Equal(pediacluster.Spec.Kubeconfig, current.Spec.Kubeconfig) &&
		bytes.Equal(pediacluster.Spec.CAData, current.Spec.CAData) &&
		bytes.Equal(pediacluster.Spec.TokenData, current.Spec.TokenData) &&
		bytes.Equal(pediacluster.Spec.CertData, current.Spec.CertData) &&
		bytes.Equal(pediacluster.Spec.KeyData, current.Spec.KeyData) {
		condition.Reason = "PediaClusterUpdated"
		condition.Message = ""
		return NoRequeueResult
	}

	// 抽象出 staging/api 中相关字段到单独结构中，避免字段修改或者结构更改导致不一致
	// 只修改认证字段
	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"apiserver":  pediacluster.Spec.APIServer,
			"caData":     pediacluster.Spec.CAData,
			"tokenData":  pediacluster.Spec.TokenData,
			"certData":   pediacluster.Spec.CertData,
			"keyData":    pediacluster.Spec.KeyData,
			"kubeconfig": pediacluster.Spec.Kubeconfig,
		},
	}
	bytes, err := json.Marshal(patch)
	if err != nil {
		condition.Reason = "FailedPatchPediaCluster"
		condition.Message = fmt.Sprintf("failed to marshal patch data: %v", err)

		klog.ErrorS(err, "failed to marshal patch data", "lifecycle", lifecycle.Name)
		return NoRequeueResult
	}

	if _, err := c.client.ClusterV1alpha2().PediaClusters().Patch(context.TODO(), current.Name, types.MergePatchType, bytes, metav1.PatchOptions{}); err != nil {
		condition.Reason = "FailedPatchPediaCluster"
		condition.Message = err.Error()

		klog.ErrorS(err, "failed to marshal patch data", "lifecycle", lifecycle.Name)
		return NoRequeueResult
	}

	condition.Reason = "PediaClusterUpdated"
	condition.Message = ""
	condition.Status = metav1.ConditionTrue
	klog.InfoS("pediacluster is updated", "lifecycle", lifecycle.Name, "pediacluster", current.Name)
	return NoRequeueResult
}

func (c *Controller) checkOwnerController(lifecycle *policyv1alpha1.PediaClusterLifecycle) (*metav1.OwnerReference, error) {
	ref := metav1.GetControllerOfNoCopy(lifecycle.GetObjectMeta())
	if ref == nil {
		return nil, errors.New("not found owner controller")
	}

	refGR, err := schema.ParseGroupVersion(ref.APIVersion)
	if err != nil {
		return nil, err
	}
	if refGR.Group != policyv1alpha1.GroupName || ref.Kind != "ClusterImportPolicy" {
		return nil, errors.New("owner controller is not policy.ClusterImportPolicy")
	}
	return ref, nil
}

func (c *Controller) resolveAndDecodePediaCluster(lifecycle *policyv1alpha1.PediaClusterLifecycle, writer *bytes.Buffer, templateData interface{}) (*clusterv1alpha2.PediaCluster, error) {
	clusterbytes, err := lifecycle.Spec.ResolvePediaCluster(writer, templateData)
	if err != nil {
		return nil, err
	}
	obj, _, err := c.pediaClusterDecoder.Decode(clusterbytes, nil, &clusterv1alpha2.PediaCluster{})
	if err != nil {
		return nil, err
	}
	return obj.(*clusterv1alpha2.PediaCluster), nil
}
