package internalstorage

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"gorm.io/gorm"
	"k8s.io/component-base/metrics/legacyregistry"
)

// opStartTimeKey is the GORM instance setting that carries an operation's start
// time from its before callback to its after callback.
const opStartTimeKey = "clusterpedia:operation_start"

var _ gorm.Plugin = &operationMetrics{}

// operationMetrics is a gorm.Plugin that records the count and latency of
// database operations (create/query/update/delete/row/raw) via GORM callbacks.
// It complements the connection-pool metrics provided by Prometheus.
type operationMetrics struct {
	dbName   string
	total    *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// NewGormOperationMetrics builds the operation-metrics plugin. dbName is attached
// as a const label so metrics from multiple databases can be told apart. The
// collectors are created and registered in Initialize, matching Prometheus.
func NewGormOperationMetrics(dbName string) gorm.Plugin {
	return &operationMetrics{dbName: dbName}
}

// Name implements gorm.Plugin.
func (m *operationMetrics) Name() string {
	return "gorm:clusterpedia-operation-metrics"
}

// Initialize implements gorm.Plugin. It registers the collectors and hooks the
// per-operation callbacks.
func (m *operationMetrics) Initialize(db *gorm.DB) error {
	labels := prometheus.Labels{"db_name": m.dbName}
	m.total = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem:   "storage",
		Name:        "operation_total",
		Help:        "Total number of database operations, by operation and status.",
		ConstLabels: labels,
	}, []string{"operation", "status"})
	m.duration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Subsystem:   "storage",
		Name:        "operation_duration_seconds",
		Help:        "Latency of database operations in seconds, by operation.",
		ConstLabels: labels,
		Buckets:     prometheus.DefBuckets,
	}, []string{"operation"})
	legacyregistry.RawMustRegister(m.total, m.duration)

	return m.registerCallbacks(db)
}

// registerCallbacks hooks a before callback that stamps the start time and an
// after callback that records the result for each operation.
func (m *operationMetrics) registerCallbacks(db *gorm.DB) error {
	cb := db.Callback()
	hooks := []struct {
		before    gormRegister
		after     gormRegister
		operation string
	}{
		{cb.Create().Before("gorm:create"), cb.Create().After("gorm:create"), "create"},
		{cb.Query().Before("gorm:query"), cb.Query().After("gorm:query"), "query"},
		{cb.Update().Before("gorm:update"), cb.Update().After("gorm:update"), "update"},
		{cb.Delete().Before("gorm:delete"), cb.Delete().After("gorm:delete"), "delete"},
		{cb.Row().Before("gorm:row"), cb.Row().After("gorm:row"), "row"},
		{cb.Raw().Before("gorm:raw"), cb.Raw().After("gorm:raw"), "raw"},
	}

	var firstErr error
	for _, h := range hooks {
		if err := h.before.Register("clusterpedia:metrics:before:"+h.operation, recordOperationStart); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("register before:%s metrics callback: %w", h.operation, err)
		}
		if err := h.after.Register("clusterpedia:metrics:after:"+h.operation, m.recordOperation(h.operation)); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("register after:%s metrics callback: %w", h.operation, err)
		}
	}
	return firstErr
}

// recordOperationStart stamps the operation's start time for recordOperation.
func recordOperationStart(tx *gorm.DB) {
	tx.InstanceSet(opStartTimeKey, time.Now())
}

// recordOperation returns an after callback that increments the operation
// counter and observes its latency.
func (m *operationMetrics) recordOperation(operation string) gormHookFunc {
	return func(tx *gorm.DB) {
		status := "success"
		if isOperationError(tx.Error) {
			status = "error"
		}
		m.total.WithLabelValues(operation, status).Inc()

		if v, ok := tx.InstanceGet(opStartTimeKey); ok {
			if start, ok := v.(time.Time); ok {
				m.duration.WithLabelValues(operation).Observe(time.Since(start).Seconds())
			}
		}
	}
}

// isOperationError reports whether err is a real database failure. Expected
// "no rows" outcomes are not counted as errors, matching the tracing plugin.
func isOperationError(err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound),
		errors.Is(err, driver.ErrSkip),
		errors.Is(err, io.EOF),
		errors.Is(err, sql.ErrNoRows):
		return false
	default:
		return true
	}
}
