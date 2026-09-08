package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// MustRegisterCounterVec creates and registers a counter vector.
// Must be called from `init`.
func MustRegisterCounterVec(namespace, component, name, help string, labelNames ...string) *prometheus.CounterVec {
	m := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: component,
		Name:      name,
		Help:      help,
	}, labelNames)
	prometheus.MustRegister(m)
	return m
}

// MustRegisterGauge creates and registers a gauge.
// Must be called from `init`.
func MustRegisterGauge(namespace, component, name, help string) prometheus.Gauge {
	m := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: component,
		Name:      name,
		Help:      help,
	})
	prometheus.MustRegister(m)
	return m
}

// MustRegisterHistogramVec creates and registers a histogram vector.
// Must be called from `init`.
func MustRegisterHistogramVec(namespace, component, name, help string, buckets []float64, labelNames ...string) *prometheus.HistogramVec {
	m := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: component,
		Name:      name,
		Help:      help,
		Buckets:   buckets,
	}, labelNames)
	prometheus.MustRegister(m)
	return m
}
