package synchromanager

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	clustersv1alpha1 "github.com/clusterpedia-io/clusterpedia/pkg/apis/clusters/v1alpha1"
	crdclientset "github.com/clusterpedia-io/clusterpedia/pkg/generated/clientset/versioned"
	"github.com/clusterpedia-io/clusterpedia/pkg/generated/informers/externalversions"
	clusterlister "github.com/clusterpedia-io/clusterpedia/pkg/generated/listers/clusters/v1alpha1"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/clustersynchro"
)

const ClusterSynchroControllerFinalizer = "clusterpedia.io/cluster-synchro-controller"

type Manager struct {
	closeOnce sync.Once
	closer    chan struct{}

	kubeclient         clientset.Interface
	clusterpediaclient crdclientset.Interface
	informerFactory    externalversions.SharedInformerFactory

	queue           workqueue.RateLimitingInterface
	storage         storage.StorageFactory
	clusterlister   clusterlister.PediaClusterLister
	clusterInformer cache.SharedIndexInformer

	synchrolock sync.RWMutex
	synchros    map[string]*clustersynchro.ClusterSynchro
}

func NewManager(kubeclient clientset.Interface, client crdclientset.Interface, storage storage.StorageFactory) *Manager {
	factory := externalversions.NewSharedInformerFactory(client, 0)
	clusterinformer := factory.Clusters().V1alpha1().PediaClusters()

	manager := &Manager{
		closer: make(chan struct{}),

		informerFactory:    factory,
		kubeclient:         kubeclient,
		clusterpediaclient: client,

		storage:         storage,
		clusterlister:   clusterinformer.Lister(),
		clusterInformer: clusterinformer.Informer(),
		queue: workqueue.NewRateLimitingQueue(
			workqueue.NewItemExponentialFailureRateLimiter(2*time.Second, 5*time.Second),
		),

		synchros: make(map[string]*clustersynchro.ClusterSynchro),
	}

	clusterinformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    manager.addCluster,
			UpdateFunc: manager.updateCluster,
			DeleteFunc: manager.deleteCluster,
		},
	)

	return manager
}

func (manager *Manager) Run(workers int, stopCh <-chan struct{}) {
	klog.Info("Start Informer Factory")
	manager.informerFactory.Start(stopCh)
	if !cache.WaitForCacheSync(stopCh, manager.clusterInformer.HasSynced) {
		return
	}

	klog.InfoS("Start Manager Cluster Worker", "workers", workers)

	var waitGroup sync.WaitGroup
	for i := 0; i < workers; i++ {
		waitGroup.Add(1)

		go func() {
			defer waitGroup.Done()
			wait.Until(manager.worker, time.Second, stopCh)
		}()
	}
	// TODO(iceber): if stop synchro manager, need shutdown synchros

	<-stopCh
	klog.Info("receive stop signal, stop...")

	manager.queue.ShutDown()

	waitGroup.Wait()
	klog.Info("cluster synchro manager stoped.")
}

func (manager *Manager) addCluster(obj interface{}) {
	manager.enqueue(obj)
}

