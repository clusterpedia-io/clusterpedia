package synchromanager

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	metricsstore "k8s.io/kube-state-metrics/v2/pkg/metrics_store"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	clusterv1alpha2 "github.com/clusterpedia-io/api/cluster/v1alpha2"
	"github.com/clusterpedia-io/clusterpedia/pkg/controller"
	crdclientset "github.com/clusterpedia-io/clusterpedia/pkg/generated/clientset/versioned"
	"github.com/clusterpedia-io/clusterpedia/pkg/generated/informers/externalversions"
	clusterlister "github.com/clusterpedia-io/clusterpedia/pkg/generated/listers/cluster/v1alpha2"
	kubestatemetrics "github.com/clusterpedia-io/clusterpedia/pkg/kube_state_metrics"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/clustersynchro"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/features"
	clusterpediafeature "github.com/clusterpedia-io/clusterpedia/pkg/utils/feature"
)

const ClusterSynchroControllerFinalizer = "clusterpedia.io/cluster-synchro-controller"

const defaultRetryNum = 5

type Manager struct {
	runLock sync.Mutex
	stopCh  <-chan struct{}

	clusterpediaclient crdclientset.Interface
	informerFactory    externalversions.SharedInformerFactory

	shardingName               string
	queue                      workqueue.RateLimitingInterface
	storage                    storage.StorageFactory
	clusterlister              clusterlister.PediaClusterLister
	clusterSyncResourcesLister clusterlister.ClusterSyncResourcesLister
	clusterInformer            cache.SharedIndexInformer

	clusterSyncConfig clustersynchro.ClusterSyncConfig
	synchrolock       sync.RWMutex
	synchros          map[string]*clustersynchro.ClusterSynchro
	synchroWaitGroup  wait.Group
}

var _ kubestatemetrics.ClusterMetricsWriterListGetter = &Manager{}

func NewManager(client crdclientset.Interface, storage storage.StorageFactory, syncConfig clustersynchro.ClusterSyncConfig, shardingName string) *Manager {
	factory := externalversions.NewSharedInformerFactory(client, 0)
	clusterinformer := factory.Cluster().V1alpha2().PediaClusters()
	clusterSyncResourcesInformer := factory.Cluster().V1alpha2().ClusterSyncResources()

	manager := &Manager{
		informerFactory:    factory,
		clusterpediaclient: client,
		shardingName:       shardingName,

		storage:                    storage,
		clusterlister:              clusterinformer.Lister(),
		clusterInformer:            clusterinformer.Informer(),
		clusterSyncResourcesLister: clusterSyncResourcesInformer.Lister(),
		queue: workqueue.NewRateLimitingQueue(
			NewItemExponentialFailureAndJitterSlowRateLimter(2*time.Second, 15*time.Second, 1*time.Minute, 1.0, defaultRetryNum),
		),

		clusterSyncConfig: syncConfig,
		synchros:          make(map[string]*clustersynchro.ClusterSynchro),
	}

	if _, err := clusterinformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    manager.addCluster,
			UpdateFunc: manager.updateCluster,
			DeleteFunc: manager.deleteCluster,
		},
	); err != nil {
		klog.ErrorS(err, "error when adding event handler to informer")
	}

	if _, err := clusterSyncResourcesInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: manager.handleClusterSyncResources,
		UpdateFunc: func(oldObj, newObj interface{}) {
			manager.handleClusterSyncResources(newObj)
		},
		DeleteFunc: manager.handleClusterSyncResources,
	}); err != nil {
		klog.ErrorS(err, "error when adding event handler to informer")
	}

	return manager
}

func (manager *Manager) GetMetricsWriterList() map[string]metricsstore.MetricsWriterList {
	manager.synchrolock.RLock()
	defer manager.synchrolock.RUnlock()

	lists := make(map[string]metricsstore.MetricsWriterList, len(manager.synchros))
	for name, synchro := range manager.synchros {
		writers := synchro.GetMetricsWriterList()
		if len(writers) != 0 {
			lists[name] = writers
		}
	}
	return lists
}

