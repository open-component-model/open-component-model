package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

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
