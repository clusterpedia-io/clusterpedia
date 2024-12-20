package internalstorage

/*
	Copy from https://github.com/go-gorm/prometheus

	Some streamlining and optimization as features like Push and HTTPServer are not needed.
*/

import (
	"context"
	"database/sql"
	"reflect"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"gorm.io/gorm"
	"k8s.io/component-base/metrics/legacyregistry"
)

var (
	_ gorm.Plugin = &Prometheus{}
)

const (
	defaultRefreshInterval = 15 * time.Second // the prometheus default pull metrics every 15 seconds
)

// Prometheus implements the gorm.Plugin and provides db metrics.
// The `Prometheus` structure name is preserved, and this file may be moved to a separate package
// in the future to be made available to other gorm-based storage layers.
type Prometheus struct {
	*gorm.DB
	*DBStats

	refreshInterval time.Duration
	refreshOnce     sync.Once
	Labels          map[string]string
}

func NewGormMetrics(dbName string, refreshInterval time.Duration) *Prometheus {
	// TODO(iceber): Use real-time mode to get DBStats when the refresh time is below a certain threshold
	if refreshInterval == 0 {
		refreshInterval = defaultRefreshInterval
	}

	labels := map[string]string{"db_name": dbName}
	return &Prometheus{refreshInterval: refreshInterval, Labels: labels}
}

func (p *Prometheus) Name() string {
	return "gorm:prometheus"
}

func (p *Prometheus) Initialize(db *gorm.DB) error { // can be called repeatedly
	p.DB = db
	p.DBStats = newStats(p.Labels)

	p.refreshOnce.Do(func() {
		go func() {
			for range time.Tick(p.refreshInterval) {
				p.refresh()
			}
		}()
	})
	return nil
}

func (p *Prometheus) refresh() {
	db, err := p.DB.DB()
	if err != nil {
		p.DB.Logger.Error(context.Background(), "gorm:prometheus failed to collect db status, got error: %v", err)
		return
	}
	p.DBStats.set(db.Stats())
}

type DBStats struct {
	MaxOpenConnections prometheus.Gauge // Maximum number of open connections to the database.

	// Pool status
	OpenConnections prometheus.Gauge // The number of established connections both in use and idle.
	InUse           prometheus.Gauge // The number of connections currently in use.
	Idle            prometheus.Gauge // The number of idle connections.

	// Counters
	WaitCount         prometheus.Gauge // The total number of connections waited for.
	WaitDuration      prometheus.Gauge // The total time blocked waiting for a new connection.
	MaxIdleClosed     prometheus.Gauge // The total number of connections closed due to SetMaxIdleConns.
	MaxLifetimeClosed prometheus.Gauge // The total number of connections closed due to SetConnMaxLifetime.
	MaxIdleTimeClosed prometheus.Gauge // The total number of connections closed due to SetConnMaxIdleTime.
}

func newStats(labels map[string]string) *DBStats {
	stats := &DBStats{
		MaxOpenConnections: prometheus.NewGauge(prometheus.GaugeOpts{
			Subsystem:   "storage",
			Name:        "dbstats_max_open_connections",
			Help:        "Maximum number of open connections to the database.",
			ConstLabels: labels,
		}),
		OpenConnections: prometheus.NewGauge(prometheus.GaugeOpts{
			Subsystem:   "storage",
			Name:        "dbstats_open_connections",
			Help:        "The number of established connections both in use and idle.",
			ConstLabels: labels,
		}),
		InUse: prometheus.NewGauge(prometheus.GaugeOpts{
			Subsystem:   "storage",
			Name:        "dbstats_in_use",
			Help:        "The number of connections currently in use.",
			ConstLabels: labels,
		}),
		Idle: prometheus.NewGauge(prometheus.GaugeOpts{
			Subsystem:   "storage",
			Name:        "dbstats_idle",
			Help:        "The number of idle connections.",
			ConstLabels: labels,
		}),
		WaitCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Subsystem:   "storage",
			Name:        "dbstats_wait_count",
			Help:        "The total number of connections waited for.",
			ConstLabels: labels,
		}),
		WaitDuration: prometheus.NewGauge(prometheus.GaugeOpts{
			Subsystem:   "storage",
			Name:        "dbstats_wait_duration",
			Help:        "The total time blocked waiting for a new connection.",
			ConstLabels: labels,
		}),
		MaxIdleClosed: prometheus.NewGauge(prometheus.GaugeOpts{
			Subsystem:   "storage",
			Name:        "dbstats_max_idle_closed",
			Help:        "The total number of connections closed due to SetMaxIdleConns.",
			ConstLabels: labels,
		}),
		MaxLifetimeClosed: prometheus.NewGauge(prometheus.GaugeOpts{
			Subsystem:   "storage",
			Name:        "dbstats_max_lifetime_closed",
			Help:        "The total number of connections closed due to SetConnMaxLifetime.",
			ConstLabels: labels,
		}),
		MaxIdleTimeClosed: prometheus.NewGauge(prometheus.GaugeOpts{
			Subsystem:   "storage",
			Name:        "dbstats_max_idletime_closed",
			Help:        "The total number of connections closed due to SetConnMaxIdleTime.",
			ConstLabels: labels,
		}),
	}

	for _, collector := range stats.collectors() {
		legacyregistry.RawMustRegister(collector)
	}

	return stats
}

func (stats *DBStats) set(dbStats sql.DBStats) {
	stats.MaxOpenConnections.Set(float64(dbStats.MaxOpenConnections))
	stats.OpenConnections.Set(float64(dbStats.OpenConnections))
	stats.InUse.Set(float64(dbStats.InUse))
	stats.Idle.Set(float64(dbStats.Idle))
	stats.WaitCount.Set(float64(dbStats.WaitCount))
	stats.WaitDuration.Set(float64(dbStats.WaitDuration))
	stats.MaxIdleClosed.Set(float64(dbStats.MaxIdleClosed))
	stats.MaxLifetimeClosed.Set(float64(dbStats.MaxLifetimeClosed))
	stats.MaxIdleTimeClosed.Set(float64(dbStats.MaxIdleTimeClosed))
}

// get collector in stats
func (stats *DBStats) collectors() (collector []prometheus.Collector) {
	dbStatsValue := reflect.ValueOf(*stats)
	for i := 0; i < dbStatsValue.NumField(); i++ {
		collector = append(collector, dbStatsValue.Field(i).Interface().(prometheus.Gauge))
	}
	return
}