func (manager *Manager) Run(workers int, stopCh <-chan struct{}) {
	manager.runLock.Lock()
	defer manager.runLock.Unlock()
	if manager.stopCh != nil {
		klog.Fatal("clustersynchro manager is already running...")
	}
	klog.Info("Start Informer Factory")

	// informerFactory should not be controlled by stopCh
	stopInformer := make(chan struct{})
	manager.informerFactory.Start(stopInformer)
	if !cache.WaitForCacheSync(stopCh, manager.clusterInformer.HasSynced) {
		klog.Fatal("clustersynchro manager: wait for informer factory failed")
	}

	manager.stopCh = stopCh

	klog.InfoS("Start Manager Cluster Worker", "workers", workers)
	var waitGroup sync.WaitGroup
	for i := 0; i < workers; i++ {
		waitGroup.Add(1)

		go func() {
			defer waitGroup.Done()
			wait.Until(manager.worker, time.Second, manager.stopCh)
		}()
	}

	<-manager.stopCh
	klog.Info("receive stop signal, stop...")

	manager.queue.ShutDown()
	waitGroup.Wait()

	klog.Info("wait for cluster synchros stop...")
	manager.synchroWaitGroup.Wait()
	klog.Info("cluster synchro manager stopped.")
}

func (manager *Manager) addCluster(obj interface{}) {
	manager.enqueue(obj)
}

func (manager *Manager) updateCluster(older, newer interface{}) {
	oldObj := older.(*clusterv1alpha2.PediaCluster)
	newObj := newer.(*clusterv1alpha2.PediaCluster)
	if newObj.DeletionTimestamp.IsZero() &&
		equality.Semantic.DeepEqual(oldObj.Spec, newObj.Spec) &&
		oldObj.Status.ShardingName == newObj.Status.ShardingName {
		return
	}

	manager.enqueue(newer)
}

func (manager *Manager) deleteCluster(obj interface{}) {
	manager.enqueue(obj)
}

func (manager *Manager) enqueue(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		return
	}

	if cluster, ok := obj.(*clusterv1alpha2.PediaCluster); ok {
		currentSharding := cluster.Status.ShardingName
		if cluster.Spec.ShardingName != manager.shardingName {
			if currentSharding == nil || *currentSharding != manager.shardingName {
				return
			}
		} else if currentSharding != nil && *currentSharding != manager.shardingName {
			return
		}
	}

	manager.queue.Add(key)
}

func (manager *Manager) handleClusterSyncResources(obj interface{}) {
	// ClusterSyncResources is cluster scoped resource, key is name
	refName, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		klog.ErrorS(err, "handle clustersyncresources failed")
		return
	}
	clusters, err := manager.clusterlister.List(labels.Everything())
	if err != nil {
		klog.ErrorS(err, "list clusters failed while handling clustersyncresources", "clustersyncresources", refName)
		return
	}
	for _, cluster := range clusters {
		if cluster.Spec.SyncResourcesRefName == refName {
			manager.enqueue(cluster)
		}
	}
}

func (manager *Manager) worker() {
	for manager.processNextCluster() {
		select {
		case <-manager.stopCh:
			return
		default:
		}
	}
}

func (manager *Manager) processNextCluster() (continued bool) {
	key, shutdown := manager.queue.Get()
	if shutdown {
		return false
	}
	defer manager.queue.Done(key)
	continued = true

	_, name, err := cache.SplitMetaNamespaceKey(key.(string))
	if err != nil {
		klog.Error(err)
		return
	}

	klog.InfoS("reconcile cluster", "cluster", name)
	cluster, err := manager.clusterlister.Get(name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			klog.InfoS("cluster has been deleted", "cluster", name)
			return
		}

		klog.ErrorS(err, "Failed to get cluster from cache", "cluster", name)
		return
	}

	cluster = cluster.DeepCopy()
	if result := manager.reconcileCluster(cluster); result.Requeue() {
		if num := manager.queue.NumRequeues(key); num < result.MaxRetryCount() {
			klog.V(3).InfoS("requeue cluster", "cluster", name, "num requeues", num+1)
			manager.queue.AddRateLimited(key)
			return
		}
		klog.V(2).Infof("Dropping cluster %q out of the queue: %v", key, err)
	}
	manager.queue.Forget(key)
	return
}

