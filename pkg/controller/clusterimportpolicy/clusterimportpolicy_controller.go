package clusterimportpolicy

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"math"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	policyv1alpha1 "github.com/clusterpedia-io/api/policy/v1alpha1"
	"github.com/clusterpedia-io/clusterpedia/pkg/controller"
	clientset "github.com/clusterpedia-io/clusterpedia/pkg/generated/clientset/versioned"
	"github.com/clusterpedia-io/clusterpedia/pkg/generated/clientset/versioned/scheme"
	policyinformers "github.com/clusterpedia-io/clusterpedia/pkg/generated/informers/externalversions/policy/v1alpha1"
	policylister "github.com/clusterpedia-io/clusterpedia/pkg/generated/listers/policy/v1alpha1"
)

const PolicyControllerFinalizer = "clusterpedia.io/cluster-import-policy-controller"

type Controller struct {
	runLock sync.Mutex
	stopCh  <-chan struct{}

	client     clientset.Interface
	restmapper meta.RESTMapper

	policyLister     policylister.ClusterImportPolicyLister
	lifecycleLister  policylister.PediaClusterLifecycleLister
	lifecycleIndexer cache.Indexer

	queue            workqueue.RateLimitingInterface
	dependentManager *controller.DependentResourceManager
}

func NewController(
	mapper meta.RESTMapper,
	client clientset.Interface,
	policyInformer policyinformers.ClusterImportPolicyInformer,
	lifecycleInformer policyinformers.PediaClusterLifecycleInformer,
	queue workqueue.RateLimitingInterface,
	dependentManager *controller.DependentResourceManager,
) (*Controller, error) {
	controller := &Controller{
		client:     client,
		restmapper: mapper,

		policyLister:     policyInformer.Lister(),
		lifecycleLister:  lifecycleInformer.Lister(),
		lifecycleIndexer: lifecycleInformer.Informer().GetIndexer(),

		queue:            queue,
		dependentManager: dependentManager,
	}

	if _, err := policyInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: controller.enqueue,
			UpdateFunc: func(older, newer interface{}) {
				oldObj := older.(*policyv1alpha1.ClusterImportPolicy)
				newObj := newer.(*policyv1alpha1.ClusterImportPolicy)
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

	if _, err := lifecycleInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    controller.addLifecycle,
			UpdateFunc: controller.updateLifecycle,
			DeleteFunc: controller.deleteLifecycle,
		},
	); err != nil {
		return nil, err
	}

	if err := lifecycleInformer.Informer().AddIndexers(
		cache.Indexers{ownerPolicyIndex: ownerPolicyIndexFunc},
	); err != nil {
		return nil, err
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

func (c *Controller) addLifecycle(obj interface{}) {
	c.enqueueByLifecycle(obj)
}

func (c *Controller) updateLifecycle(older, newer interface{}) {
	oldObj := older.(*policyv1alpha1.PediaClusterLifecycle)
	newObj := newer.(*policyv1alpha1.PediaClusterLifecycle)
	if equality.Semantic.DeepEqual(oldObj.OwnerReferences, newObj.OwnerReferences) &&
		equality.Semantic.DeepEqual(oldObj.Spec, newObj.Spec) {
		return
	}

	c.enqueueByLifecycle(older)
	c.enqueueByLifecycle(newer)
}

func (c *Controller) deleteLifecycle(obj interface{}) {
	if _, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		return
	}
	c.enqueueByLifecycle(obj)
}

func (c *Controller) enqueueByLifecycle(obj interface{}) {
	metaobj, err := meta.Accessor(obj)
	if err != nil {
		klog.Warningf("meta.Accessor lifecycle<%T> failed: %v", obj, err)
		return
	}

	ownerRef := metav1.GetControllerOfNoCopy(metaobj)
	if ownerRef == nil {
		klog.Warningf("lifecycle<%s>'s owner not found", metaobj.GetName())
		return
	}
	ownerGR, err := schema.ParseGroupVersion(ownerRef.APIVersion)
	if err != nil {
		klog.Warningf("parse lifecycle<%s>'s owner<%s>/<%s>  failed: %v", metaobj.GetName(), ownerRef.APIVersion, ownerRef.Name, err)
		return
	}
	if ownerGR.Group != policyv1alpha1.GroupName || ownerRef.Kind != "ClusterImportPolicy" {
		klog.Warningf("lifecycle's owner<%s> is <%s>/<%s>", metaobj.GetName(), ownerRef.APIVersion, ownerRef.Name)
		return
	}

	c.queue.Add(ownerRef.Name)
}

