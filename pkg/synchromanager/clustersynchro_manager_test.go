package synchromanager

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"

	clusterv1alpha2 "github.com/clusterpedia-io/api/cluster/v1alpha2"
)

func TestUpdateClusterEnqueue(t *testing.T) {
	const sharding = "shard-a"
	newCluster := func(spec clusterv1alpha2.ClusterSpec, statusSharding *string) *clusterv1alpha2.PediaCluster {
		return &clusterv1alpha2.PediaCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       spec,
			Status:     clusterv1alpha2.ClusterStatus{ShardingName: statusSharding},
		}
	}
	spec := clusterv1alpha2.ClusterSpec{ShardingName: sharding, APIServer: "https://a"}

	tests := []struct {
		name     string
		older    *clusterv1alpha2.PediaCluster
		newer    *clusterv1alpha2.PediaCluster
		enqueued bool
	}{
		{
			name:     "same status sharding value with different pointers is ignored",
			older:    newCluster(spec, ptr.To(sharding)),
			newer:    newCluster(spec, ptr.To(sharding)),
			enqueued: false,
		},
		{
			name:     "status sharding nil on both sides is ignored",
			older:    newCluster(spec, nil),
			newer:    newCluster(spec, nil),
			enqueued: false,
		},
		{
			name:     "status sharding set from nil is enqueued",
			older:    newCluster(spec, nil),
			newer:    newCluster(spec, ptr.To(sharding)),
			enqueued: true,
		},
		{
			name:     "status sharding cleared is enqueued",
			older:    newCluster(spec, ptr.To(sharding)),
			newer:    newCluster(spec, nil),
			enqueued: true,
		},
		{
			name:     "spec change is enqueued",
			older:    newCluster(spec, ptr.To(sharding)),
			newer:    newCluster(clusterv1alpha2.ClusterSpec{ShardingName: sharding, APIServer: "https://b"}, ptr.To(sharding)),
			enqueued: true,
		},
		{
			name:  "deletion is enqueued",
			older: newCluster(spec, ptr.To(sharding)),
			newer: func() *clusterv1alpha2.PediaCluster {
				c := newCluster(spec, ptr.To(sharding))
				c.DeletionTimestamp = ptr.To(metav1.Now())
				return c
			}(),
			enqueued: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := &Manager{
				shardingName: sharding,
				queue:        workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
			}
			defer manager.queue.ShutDown()

			manager.updateCluster(tt.older, tt.newer)
			if tt.enqueued {
				assert.Equal(t, 1, manager.queue.Len())
			} else {
				assert.Equal(t, 0, manager.queue.Len())
			}
		})
	}
}

func TestItemExponentialFailureAndJitterSlowRateLimter(t *testing.T) {
	limiter := NewItemExponentialFailureAndJitterSlowRateLimter(1*time.Second, 5*time.Second, 10*time.Second, 1.0, 5)

	assert.Equal(t, 1*time.Second, limiter.When("one"))
	assert.Equal(t, 2*time.Second, limiter.When("one"))
	assert.Equal(t, 4*time.Second, limiter.When("one"))
	assert.Equal(t, 5*time.Second, limiter.When("one"))
	assert.Equal(t, 5*time.Second, limiter.When("one"))
	assert.GreaterOrEqual(t, limiter.When("one"), 10*time.Second)

	assert.Equal(t, 6, limiter.NumRequeues("one"))

	assert.Equal(t, 1*time.Second, limiter.When("two"))
	assert.Equal(t, 2*time.Second, limiter.When("two"))

	limiter.Forget("one")
	assert.Equal(t, 1*time.Second, limiter.When("one"))
	assert.Equal(t, 2*time.Second, limiter.When("one"))

	assert.Equal(t, 4*time.Second, limiter.When("two"))
}