// if err returned is not nil, cluster will be requeued
func (manager *Manager) reconcileCluster(cluster *clusterv1alpha2.PediaCluster) controller.Result {
	if cluster.Status.ShardingName == nil && cluster.Spec.ShardingName != manager.shardingName {
		return controller.NoRequeueResult
	}

	if cluster.Status.ShardingName != nil && *cluster.Status.ShardingName != manager.shardingName {
		return controller.NoRequeueResult
	}
	// After the above filtering， The cluster will be in the following state：
	// 1. spec.sharding == manager.shardingName and status.sharding == nil
	// 2. spec.sharding == manager.shardingName and status != nil and status.sharding == manager.shardingName
	// 3. spec.sharding != manager.shardingName and status != nil and status.sharding == manager.shardingName
	if !cluster.DeletionTimestamp.IsZero() {
		klog.InfoS("remove cluster", "cluster", cluster.Name)
		if err := manager.removeCluster(cluster.Name); err != nil {
			klog.ErrorS(err, "Failed to remove cluster", cluster.Name)
			return controller.RequeueResult(defaultRetryNum)
		}

		if !controllerutil.ContainsFinalizer(cluster, ClusterSynchroControllerFinalizer) {
			return controller.NoRequeueResult
		}

		// remove finalizer
		controllerutil.RemoveFinalizer(cluster, ClusterSynchroControllerFinalizer)
		if _, err := manager.clusterpediaclient.ClusterV1alpha2().PediaClusters().Update(context.TODO(), cluster, metav1.UpdateOptions{}); err != nil {
			klog.ErrorS(err, "Failed to remove finalizer", "cluster", cluster.Name)
			return controller.RequeueResult(defaultRetryNum)
		}
		return controller.NoRequeueResult
	}

	// ensure finalizer
	if !controllerutil.ContainsFinalizer(cluster, ClusterSynchroControllerFinalizer) {
		controllerutil.AddFinalizer(cluster, ClusterSynchroControllerFinalizer)

		if _, err := manager.clusterpediaclient.ClusterV1alpha2().PediaClusters().Update(context.TODO(), cluster, metav1.UpdateOptions{}); err != nil {
			klog.ErrorS(err, "Failed to add finalizer", "cluster", cluster.Name)
			return controller.RequeueResult(defaultRetryNum)
		}
	}

	if cluster.Spec.ShardingName != manager.shardingName {
		// status.sharding == manager.shardingName
		manager.stopClusterSynchro(cluster.Name)

		if err := manager.UpdateClusterShardingStatus(context.TODO(), cluster.Name, nil); err != nil {
			klog.ErrorS(err, "Failed to remove cluster shardingName status", "cluster", cluster.Name)
			return controller.RequeueResult(defaultRetryNum)
		}

		return controller.NoRequeueResult
	}

	cluster.Status.ShardingName = &manager.shardingName

	manager.synchrolock.RLock()
	synchro := manager.synchros[cluster.Name]
	manager.synchrolock.RUnlock()

	config, err := buildClusterConfig(cluster)
	if err != nil {
		klog.ErrorS(err, "Failed to build cluster config", "cluster", cluster.Name)
		manager.UpdateClusterAPIServerAndValidatedCondition(cluster.Name, cluster.Spec.APIServer, synchro, clusterv1alpha2.InvalidConfigReason,
			"invalid cluster config: "+err.Error(), metav1.ConditionFalse)
		return controller.NoRequeueResult
	}

	var warnMsg string
	syncResources := cluster.Spec.SyncResources
	if refName := cluster.Spec.SyncResourcesRefName; refName != "" {
		if ref, err := manager.clusterSyncResourcesLister.Get(refName); err != nil {
			if !apierrors.IsNotFound(err) {
				klog.ErrorS(err, "Failed to get SyncResourcesRef of cluster", "cluster", cluster.Name, "SyncResourcesRef", refName)
				manager.UpdateClusterAPIServerAndValidatedCondition(cluster.Name, config.Host, synchro, clusterv1alpha2.InvalidSyncResourcesReason,
					fmt.Sprintf("Failed to get cluster sync resources of cluster: %v", err), metav1.ConditionFalse)
				return controller.RequeueResult(defaultRetryNum)
			}

			// TODO: Use more obvious method to let users know.
			klog.Warningf("cluster(%s)'s SyncResourcesRef is not found", cluster.Name)
			warnMsg = "Warning: sync resource ref is not found"
		} else {
			syncResources = append(syncResources, ref.Spec.SyncResources...)
		}
	}

	// if `AllowSyncAllResources` is not enabled, then check whether the all-resource wildcard is used
	if !clusterpediafeature.FeatureGate.Enabled(features.AllowSyncAllResources) {
		for _, groupResources := range syncResources {
			if groupResources.Group == "*" {
				// When using the all-resource wildcard without feature gate enabled,
				// it just updates the condition information and
				// does not stop it if cluster synchro is already running.
				//
				// If have better suggestions can be discussed in the https://github.com/clusterpedia-io/clusterpedia/issues.
				manager.UpdateClusterAPIServerAndValidatedCondition(cluster.Name, config.Host, synchro, clusterv1alpha2.InvalidSyncResourcesReason,
					"ClusterSynchro Manager's feature gate `AllowSyncAllResources` is not enabled, cannot use all-resources wildcard", metav1.ConditionFalse)
				return controller.NoRequeueResult
			}
		}
	}

	manager.UpdateClusterAPIServerAndValidatedCondition(cluster.Name, config.Host, synchro, clusterv1alpha2.ValidatedReason, warnMsg, metav1.ConditionTrue)

	// check cluster config
	if synchro != nil && !reflect.DeepEqual(synchro.RESTConfig, config) {
		klog.InfoS("cluster config is changed, rebuild cluster synchro", "cluster", cluster.Name)
		synchro.Shutdown(true)
		synchro = nil

		manager.synchrolock.Lock()
		manager.synchros[cluster.Name] = synchro
		manager.synchrolock.Unlock()
	}

	// create resource synchro
	if synchro == nil {
		synchro, err = clustersynchro.New(cluster.Name, config, manager.storage, manager, manager.clusterSyncConfig)
		if err != nil {
			_, forever := err.(clustersynchro.RetryableError)
			klog.ErrorS(err, "Failed to create cluster synchro", "cluster", cluster.Name)

			runningCondition := metav1.Condition{
				Type:    clusterv1alpha2.SynchroRunningCondition,
				Reason:  clusterv1alpha2.SynchroInitialFailedReason,
				Status:  metav1.ConditionFalse,
				Message: err.Error(),
			}
			healthyCondition := metav1.Condition{
				Type:    clusterv1alpha2.ClusterHealthyCondition,
				Reason:  clusterv1alpha2.ClusterMonitorStopReason,
				Status:  metav1.ConditionUnknown,
				Message: "wait cluster synchro",
			}
			clusterStatus := &clusterv1alpha2.ClusterStatus{Conditions: []metav1.Condition{runningCondition, healthyCondition}}
			if err := manager.UpdateClusterStatus(context.TODO(), cluster.Name, clusterStatus); err != nil {
				klog.ErrorS(err, "Failed to update cluster synchro running condition status", "cluster", cluster.Name)
				if forever {
					// if initial failed error is retryable, retry forever
					return controller.RequeueResult(math.MaxInt)
				}
				return controller.RequeueResult(defaultRetryNum)
			}

			if forever {
				return controller.RequeueResult(math.MaxInt)
			}
			return controller.NoRequeueResult
		}

		err = manager.storage.PrepareCluster(cluster.Name)
		if err != nil {
			klog.ErrorS(err, "Failed to prepare cluster", "cluster", cluster.Name)
			return controller.NoRequeueResult
		}

		if err := manager.UpdateClusterShardingStatus(context.TODO(), cluster.Name, &manager.shardingName); err != nil {
			klog.ErrorS(err, "Failed to update cluster shardingName status", "cluster", cluster.Name)
			return controller.RequeueResult(defaultRetryNum)
		}

		manager.synchroWaitGroup.StartWithChannel(manager.stopCh, synchro.Run)

		manager.synchrolock.Lock()
		manager.synchros[cluster.Name] = synchro
		manager.synchrolock.Unlock()
	}

	synchro.SetResources(syncResources, cluster.Spec.SyncAllCustomResources)
	return controller.NoRequeueResult
}