func (c *Controller) Run(workers int, stopCh <-chan struct{}) {
	c.runLock.Lock()
	defer c.runLock.Unlock()
	if c.stopCh != nil {
		return
	}

	klog.InfoS("ClusterImportPolicy Controller is running", "workers", workers)
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

	// ClusterImportPolicy is cluster scope, key == name
	_, name, err := cache.SplitMetaNamespaceKey(key.(string))
	if err != nil {
		klog.ErrorS(err, "failed to split policy key", "key", key)
		return
	}

	policy, err := c.policyLister.Get(name)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			klog.ErrorS(err, "failed to get policy from lister", "policy", name)
			return
		}
		c.dependentManager.RemovePolicy(name)
		return
	}

	policy = policy.DeepCopy()
	result := c.reconcilePolicy(policy)
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		newer, err := c.policyLister.Get(name)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		if equality.Semantic.DeepEqual(newer.Status, policy.Status) {
			return nil
		}

		newer = newer.DeepCopy()
		newer.Status = policy.Status
		_, err = c.client.PolicyV1alpha1().ClusterImportPolicies().UpdateStatus(context.TODO(), newer, metav1.UpdateOptions{})
		return err
	}); err != nil {
		klog.ErrorS(err, "failed to update policy status", "policy", policy.Name)
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

func (c *Controller) reconcilePolicy(policy *policyv1alpha1.ClusterImportPolicy) (result controller.Result) {
	if !policy.DeletionTimestamp.IsZero() {
		c.dependentManager.RemovePolicy(policy.Name)

		if !controllerutil.ContainsFinalizer(policy, PolicyControllerFinalizer) {
			return NoRequeueResult
		}

		// remove finalizer
		controllerutil.RemoveFinalizer(policy, PolicyControllerFinalizer)
		if _, err := c.client.PolicyV1alpha1().ClusterImportPolicies().Update(context.TODO(), policy, metav1.UpdateOptions{}); err != nil {
			klog.ErrorS(err, "failed to update policy for removing finalizer", "policy", policy.Name)
			return NoRequeueResult
		}
		return NoRequeueResult
	}

	// ensure finalizer
	if !controllerutil.ContainsFinalizer(policy, PolicyControllerFinalizer) {
		controllerutil.AddFinalizer(policy, PolicyControllerFinalizer)
		if _, err := c.client.PolicyV1alpha1().ClusterImportPolicies().Update(context.TODO(), policy, metav1.UpdateOptions{}); err != nil {
			klog.ErrorS(err, "failed to update policy for adding finalizer", "policy", policy.Name)
			return NoRequeueResult
		}
	}

	nameTmpl, err := policy.Spec.NameTemplate.Template()
	if err != nil {
		policy.Status.Conditions = []metav1.Condition{NewValidateCondition("FailedParseLifecycleName", err)}

		klog.ErrorS(err, "failed to parse policy's name template", "policy", policy.Name, "name template", policy.Spec.NameTemplate)
		return NoRequeueResult
	}

	var sourceSelectorTmpl *template.Template
	if policy.Spec.Source.SelectorTemplate != "" {
		sourceSelectorTmpl, err = policy.Spec.Source.SelectorTemplate.Template()
		if err != nil {
			policy.Status.Conditions = []metav1.Condition{NewValidateCondition("FailedParseSourceSelector", err)}

			klog.ErrorS(err, "failed to parse policy's source selector template", "policy", policy.Name, "source selector template", policy.Spec.Source.SelectorTemplate)
			return NoRequeueResult
		}
	}

	// validate policy
	if errors := policy.Spec.Policy.Validate(); len(errors) != 0 {
		policy.Status.Conditions = []metav1.Condition{NewValidateCondition("InvalaidPolicy", utilerrors.NewAggregate(errors))}

		klog.ErrorS(fmt.Errorf("invalid policy"), "failed to validate policy", "policy", policy.Name, "errors", errors)
		return NoRequeueResult
	}

	// negotiate gvr of the source resource
	sourceGR := policy.Spec.Source.GroupResource()
	sourceGVR, err := c.negotiateGVR(sourceGR, policy.Spec.Source.Versions)
	if err != nil {
		policy.Status.Conditions = []metav1.Condition{NewValidateCondition("FailedNegotiateSourceResource", err)}

		klog.ErrorS(err, "failed to negotiate source resource", "resource", sourceGR)
		return NoRequeueResult
	}

	// negotiate gvr of the reference resources
	references := make([]policyv1alpha1.ReferenceResourceTemplate, 0, len(policy.Spec.References))
	referenceGVRs := make(map[schema.GroupVersionResource]struct{}, len(policy.Spec.References))
	for i, ref := range policy.Spec.References {
		refGVR, err := c.negotiateGVR(ref.GroupResource(), ref.Versions)
		if err != nil {
			policy.Status.Conditions = []metav1.Condition{NewValidateCondition("FailedNegotiateReferenceResource", fmt.Errorf("[%d] %w", i, err))}

			klog.ErrorS(err, "failed to negotiate reference resource", "resource", sourceGR)
			return NoRequeueResult
		}

		referenceGVRs[refGVR] = struct{}{}
		references = append(references, policyv1alpha1.ReferenceResourceTemplate{
			BaseReferenceResourceTemplate: ref.BaseReferenceResourceTemplate,
			Version:                       refGVR.Version,
		})
	}
	_ = c.dependentManager.SetPolicyDependentGVRs(policy.Name, sourceGVR, referenceGVRs)

	meta.SetStatusCondition(&policy.Status.Conditions, NewValidateCondition("Success", nil))

	// check all dependent resources is synced
	if synced, err := c.dependentManager.HasSyncedPolicyDependentResources(policy.Name); err != nil {
		meta.SetStatusCondition(&policy.Status.Conditions, NewReconcilingCondition("FailedCheckResourceSync", err))
		return NoRequeueResult
	} else if !synced {
		meta.SetStatusCondition(&policy.Status.Conditions, NewReconcilingCondition("WaitResourceSynced", fmt.Errorf("the depent resource controllers haven't synced")))

		klog.V(2).InfoS("policy's dependent resource controllers haven't synced", "policy", policy.Name)
		return controller.RequeueResult(math.MaxInt)
	}

	objects, err := c.dependentManager.List(sourceGVR)
	if err != nil {
		meta.SetStatusCondition(&policy.Status.Conditions, NewReconcilingCondition("FailedListSourceResources", err))

		klog.ErrorS(err, "failed to list source resources", "policy", policy.Name, "source", sourceGVR)
		return NoRequeueResult
	}
	lifecycles, err := c.lifecycleIndexer.IndexKeys(ownerPolicyIndex, policy.Name)
	if err != nil {
		meta.SetStatusCondition(&policy.Status.Conditions, NewReconcilingCondition("FailedListLifecycles", err))

		klog.ErrorS(err, fmt.Sprintf("failed to list lifecycles from indexer with %s", ownerPolicyIndex), "policy", policy.Name)
		return NoRequeueResult
	}
	klog.InfoS("reconcile source resource and lifecycle", "policy", policy.Name, "source resources", len(objects))

	var writer bytes.Buffer
	var wouldDeletedLifecycles = sets.NewString(lifecycles...)
	var failedCount int
	for _, object := range objects {
		data := map[string]interface{}{"source": object.UnstructuredContent()}
		if sourceSelectorTmpl != nil {
			writer.Reset()
			if err := sourceSelectorTmpl.Execute(&writer, data); err != nil {
				failedCount++
				klog.ErrorS(err, "failed to select source", "policy", policy.Name, "source namespace", object.GetNamespace(), "source name", object.GetName())
				continue
			}
			if strings.TrimSpace(strings.ToLower(strings.ReplaceAll(writer.String(), "<no value>", ""))) != "true" {
				continue
			}
		}

		writer.Reset()
		if err := nameTmpl.Execute(&writer, data); err != nil {
			failedCount++
			klog.ErrorS(err, "failed to parse lifecycle name for source", "policy", policy.Name, "source namespace", object.GetNamespace(), "source name", object.GetName())
			continue
		}

		lifecycleName := strings.ReplaceAll(writer.String(), "<no value>", "")
		// TODO: validate lifecycle name
		wouldDeletedLifecycles.Delete(lifecycleName)

		objectSource := policyv1alpha1.DependentResource{Group: sourceGVR.Group, Version: sourceGVR.Version, Resource: sourceGVR.Resource,
			Namespace: object.GetNamespace(), Name: object.GetName(),
		}
		if err := c.createOrUpdateLifecycle(policy, lifecycleName, objectSource, references); err != nil {
			klog.ErrorS(err, "failed to handle lifecycle", "policy", policy.Name, "lifecycle", lifecycleName)
			failedCount++
		}
	}

	// remove the redundant lifecycles
	var failedDeletedCount int
	for lifecycleName := range wouldDeletedLifecycles {
		if err := c.client.PolicyV1alpha1().PediaClusterLifecycles().Delete(context.TODO(), lifecycleName, metav1.DeleteOptions{}); err != nil {
			klog.ErrorS(err, "failed to delete lifecycle", "policy", policy.Name, "lifecycle", lifecycleName)
			failedDeletedCount++
		}
	}

	if failedCount != 0 || failedDeletedCount != 0 {
		meta.SetStatusCondition(&policy.Status.Conditions, NewReconcilingCondition("FailedReconcile",
			fmt.Errorf("failed to create or update lifecycle: %d, failed to remove lifecycles: %d", failedCount, failedDeletedCount),
		))
	} else {
		meta.SetStatusCondition(&policy.Status.Conditions, NewReconcilingCondition("ReconcileSourceResourceAndLifecycle", nil))
	}

	return NoRequeueResult
}

func (c *Controller) createOrUpdateLifecycle(policy *policyv1alpha1.ClusterImportPolicy, name string, source policyv1alpha1.DependentResource, references []policyv1alpha1.ReferenceResourceTemplate) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		lifecycle, err := c.lifecycleLister.Get(name)
		if err != nil {
			if !apierrors.IsNotFound(err) {
				return err
			}

			lifecycle := &policyv1alpha1.PediaClusterLifecycle{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
				Spec: policyv1alpha1.PediaClusterLifecycleSpec{
					Source:     source,
					References: references,
					Policy:     policy.Spec.Policy,
				},
			}
			if err := controllerutil.SetControllerReference(policy, lifecycle, scheme.Scheme); err != nil {
				return fmt.Errorf("failed to set owner: %w", err)
			}

			if _, err := c.client.PolicyV1alpha1().PediaClusterLifecycles().Create(context.TODO(), lifecycle, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create lifecycle: %w", err)
			}
			return nil
		}

		if metav1.IsControlledBy(lifecycle, policy) &&
			equality.Semantic.DeepEqual(lifecycle.Spec.Source, source) &&
			equality.Semantic.DeepEqual(lifecycle.Spec.References, references) &&
			equality.Semantic.DeepEqual(lifecycle.Spec.Policy, policy.Spec.Policy) {
			return nil
		}

		lifecycle = lifecycle.DeepCopy()
		lifecycle.Spec.Source = source
		lifecycle.Spec.References = references
		lifecycle.Spec.Policy = policy.Spec.Policy
		if err := controllerutil.SetControllerReference(policy, lifecycle, scheme.Scheme); err != nil {
			return fmt.Errorf("failed to set owner: %w", err)
		}

		if _, err := c.client.PolicyV1alpha1().PediaClusterLifecycles().Update(context.TODO(), lifecycle, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("failed to update lifecycle: %w", err)
		}
		return nil
	})
}

