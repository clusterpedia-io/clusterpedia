package clustersynchro

import (
	"context"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	clusterv1alpha2 "github.com/clusterpedia-io/api/cluster/v1alpha2"
)

func (synchro *ClusterSynchro) monitor() {
	klog.V(2).InfoS("Cluster Synchro Monitor Running...", "cluster", synchro.name)

	wait.JitterUntil(synchro.checkClusterHealthy, 5*time.Second, 0.5, false, synchro.closer)
}

func (synchro *ClusterSynchro) checkClusterHealthy() {
	defer synchro.updateStatus()
	lastReadyCondition := synchro.readyCondition.Load().(metav1.Condition)

	if ready, err := checkKubeHealthy(synchro.clusterclient); !ready {
		// if the last status was not ConditionTrue, stop resource synchros
		if lastReadyCondition.Status != metav1.ConditionTrue {
			synchro.stopResourceSynchro()
		}

		condition := metav1.Condition{
			Type:    clusterv1alpha2.ClusterReadyCondition,
			Status:  metav1.ConditionFalse,
			Reason:  "Unhealthy",
			Message: "cluster health responded without ok",
		}
		if err != nil {
			condition.Reason = "NotReachable"
			condition.Message = err.Error()
		}

		if lastReadyCondition.Status != condition.Status || lastReadyCondition.Reason != condition.Reason || lastReadyCondition.Message != condition.Message {
			condition.LastTransitionTime = metav1.Now().Rfc3339Copy()
			synchro.readyCondition.Store(condition)
		}
		return
	}

	synchro.startResourceSynchro()
	if lastReadyCondition.Status == metav1.ConditionTrue {
		// TODO: if lastReadyCondition.Message != "", need process
		return
	}

	condition := metav1.Condition{
		Type:               clusterv1alpha2.ClusterReadyCondition,
		Status:             metav1.ConditionTrue,
		Reason:             "Healthy",
		LastTransitionTime: metav1.Now().Rfc3339Copy(),
	}
	defer func() {
		synchro.readyCondition.Store(condition)
	}()

	version, err := synchro.clusterclient.Discovery().ServerVersion()
	if err == nil {
		synchro.version.Store(*version)
		return
	}

	condition.Message = err.Error()
	klog.ErrorS(err, "Failed to get cluster version", "cluster", synchro.name)
}

// TODO(iceber): resolve for more detailed error
func checkKubeHealthy(client kubernetes.Interface) (bool, error) {
	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Second)
	defer cancel()

	_, err := client.Discovery().RESTClient().Get().AbsPath("/readyz").DoRaw(ctx)
	if apierrors.IsNotFound(err) {
		_, err = client.Discovery().RESTClient().Get().AbsPath("/healthz").DoRaw(ctx)
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
