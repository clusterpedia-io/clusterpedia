package clustersynchro

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	clusterv1alpha2 "github.com/clusterpedia-io/api/cluster/v1alpha2"
)

func (synchro *ClusterSynchro) monitor() {
	klog.InfoS("cluster synchro monitor is running...", "cluster", synchro.name)
	defer klog.InfoS("cluster synchro monitor is stoped", "cluster", synchro.name)

	wait.JitterUntil(synchro.checkClusterHealthy, 5*time.Second, 0.5, true, synchro.closer)

	healthyCondition := metav1.Condition{
		Type:               clusterv1alpha2.ClusterHealthyCondition,
		Status:             metav1.ConditionUnknown,
		Reason:             clusterv1alpha2.ClusterMonitorStopReason,
		Message:            "cluster synchro is shutdown",
		LastTransitionTime: metav1.Now().Rfc3339Copy(),
	}
	if lastReadyCondition := synchro.healthyCondition.Load().(metav1.Condition); lastReadyCondition.Status == metav1.ConditionFalse {
		healthyCondition.Message = fmt.Sprintf("Last Condition Reason: %s, Message: %s", lastReadyCondition.Reason, lastReadyCondition.Message)
	}
	synchro.healthyCondition.Store(healthyCondition)
}

func (synchro *ClusterSynchro) checkClusterHealthy() {
	defer synchro.updateStatus()
	lastReadyCondition := synchro.healthyCondition.Load().(metav1.Condition)

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Second)
	defer cancel()
	if ready, err := synchro.healthChecker.Ready(ctx); !ready {
		// if the last status was not ConditionTrue, stop resource synchros
		if lastReadyCondition.Status != metav1.ConditionTrue {
			synchro.stopRunner()
		}

		condition := metav1.Condition{
			Type:    clusterv1alpha2.ClusterHealthyCondition,
			Status:  metav1.ConditionFalse,
			Reason:  clusterv1alpha2.ClusterUnhealthyReason,
			Message: "cluster health responded without ok",
		}
		if err != nil {
			condition.Reason = clusterv1alpha2.ClusterNotReachableReason
			condition.Message = err.Error()
		}

		if lastReadyCondition.Status != condition.Status || lastReadyCondition.Reason != condition.Reason || lastReadyCondition.Message != condition.Message {
			condition.LastTransitionTime = metav1.Now().Rfc3339Copy()
			synchro.healthyCondition.Store(condition)
		}
		return
	}

	synchro.startRunner()
	message := "cluster health responded with ok"
	if lastReadyCondition.Status == metav1.ConditionTrue && lastReadyCondition.Message == message {
		return
	}

	if _, err := synchro.dynamicDiscovery.GetAndFetchServerVersion(); err != nil {
		message = fmt.Sprintf("cluster health responded with ok, but get server version: %v", err)
	}

	if lastReadyCondition.Status == metav1.ConditionTrue && lastReadyCondition.Message == message {
		// the ready status and message has not been modified,
		// to reduce the cluster status updates, do not update the `synchro.healthyCondition`.
		return
	}

	condition := metav1.Condition{
		Type:               clusterv1alpha2.ClusterHealthyCondition,
		Status:             metav1.ConditionTrue,
		Reason:             clusterv1alpha2.ClusterHealthyReason,
		Message:            message,
		LastTransitionTime: metav1.Now().Rfc3339Copy(),
	}
	synchro.healthyCondition.Store(condition)
}

type healthChecker struct {
	client rest.Interface
}

func newHealthChecker(config *rest.Config) (*healthChecker, error) {
	client, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, err
	}
	return &healthChecker{
		client: client.RESTClient(),
	}, nil
}

// TODO(iceber): resolve for more detailed error
func (checker *healthChecker) Ready(ctx context.Context) (bool, error) {
	_, err := checker.client.Get().AbsPath("/readyz").DoRaw(ctx)
	if apierrors.IsNotFound(err) {
		_, err = checker.client.Get().AbsPath("/healthz").DoRaw(ctx)
	}
	return err == nil, err
}