func (c *Controller) negotiateGVR(gr schema.GroupResource, versions []string) (schema.GroupVersionResource, error) {
	// TODO(iceber): watch CRD and APIService
	gvrs, err := c.restmapper.ResourcesFor(gr.WithVersion(""))
	if err != nil {
		return schema.GroupVersionResource{}, err
	}

	if gr.Group == "" {
		filtered := make([]schema.GroupVersionResource, 0, len(gvrs))
		for _, gvr := range gvrs {
			if gvr.Group == "" {
				filtered = append(filtered, gvr)
			}
		}
		gvrs = filtered
	}
	if len(gvrs) == 0 {
		return schema.GroupVersionResource{}, fmt.Errorf("not found %s's version", gr)
	}

	if len(versions) == 0 {
		return gvrs[0], nil
	}

	for _, gvr := range gvrs {
		for _, version := range versions {
			if gvr.Version == version {
				return gvr, nil
			}
		}
	}
	return schema.GroupVersionResource{}, fmt.Errorf("expected versions(%v) are not match %v", versions, gvrs)
}

func NewValidateCondition(reason string, err error) (cond metav1.Condition) {
	cond.Type = policyv1alpha1.PolicyValidatedCondition
	cond.Reason = reason
	if err != nil {
		cond.Status = metav1.ConditionFalse
	} else {
		cond.Status = metav1.ConditionTrue
	}
	cond.LastTransitionTime = metav1.Now()
	return
}

func NewReconcilingCondition(reason string, err error) (cond metav1.Condition) {
	cond.Type = policyv1alpha1.PolicyReconcilingCondition
	cond.Reason = reason
	if err != nil {
		cond.Status = metav1.ConditionFalse
	} else {
		cond.Status = metav1.ConditionTrue
	}
	cond.LastTransitionTime = metav1.Now()
	return
}

const ownerPolicyIndex = "ownerpolicy"

var _ cache.IndexFunc = ownerPolicyIndexFunc

func ownerPolicyIndexFunc(obj interface{}) ([]string, error) {
	meta, err := meta.Accessor(obj)
	if err != nil {
		return []string{""}, fmt.Errorf("object has no meta: %v", err)
	}

	ref := metav1.GetControllerOfNoCopy(meta)
	if ref == nil {
		return []string{""}, nil
	}

	refGR, err := schema.ParseGroupVersion(ref.APIVersion)
	if err != nil {
		return []string{""}, fmt.Errorf("parse ref apisversion failed")
	}

	if refGR.Group != policyv1alpha1.GroupName || ref.Kind != "ClusterImportPolicy" {
		return []string{""}, fmt.Errorf("lifecycle not has controller")
	}

	return []string{ref.Name}, nil
}
