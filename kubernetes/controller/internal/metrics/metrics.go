package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	// StatusStarted signifies that an installation has been started.
	StatusStarted = "started"
	// StatusSuccess signifies that an installation has been finished successfully.
	StatusSuccess = "success"
	// StatusFailure signifies that an installation has failed.
	StatusFailure = "failure"
)

// MustRegisterCounter creates and registers a counter.
// Must be called from `init`.
func MustRegisterCounter(namespace, component, name, help string) prometheus.Counter {
	m := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: component,
		Name:      name,
		Help:      help,
	})
	prometheus.MustRegister(m)
	return m
}

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

// MustRegisterGaugeVec creates and registers a gauge vector.
// Must be called from `init`.
func MustRegisterGaugeVec(namespace, component, name, help string, labelNames ...string) *prometheus.GaugeVec {
	m := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: component,
		Name:      name,
		Help:      help,
	}, labelNames)
	prometheus.MustRegister(m)
	return m
}

// MustRegisterSummary creates and registers a summary.
// Must be called from `init`.
func MustRegisterSummary(namespace, component, name, help string, objectives map[float64]float64) prometheus.Summary {
	if objectives == nil {
		objectives = map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001}
	}
	m := prometheus.NewSummary(prometheus.SummaryOpts{
		Namespace:  namespace,
		Subsystem:  component,
		Name:       name,
		Help:       help,
		Objectives: objectives,
	})
	prometheus.MustRegister(m)
	return m
}

// MustRegisterHistogram creates and registers a histogram.
// Must be called from `init`.
func MustRegisterHistogram(namespace, component, name, help string, buckets []float64) prometheus.Histogram {
	m := prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: component,
		Name:      name,
		Help:      help,
		Buckets:   buckets,
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

// SetDurationObserver sets an observed value for the duration since the given start time
// in seconds.
func SetDurationObserver(o prometheus.Observer, startTime time.Time) {
	o.Observe(time.Since(startTime).Seconds())
}

// SetDuration sets a gauge value for the duration since the given start time
// in seconds.
func SetDuration(g prometheus.Gauge, startTime time.Time) {
	g.Set(time.Since(startTime).Seconds())
}
