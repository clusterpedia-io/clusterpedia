package synchromanager

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"

	clusterv1alpha2 "github.com/clusterpedia-io/api/cluster/v1alpha2"
	"github.com/clusterpedia-io/clusterpedia/pkg/generated/clientset/versioned/fake"
	clusterlister "github.com/clusterpedia-io/clusterpedia/pkg/generated/listers/cluster/v1alpha2"
)

const (
	testCluster = "cluster"
	me, other   = "shard-a", "shard-b"
)

var pediaClusterGVR = clusterv1alpha2.SchemeGroupVersion.WithResource("pediaclusters")

// newShardedCluster has no conditions, so the status writer would derive and write a Ready condition
// on any attempt; a zero-write assertion on it therefore proves the ownership gate, not an unchanged status.
func newShardedCluster(specSharding string, statusSharding *string) *clusterv1alpha2.PediaCluster {
	return &clusterv1alpha2.PediaCluster{
		ObjectMeta: metav1.ObjectMeta{Name: testCluster, ResourceVersion: "1", Finalizers: []string{ClusterSynchroControllerFinalizer}},
		Spec:       clusterv1alpha2.ClusterSpec{ShardingName: specSharding},
		Status:     clusterv1alpha2.ClusterStatus{ShardingName: statusSharding},
	}
}

// settled pre-populates the Ready condition the status writer derives for a cluster without other conditions,
// for asserting that an owner leaves an unchanged status alone.
func settled(cluster *clusterv1alpha2.PediaCluster) *clusterv1alpha2.PediaCluster {
	cluster.Status.Conditions = []metav1.Condition{{
		Type:               clusterv1alpha2.ReadyCondition,
		Status:             metav1.ConditionFalse,
		Reason:             clusterv1alpha2.NotReadyReason,
		Message:            clusterv1alpha2.ValidatedCondition + " condition is not found",
		LastTransitionTime: metav1.Now().Rfc3339Copy(),
	}}
	return cluster
}

// newTestManager serves the lister from the given snapshot and the client from a fake tracker,
// so lister lag and write conflicts can be simulated independently.
func newTestManager(snapshot *clusterv1alpha2.PediaCluster) (*Manager, *fake.Clientset) {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	var objs []runtime.Object
	if snapshot != nil {
		_ = indexer.Add(snapshot)
		objs = append(objs, snapshot.DeepCopy())
	}
	client := fake.NewSimpleClientset(objs...)
	return &Manager{
		shardingName:       me,
		clusterpediaclient: client,
		clusterlister:      clusterlister.NewPediaClusterLister(indexer),
		queue:              workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}, client
}

// setLive replaces the tracker object with a newer resourceVersion than the snapshot
// and makes every write still carrying the snapshot's resourceVersion fail with Conflict.
func setLive(t *testing.T, client *fake.Clientset, live *clusterv1alpha2.PediaCluster) {
	t.Helper()
	live.ResourceVersion = "2"
	require.NoError(t, client.Tracker().Update(pediaClusterGVR, live, ""))
	client.PrependReactor("update", "pediaclusters", func(action clienttesting.Action) (bool, runtime.Object, error) {
		obj := action.(clienttesting.UpdateAction).GetObject().(*clusterv1alpha2.PediaCluster)
		if obj.ResourceVersion != live.ResourceVersion {
			return true, nil, apierrors.NewConflict(pediaClusterGVR.GroupResource(), obj.Name, errors.New("stale resource version"))
		}
		return false, nil, nil
	})
}

// newLaggingManager serves snapshot from the lister while the live object has already moved on.
func newLaggingManager(t *testing.T, snapshot, live *clusterv1alpha2.PediaCluster) (*Manager, *fake.Clientset) {
	t.Helper()
	manager, client := newTestManager(snapshot)
	setLive(t, client, live)
	return manager, client
}

func getCluster(t *testing.T, client *fake.Clientset) *clusterv1alpha2.PediaCluster {
	t.Helper()
	cluster, err := client.ClusterV1alpha2().PediaClusters().Get(context.TODO(), testCluster, metav1.GetOptions{})
	require.NoError(t, err)
	return cluster
}

func countActions(client *fake.Clientset, verb, subresource string) int {
	n := 0
	for _, action := range client.Actions() {
		if action.GetVerb() == verb && action.GetSubresource() == subresource {
			n++
		}
	}
	return n
}

func TestOwnedBy(t *testing.T) {
	tests := []struct {
		name     string
		status   *string
		sharding string
		owned    bool
	}{
		{"unset is never owned by the default sharding", nil, "", false},
		{"empty string is the claim of the default sharding", ptr.To(""), "", true},
		{"unset is not owned by a named sharding", nil, me, false},
		{"same name is owned", ptr.To(me), me, true},
		{"other name is not owned", ptr.To(other), me, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &clusterv1alpha2.PediaCluster{Status: clusterv1alpha2.ClusterStatus{ShardingName: tt.status}}
			assert.Equal(t, tt.owned, ownedBy(cluster, tt.sharding))
		})
	}
}