func (manager *Manager) stopClusterSynchro(name string) {
	manager.synchrolock.Lock()
	synchro := manager.synchros[name]
	delete(manager.synchros, name)
	manager.synchrolock.Unlock()

	if synchro != nil {
		synchro.Shutdown(true)
	}
}

func (manager *Manager) removeCluster(name string) error {
	manager.synchrolock.Lock()
	synchro := manager.synchros[name]
	delete(manager.synchros, name)
	manager.synchrolock.Unlock()

	if synchro != nil {
		// not update removed cluster status,
		// and ensure that no more data is being synchronized to the resource storage
		synchro.Shutdown(false)
	}

	// clean cluster from storage
	return manager.storage.CleanCluster(context.TODO(), name)
}

func (manager *Manager) UpdateClusterAPIServerAndValidatedCondition(name string, apiServerEndpoint string, synchro *clustersynchro.ClusterSynchro, reason, message string, status metav1.ConditionStatus) {
	validatedCondition := metav1.Condition{
		Type:    clusterv1alpha2.ValidatedCondition,
		Reason:  reason,
		Status:  status,
		Message: message,
	}
	if err := manager.updateClusterStatus(context.TODO(), name, func(clusterStatus *clusterv1alpha2.ClusterStatus) {
		// set cluster apiserver endpoint to cluster status
		if apiServerEndpoint != "" {
			clusterStatus.APIServer = apiServerEndpoint
		}

		meta.SetStatusCondition(&clusterStatus.Conditions, validatedCondition)
		if synchro != nil {
			return
		}

		// if the cluster synchro is nil, update SynchroRunning and ClusterHealthy conditions.
		runningCondition := meta.FindStatusCondition(clusterStatus.Conditions, clusterv1alpha2.SynchroRunningCondition)
		if validatedCondition.Status != metav1.ConditionTrue {
			condition := metav1.Condition{
				Type:    clusterv1alpha2.SynchroRunningCondition,
				Reason:  validatedCondition.Reason,
				Status:  metav1.ConditionFalse,
				Message: validatedCondition.Message,
			}
			meta.SetStatusCondition(&clusterStatus.Conditions, condition)
		} else if runningCondition == nil || runningCondition.Reason != clusterv1alpha2.SynchroInitialFailedReason {
			condition := metav1.Condition{
				Type:    clusterv1alpha2.SynchroRunningCondition,
				Reason:  clusterv1alpha2.SynchroWaitInitReason,
				Status:  metav1.ConditionFalse,
				Message: "pediacluster is validated",
			}
			meta.SetStatusCondition(&clusterStatus.Conditions, condition)
		}

		healthyCondition := metav1.Condition{
			Type:    clusterv1alpha2.ClusterHealthyCondition,
			Reason:  clusterv1alpha2.ClusterMonitorStopReason,
			Status:  metav1.ConditionUnknown,
			Message: "wait cluster synchro",
		}
		meta.SetStatusCondition(&clusterStatus.Conditions, healthyCondition)
	}); err != nil {
		klog.ErrorS(err, "Failed to update cluster validated condition status", "cluster", name, "condition", validatedCondition)
	}
}