func (manager *Manager) updateCluster(older, newer interface{}) {
	oldObj := older.(*clustersv1alpha1.PediaCluster)
	newObj := newer.(*clustersv1alpha1.PediaCluster)
	if newObj.DeletionTimestamp.IsZero() && equality.Semantic.DeepEqual(oldObj.Spec, newObj.Spec) {
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

	manager.queue.Add(key)
}

func (manager *Manager) worker() {
	for manager.processNextCluster() {
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
	if err := manager.reconcileCluster(cluster); err != nil {
		klog.ErrorS(err, "Failed to reconcile cluster", "cluster", name, "num requeues", manager.queue.NumRequeues(key))
		manager.queue.AddRateLimited(key)
		return
	}
	manager.queue.Forget(key)
	return
}

// if err returned is not nil, cluster will be requeued
func (manager *Manager) reconcileCluster(cluster *clustersv1alpha1.PediaCluster) (err error) {
	if !cluster.DeletionTimestamp.IsZero() {
		klog.InfoS("remove cluster", "cluster", cluster.Name)
		if err := manager.removeCluster(cluster.Name); err != nil {
			klog.ErrorS(err, "Failed to remove cluster", cluster.Name)
			return err
		}

		if !controllerutil.ContainsFinalizer(cluster, ClusterSynchroControllerFinalizer) {
			return nil
		}

		// remove finalizer
		controllerutil.RemoveFinalizer(cluster, ClusterSynchroControllerFinalizer)
		if _, err := manager.clusterpediaclient.ClustersV1alpha1().PediaClusters().Update(context.TODO(), cluster, metav1.UpdateOptions{}); err != nil {
			klog.ErrorS(err, "Failed to remove finializer", "cluster", cluster.Name)
			return err
		}
		return nil
	}

	// ensure finalizer
	if !controllerutil.ContainsFinalizer(cluster, ClusterSynchroControllerFinalizer) {
		controllerutil.AddFinalizer(cluster, ClusterSynchroControllerFinalizer)
		cluster, err = manager.clusterpediaclient.ClustersV1alpha1().PediaClusters().Update(context.TODO(), cluster, metav1.UpdateOptions{})
		if err != nil {
			klog.ErrorS(err, "Failed to add finializer", "cluster", cluster.Name)
			return err
		}
	}

	config, err := buildClusterConfig(cluster)
	if err != nil {
		// TODO(iceber): update cluster status
		klog.ErrorS(err, "Failed to build cluster config", "cluster", cluster.Name)
		return nil
	}

	manager.synchrolock.RLock()
	synchro := manager.synchros[cluster.Name]
	manager.synchrolock.RUnlock()
	if synchro != nil && !reflect.DeepEqual(synchro.RESTConfig, config) {
		klog.InfoS("cluster config is changed, rebuild cluster synchro", "cluster", cluster.Name)

		synchro.Shutdown()
		synchro = nil

		// manager.cleanCluster(cluster.Name)
	}

	// create resource synchro
	if synchro == nil {
		// TODO(iceber): set the stop sign of the manager to cluster synchro
		synchro, err = clustersynchro.New(cluster.Name, config, manager.storage, manager)
		if err != nil {
			// TODO(iceber): update cluster status
			// There are many reasons why creating a cluster synchro can fail.
			// How do you gracefully handle different errors?

			klog.ErrorS(err, "Failed to create cluster synchro", "cluster", cluster.Name)
			// Not requeue
			return nil
		}
	}

	synchro.SetResources(cluster.Spec.Resources)

	manager.synchrolock.Lock()
	manager.synchros[cluster.Name] = synchro
	manager.synchrolock.Unlock()
	return nil
}

func (manager *Manager) removeCluster(name string) error {
	manager.synchrolock.Lock()
	synchro := manager.synchros[name]
	delete(manager.synchros, name)
	manager.synchrolock.Unlock()

	if synchro != nil {
		synchro.Shutdown()
	}
	return manager.cleanCluster(name)
}

func (manager *Manager) cleanCluster(name string) error {
	return manager.storage.CleanCluster(context.TODO(), name)
}

func (manager *Manager) UpdateClusterStatus(ctx context.Context, name string, status *clustersv1alpha1.ClusterStatus) error {
	cluster, err := manager.clusterlister.Get(name)
	if err != nil {
		return err
	}

	if equality.Semantic.DeepEqual(cluster.Status, status) {
		return nil
	}

	cluster = cluster.DeepCopy()
	cluster.Status = *status
	_, err = manager.clusterpediaclient.ClustersV1alpha1().PediaClusters().UpdateStatus(ctx, cluster, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	klog.V(2).InfoS("Update Cluster Status", "cluster", cluster.Name, "status", status.Conditions[0].Reason)
	return nil
}

func buildClusterConfig(cluster *clustersv1alpha1.PediaCluster) (*rest.Config, error) {
	if cluster.Spec.APIServerURL == "" {
		return nil, errors.New("Cluster APIServer Endpoint is required")
	}

	if cluster.Spec.TokenData == "" && cluster.Spec.CAData == "" {
		return nil, errors.New("Cluster APIServer's Token or CA is required")
	}

	config := &rest.Config{
		Host: cluster.Spec.APIServerURL,
	}

	if cluster.Spec.CAData != "" {
		ca, err := base64.StdEncoding.DecodeString(cluster.Spec.CAData)
		if err != nil {
			return nil, fmt.Errorf("Cluster CA is invalid: %v", err)
		}
		config.TLSClientConfig.CAData = ca

		if cluster.Spec.CertData != "" && cluster.Spec.KeyData != "" {
			cert, err := base64.StdEncoding.DecodeString(cluster.Spec.CertData)
			if err != nil {
				return nil, fmt.Errorf("Cluster Cert is invalid: %v", err)
			}
			key, err := base64.StdEncoding.DecodeString(cluster.Spec.KeyData)
			if err != nil {
				return nil, fmt.Errorf("Cluster Cert is invalid: %v", err)
			}

			config.TLSClientConfig.CertData = cert
			config.TLSClientConfig.KeyData = key
		}
	} else {
		config.TLSClientConfig.Insecure = true
	}

	if cluster.Spec.TokenData != "" {
		token, err := base64.StdEncoding.DecodeString(cluster.Spec.TokenData)
		if err != nil {
			return nil, fmt.Errorf("Cluster CA is invalid: %v", err)
		}

		config.BearerToken = string(token)
	}
	return config, nil
}