func TestClaimSharding(t *testing.T) {
	t.Run("claims unset sharding", func(t *testing.T) {
		manager, client := newTestManager(newShardedCluster(me, nil))
		require.NoError(t, manager.claimSharding(context.TODO(), testCluster))
		assert.Equal(t, ptr.To(me), getCluster(t, client).Status.ShardingName)
	})

	t.Run("default sharding claims with an explicit empty string", func(t *testing.T) {
		manager, client := newTestManager(newShardedCluster("", nil))
		manager.shardingName = ""
		require.NoError(t, manager.claimSharding(context.TODO(), testCluster))
		assert.Equal(t, ptr.To(""), getCluster(t, client).Status.ShardingName)
	})

	t.Run("already owned is validated live without writing", func(t *testing.T) {
		manager, client := newTestManager(settled(newShardedCluster(me, ptr.To(me))))
		require.NoError(t, manager.claimSharding(context.TODO(), testCluster))
		assert.Equal(t, 1, countActions(client, "get", ""))
		assert.Zero(t, countActions(client, "update", "status"))
	})

	// whatever the lister shows, the live owner is other.
	for name, cached := range map[string]*string{"cached unset": nil, "cached own": ptr.To(me), "cached other": ptr.To(other)} {
		t.Run(name+" but live owned by others is rejected", func(t *testing.T) {
			manager, client := newLaggingManager(t, newShardedCluster(me, cached), newShardedCluster(me, ptr.To(other)))
			assert.ErrorIs(t, manager.claimSharding(context.TODO(), testCluster), errShardingNotOwned)
			assert.Zero(t, countActions(client, "update", "status"))
			assert.Equal(t, ptr.To(other), getCluster(t, client).Status.ShardingName)
		})
	}

	t.Run("unset owner is not claimed when the live spec assigns another sharding", func(t *testing.T) {
		manager, client := newLaggingManager(t, newShardedCluster(me, nil), newShardedCluster(other, nil))
		assert.ErrorIs(t, manager.claimSharding(context.TODO(), testCluster), errShardingNotOwned)
		assert.Zero(t, countActions(client, "update", "status"))
		assert.Nil(t, getCluster(t, client).Status.ShardingName)
	})
}

func TestReleaseSharding(t *testing.T) {
	t.Run("releases own sharding", func(t *testing.T) {
		manager, client := newTestManager(newShardedCluster(other, ptr.To(me)))
		require.NoError(t, manager.releaseSharding(context.TODO(), testCluster))
		assert.Nil(t, getCluster(t, client).Status.ShardingName)
	})

	t.Run("does not touch a cluster owned by others", func(t *testing.T) {
		manager, client := newTestManager(newShardedCluster(other, ptr.To(other)))
		require.NoError(t, manager.releaseSharding(context.TODO(), testCluster))
		assert.Zero(t, countActions(client, "update", "status"))
		assert.Empty(t, getCluster(t, client).Status.Conditions, "derived conditions must not be written by a non-owner")
	})

	t.Run("keeps ownership when the live spec still assigns this sharding", func(t *testing.T) {
		manager, client := newLaggingManager(t, newShardedCluster(other, ptr.To(me)), settled(newShardedCluster(me, ptr.To(me))))
		require.NoError(t, manager.releaseSharding(context.TODO(), testCluster))
		assert.Zero(t, countActions(client, "update", "status"))
		assert.Equal(t, ptr.To(me), getCluster(t, client).Status.ShardingName)
	})
}

func TestUpdateClusterStatusFallsBackToLiveReadOnConflict(t *testing.T) {
	live := newShardedCluster(me, ptr.To(me))
	live.Status.APIServer = "https://live"
	manager, client := newLaggingManager(t, newShardedCluster(me, ptr.To(me)), live)

	err := manager.updateClusterStatus(context.TODO(), testCluster, func(cluster *clusterv1alpha2.PediaCluster) error {
		cluster.Status.Version = "v1.30.0"
		return nil
	})
	require.NoError(t, err)

	assert.Equal(t, 1, countActions(client, "get", ""))
	assert.Equal(t, 2, countActions(client, "update", "status"), "stale write + successful write")
	cluster := getCluster(t, client)
	assert.Equal(t, "v1.30.0", cluster.Status.Version)
	assert.Equal(t, "https://live", cluster.Status.APIServer, "live object fields must be preserved")
}