func (manager *Manager) UpdateClusterStatus(ctx context.Context, name string, status *clusterv1alpha2.ClusterStatus) error {
	return manager.updateClusterStatus(ctx, name, func(clusterStatus *clusterv1alpha2.ClusterStatus) {
		if status.Version != "" {
			clusterStatus.Version = status.Version
		}
		if status.SyncResources != nil {
			clusterStatus.SyncResources = status.SyncResources
		}
		for _, condition := range status.Conditions {
			meta.SetStatusCondition(&clusterStatus.Conditions, condition)
		}
	})
}

func (manager *Manager) updateClusterStatus(ctx context.Context, name string, updateFunc func(status *clusterv1alpha2.ClusterStatus)) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cluster, err := manager.clusterlister.Get(name)
		if err != nil {
			return err
		}
		lastStatus := cluster.Status

		cluster = cluster.DeepCopy()
		updateFunc(&cluster.Status)

		// remove deprecated conditions
		meta.RemoveStatusCondition(&cluster.Status.Conditions, clusterv1alpha2.ClusterSynchroInitializedCondition)

		// TODO: need optimize?
		readyCondition := metav1.Condition{
			Type:    clusterv1alpha2.ReadyCondition,
			Status:  metav1.ConditionTrue,
			Reason:  clusterv1alpha2.ReadyReason,
			Message: "",
		}
		for _, condType := range []string{
			clusterv1alpha2.ValidatedCondition,
			clusterv1alpha2.SynchroRunningCondition,
			clusterv1alpha2.ClusterHealthyCondition,
		} {
			cond := meta.FindStatusCondition(cluster.Status.Conditions, condType)
			if cond != nil && cond.Status == metav1.ConditionTrue {
				continue
			}

			readyCondition.Status = metav1.ConditionFalse
			readyCondition.Reason = clusterv1alpha2.NotReadyReason
			if cond == nil {
				readyCondition.Message = fmt.Sprintf("%s condition is not found", condType)
			} else {
				readyCondition.Message = fmt.Sprintf("%s condition is %s, reason is %s", condType, cond.Status, cond.Reason)
			}
			break
		}
		meta.SetStatusCondition(&cluster.Status.Conditions, readyCondition)

		if equality.Semantic.DeepEqual(cluster.Status, lastStatus) {
			return nil
		}

		_, err = manager.clusterpediaclient.ClusterV1alpha2().PediaClusters().UpdateStatus(ctx, cluster, metav1.UpdateOptions{})
		if err == nil {
			klog.V(2).InfoS("Update Cluster Status", "cluster", cluster.Name, "conditions", cluster.Status.Conditions)
		}
		return err
	})
}

