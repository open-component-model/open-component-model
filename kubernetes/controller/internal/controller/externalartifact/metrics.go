package externalartifact

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds the Prometheus collectors for the artifact storage subsystem.
// It is deliberately small and process-global via MustRegisterMetrics so the
// Storage and StorageServer can record without threading a registry everywhere.
type Metrics struct {
	// ArtifactsProducedTotal counts successfully packaged artifacts.
	ArtifactsProducedTotal prometheus.Counter
	// ArchiveErrorsTotal counts failed packaging attempts.
	ArchiveErrorsTotal prometheus.Counter
	// ArchiveDurationSeconds observes packaging wall-clock time.
	ArchiveDurationSeconds prometheus.Histogram
	// ArtifactSizeBytes observes the size of produced archives.
	ArtifactSizeBytes prometheus.Histogram
	// ServeRequestsTotal counts artifact HTTP requests by status class.
	ServeRequestsTotal *prometheus.CounterVec
	// ServeDurationSeconds observes artifact HTTP serve latency.
	ServeDurationSeconds prometheus.Histogram
	// PruneErrorsTotal counts failures while pruning stale/orphaned artifacts.
	PruneErrorsTotal prometheus.Counter
	// DiskUsedBytes reports the total bytes currently occupied by stored
	// artifacts, refreshed on each garbage-collection sweep.
	DiskUsedBytes prometheus.Gauge
	// ArtifactCount reports the number of artifact objects currently on disk,
	// refreshed on each garbage-collection sweep.
	ArtifactCount prometheus.Gauge
	// IntegrityFailuresTotal counts artifacts found corrupt (digest mismatch)
	// during integrity verification.
	IntegrityFailuresTotal prometheus.Counter
	// SweepDurationSeconds observes the wall-clock time of a full GC sweep
	// (orphan removal + integrity verification + usage refresh).
	SweepDurationSeconds prometheus.Histogram
}

var (
	metricsOnce   sync.Once
	globalMetrics *Metrics
)

// MustRegisterMetrics registers the storage metrics with the given registry
// exactly once and returns the shared Metrics. Subsequent calls return the
// already-registered instance (the registry argument is ignored after the
// first call), mirroring the pattern used by the other controller subsystems.
func MustRegisterMetrics(registry prometheus.Registerer) *Metrics {
	metricsOnce.Do(func() {
		globalMetrics = newMetricsFor(registry)
	})

	return globalMetrics
}

// newMetricsFor builds and registers the metric collectors with registry. It is
// used by MustRegisterMetrics (with the process registry) and directly by tests
// (with a private registry) so metric assertions stay isolated.
func newMetricsFor(registry prometheus.Registerer) *Metrics {
	m := &Metrics{
		ArtifactsProducedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ocm_external_artifact_produced_total",
			Help: "Total number of ExternalArtifact tar.gz archives successfully produced.",
		}),
		ArchiveErrorsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ocm_external_artifact_archive_errors_total",
			Help: "Total number of failed ExternalArtifact packaging attempts.",
		}),
		ArchiveDurationSeconds: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "ocm_external_artifact_archive_duration_seconds",
			Help:    "Time spent packaging an ExternalArtifact archive.",
			Buckets: prometheus.DefBuckets,
		}),
		ArtifactSizeBytes: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "ocm_external_artifact_size_bytes",
			Help:    "Size in bytes of produced ExternalArtifact archives.",
			Buckets: prometheus.ExponentialBuckets(1024, 4, 8), //nolint:mnd // 1KiB..~16MiB range
		}),
		ServeRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ocm_external_artifact_serve_requests_total",
			Help: "Total number of artifact HTTP requests by status class (2xx/4xx/5xx).",
		}, []string{"code"}),
		ServeDurationSeconds: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "ocm_external_artifact_serve_duration_seconds",
			Help:    "Latency of artifact HTTP requests.",
			Buckets: prometheus.DefBuckets,
		}),
		PruneErrorsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ocm_external_artifact_prune_errors_total",
			Help: "Total number of failures while pruning stale or orphaned artifacts.",
		}),
		DiskUsedBytes: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ocm_external_artifact_disk_used_bytes",
			Help: "Total bytes occupied by stored ExternalArtifact archives (refreshed each GC sweep).",
		}),
		ArtifactCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ocm_external_artifact_count",
			Help: "Number of ExternalArtifact objects currently stored on disk (refreshed each GC sweep).",
		}),
		IntegrityFailuresTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ocm_external_artifact_integrity_failures_total",
			Help: "Total number of stored artifacts found corrupt (digest mismatch) during integrity verification.",
		}),
		SweepDurationSeconds: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "ocm_external_artifact_gc_sweep_duration_seconds",
			Help:    "Wall-clock time of a full artifact garbage-collection sweep.",
			Buckets: prometheus.DefBuckets,
		}),
	}
	registry.MustRegister(
		m.ArtifactsProducedTotal,
		m.ArchiveErrorsTotal,
		m.ArchiveDurationSeconds,
		m.ArtifactSizeBytes,
		m.ServeRequestsTotal,
		m.ServeDurationSeconds,
		m.PruneErrorsTotal,
		m.DiskUsedBytes,
		m.ArtifactCount,
		m.IntegrityFailuresTotal,
		m.SweepDurationSeconds,
	)

	return m
}
