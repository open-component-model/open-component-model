package dynamic

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	activeTasks             *prometheus.GaugeVec
	registerTotal           *prometheus.CounterVec
	unregisterTotal         *prometheus.CounterVec
	eventCount              *prometheus.CounterVec
	workerOperationDuration *prometheus.HistogramVec
)

func MustRegisterMetrics(registerer prometheus.Registerer) {
	if err := RegisterMetrics(registerer); err != nil {
		panic(err)
	}
}

func RegisterMetrics(registerer prometheus.Registerer) error {
	return errors.Join(
		registerer.Register(activeTasks),
		registerer.Register(registerTotal),
		registerer.Register(unregisterTotal),
		registerer.Register(eventCount),
		registerer.Register(workerOperationDuration),
	)
}

func init() {
	activeTasks = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dynamic_informer_active_watch_tasks",
		Help: "Number of active watch tasks",
	}, []string{"name"})
	registerTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dynamic_informer_register_total",
			Help: "Total number of dynamic register operations for new watches",
		},
		[]string{"name", "group", "version", "kind", "namespace"},
	)
	unregisterTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dynamic_informer_unregister_total",
			Help: "Total number of unregister operations for old watches",
		},
		[]string{"name", "group", "version", "kind", "namespace"},
	)
	eventCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dynamic_informer_events_handled_total",
			Help: "Number of events handled by type",
		},
		[]string{"name", "event_type"},
	)
	workerOperationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dynamic_informer_operation_duration_seconds",
			Help:    "Duration of watch registration operations",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"name", "operation"},
	)
}
