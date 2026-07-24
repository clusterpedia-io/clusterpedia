package internalstorage

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// newTestOperationMetrics builds an operationMetrics with un-registered
// collectors so the test does not touch the global registry.
func newTestOperationMetrics() *operationMetrics {
	labels := prometheus.Labels{"db_name": "test"}
	return &operationMetrics{
		total: prometheus.NewCounterVec(prometheus.CounterOpts{
			Subsystem: "storage", Name: "operation_total", ConstLabels: labels,
		}, []string{"operation", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Subsystem: "storage", Name: "operation_duration_seconds", ConstLabels: labels,
			Buckets: prometheus.DefBuckets,
		}, []string{"operation"}),
	}
}

func TestOperationMetrics(t *testing.T) {
	db, cleanup, err := newSQLiteDB()
	if err != nil {
		t.Fatalf("newSQLiteDB() failed: %v", err)
	}
	defer cleanup()

	// Wire the callbacks directly so the test does not register collectors on
	// the global registry (Initialize would).
	m := newTestOperationMetrics()
	if err := m.registerCallbacks(db); err != nil {
		t.Fatalf("register operation metrics callbacks: %v", err)
	}

	// A successful query is counted under query/success.
	var resources []Resource
	if err := db.Find(&resources).Error; err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if got := testutil.ToFloat64(m.total.WithLabelValues("query", "success")); got != 1 {
		t.Errorf("query success count = %v, want 1", got)
	}

	// A failing query (unknown table) is counted under query/error.
	if err := db.Table("does_not_exist").Find(&resources).Error; err == nil {
		t.Fatal("expected query against unknown table to fail")
	}
	if got := testutil.ToFloat64(m.total.WithLabelValues("query", "error")); got != 1 {
		t.Errorf("query error count = %v, want 1", got)
	}

	// Exec runs through the raw callback and is counted under raw/success.
	if err := db.Exec("SELECT 1").Error; err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if got := testutil.ToFloat64(m.total.WithLabelValues("raw", "success")); got != 1 {
		t.Errorf("raw success count = %v, want 1", got)
	}

	// Latency is observed for the operations that ran.
	if got := testutil.CollectAndCount(m.duration); got == 0 {
		t.Error("operation duration histogram recorded no samples")
	}
}