func (manager *Manager) UpdateClusterShardingStatus(ctx context.Context, name string, shardingName *string) error {
	return manager.updateClusterStatus(ctx, name, func(clusterStatus *clusterv1alpha2.ClusterStatus) {
		clusterStatus.ShardingName = shardingName
	})
}

func buildClusterConfig(cluster *clusterv1alpha2.PediaCluster) (*rest.Config, error) {
	if len(cluster.Spec.Kubeconfig) != 0 {
		clientconfig, err := clientcmd.NewClientConfigFromBytes(cluster.Spec.Kubeconfig)
		if err != nil {
			return nil, err
		}
		return clientconfig.ClientConfig()
	}

	if cluster.Spec.APIServer == "" {
		return nil, errors.New("Cluster APIServer Endpoint is required")
	}

	if len(cluster.Spec.TokenData) == 0 &&
		(len(cluster.Spec.CertData) == 0 || len(cluster.Spec.KeyData) == 0) {
		return nil, errors.New("Cluster APIServer's Token or Cert is required")
	}

	config := &rest.Config{
		Host: cluster.Spec.APIServer,
	}

	if len(cluster.Spec.CAData) != 0 {
		config.TLSClientConfig.CAData = cluster.Spec.CAData
	} else {
		config.TLSClientConfig.Insecure = true
	}

	if len(cluster.Spec.CertData) != 0 && len(cluster.Spec.KeyData) != 0 {
		config.TLSClientConfig.CertData = cluster.Spec.CertData
		config.TLSClientConfig.KeyData = cluster.Spec.KeyData
	}

	if len(cluster.Spec.TokenData) != 0 {
		config.BearerToken = string(cluster.Spec.TokenData)
	}
	return config, nil
}

type ItemExponentialFailureAndJitterSlowRateLimter struct {
	failuresLock sync.Mutex
	failures     map[interface{}]int

	maxFastAttempts int

	fastBaseDelay time.Duration
	fastMaxDelay  time.Duration

	slowBaseDelay time.Duration
	slowMaxFactor float64
}

func NewItemExponentialFailureAndJitterSlowRateLimter(fastBaseDelay, fastMaxDelay, slowBaseDeploy time.Duration, slowMaxFactor float64, maxFastAttempts int) workqueue.RateLimiter {
	if slowMaxFactor <= 0.0 {
		slowMaxFactor = 1.0
	}
	return &ItemExponentialFailureAndJitterSlowRateLimter{
		failures:        map[interface{}]int{},
		maxFastAttempts: maxFastAttempts,
		fastBaseDelay:   fastBaseDelay,
		fastMaxDelay:    fastMaxDelay,
		slowBaseDelay:   slowBaseDeploy,
		slowMaxFactor:   slowMaxFactor,
	}
}

func (r *ItemExponentialFailureAndJitterSlowRateLimter) When(item interface{}) time.Duration {
	r.failuresLock.Lock()
	defer r.failuresLock.Unlock()

	fastExp, num := r.failures[item], r.failures[item]+1
	r.failures[item] = num
	if num > r.maxFastAttempts {
		return r.slowBaseDelay + time.Duration(rand.Float64()*r.slowMaxFactor*float64(r.slowBaseDelay))
	}

	// The backoff is capped such that 'calculated' value never overflows.
	backoff := float64(r.fastBaseDelay.Nanoseconds()) * math.Pow(2, float64(fastExp))
	if backoff > math.MaxInt64 {
		return r.fastMaxDelay
	}

	calculated := time.Duration(backoff)
	if calculated > r.fastMaxDelay {
		return r.fastMaxDelay
	}
	return calculated
}

func (r *ItemExponentialFailureAndJitterSlowRateLimter) NumRequeues(item interface{}) int {
	r.failuresLock.Lock()
	defer r.failuresLock.Unlock()

	return r.failures[item]
}

func (r *ItemExponentialFailureAndJitterSlowRateLimter) Forget(item interface{}) {
	r.failuresLock.Lock()
	defer r.failuresLock.Unlock()

	delete(r.failures, item)
}