// Condition writers must not land on a cluster whose ownership moved to another sharding,
// even when the lister still names this manager and the write only reaches the live object on retry.
func TestStatusWritersRejectLostOwnership(t *testing.T) {
	newManager := func(t *testing.T) (*Manager, *fake.Clientset) {
		return newLaggingManager(t, newShardedCluster(me, ptr.To(me)), newShardedCluster(me, ptr.To(other)))
	}
	unchanged := func(t *testing.T, client *fake.Clientset) {
		t.Helper()
		cluster := getCluster(t, client)
		assert.Equal(t, ptr.To(other), cluster.Status.ShardingName)
		assert.Empty(t, cluster.Status.Conditions, "derived conditions must not be written by a non-owner")
	}

	t.Run("UpdateClusterStatus", func(t *testing.T) {
		manager, client := newManager(t)
		err := manager.UpdateClusterStatus(context.TODO(), testCluster, &clusterv1alpha2.ClusterStatus{
			Conditions: []metav1.Condition{{Type: clusterv1alpha2.SynchroRunningCondition, Status: metav1.ConditionTrue, Reason: clusterv1alpha2.SynchroRunningReason}},
		})
		assert.ErrorIs(t, err, errShardingNotOwned)
		unchanged(t, client)
	})

	t.Run("UpdateClusterAPIServerAndValidatedCondition", func(t *testing.T) {
		manager, client := newManager(t)
		manager.UpdateClusterAPIServerAndValidatedCondition(testCluster, "https://a", nil, clusterv1alpha2.ValidatedReason, "", metav1.ConditionTrue)
		unchanged(t, client)
	})
}

// An unclaimed cluster is not owned by anyone, including the default sharding whose name is the empty string.
func TestStatusWritersRejectUnclaimedClusterForDefaultSharding(t *testing.T) {
	manager, client := newTestManager(newShardedCluster("", nil))
	manager.shardingName = ""

	err := manager.UpdateClusterStatus(context.TODO(), testCluster, &clusterv1alpha2.ClusterStatus{Version: "v1.30.0"})
	assert.ErrorIs(t, err, errShardingNotOwned)
	assert.Equal(t, 1, countActions(client, "get", ""), "rejection from the lister must be confirmed live")
	assert.Zero(t, countActions(client, "update", "status"))
}

// Writers right after a claim see a lister that has not observed the claim yet,
// an ownership rejection from the lister must be re-evaluated on the live object instead of being final.
func TestStatusWritersConfirmOwnershipLiveWhenListerLags(t *testing.T) {
	newManager := func(t *testing.T) (*Manager, *fake.Clientset) {
		return newLaggingManager(t, newShardedCluster(me, nil), newShardedCluster(me, ptr.To(me)))
	}

	t.Run("UpdateClusterStatus", func(t *testing.T) {
		manager, client := newManager(t)
		err := manager.UpdateClusterStatus(context.TODO(), testCluster, &clusterv1alpha2.ClusterStatus{Version: "v1.30.0"})
		require.NoError(t, err)
		assert.Equal(t, 1, countActions(client, "get", ""))
		assert.Equal(t, "v1.30.0", getCluster(t, client).Status.Version)
	})

	t.Run("UpdateClusterAPIServerAndValidatedCondition", func(t *testing.T) {
		manager, client := newManager(t)
		manager.UpdateClusterAPIServerAndValidatedCondition(testCluster, "https://a", nil, clusterv1alpha2.ValidatedReason, "", metav1.ConditionTrue)
		cluster := getCluster(t, client)
		assert.Equal(t, "https://a", cluster.Status.APIServer)
		assert.True(t, meta.IsStatusConditionTrue(cluster.Status.Conditions, clusterv1alpha2.ValidatedCondition))
	})
}

func TestReconcileClusterClaim(t *testing.T) {
	t.Run("stops without requeue when claimed by others concurrently", func(t *testing.T) {
		manager, client := newLaggingManager(t, newShardedCluster(me, nil), newShardedCluster(me, ptr.To(other)))
		result := manager.reconcileCluster(newShardedCluster(me, nil))
		assert.False(t, result.Requeue())
		assert.Equal(t, ptr.To(other), getCluster(t, client).Status.ShardingName, "must not overwrite the other owner")
		assert.Empty(t, manager.synchros)
	})

	t.Run("retries without limit on a transient claim error", func(t *testing.T) {
		manager, client := newTestManager(newShardedCluster(me, nil))
		client.PrependReactor("update", "pediaclusters", func(clienttesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewInternalError(errors.New("boom"))
		})

		result := manager.reconcileCluster(newShardedCluster(me, nil))
		assert.True(t, result.Requeue())
		assert.Equal(t, math.MaxInt, result.MaxRetryCount())
		assert.Empty(t, manager.synchros)
	})
}

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
		{
			name:     "ownership taken over by another sharding is enqueued",
			older:    newCluster(spec, ptr.To(sharding)),
			newer:    newCluster(spec, ptr.To(other)),
			enqueued: true,
		},
		{
			name:     "ownership taken over before an assignment change is enqueued",
			older:    newCluster(spec, ptr.To(sharding)),
			newer:    newCluster(clusterv1alpha2.ClusterSpec{ShardingName: other}, ptr.To(other)),
			enqueued: true,
		},
		{
			name:     "cluster of another sharding is ignored",
			older:    newCluster(clusterv1alpha2.ClusterSpec{ShardingName: other}, ptr.To(other)),
			newer:    newCluster(clusterv1alpha2.ClusterSpec{ShardingName: other, APIServer: "https://b"}, ptr.To(other)),
			enqueued: false,
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
