package resolution

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
	kmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

func init() {
	kmetrics.Registry.MustRegister(
		ResolutionDurationHistogram,
	)
}

const (
	// ResolutionDurationHistogramLabel tracks the duration of component version resolutions.
	ResolutionDurationHistogramLabel = "resolution_duration_seconds"
	// MetricsNamespace defines the namespace of all the resolution metrics.
	MetricsNamespace = "ocm_system"
	// OcmComponent is the name of the component registering for these metrics.
	OcmComponent = "ocm_k8s_toolkit"
)

const (
	// VerificationStateLabel is the name of the label for the verification state of a resolved component version.
	VerificationStateLabel = "verification_state"
	// ResultLabel is the name of the label for the outcome of a resolution, either "success" or "error".
	ResultLabel = "result"
)

// ResolutionDurationHistogram tracks the duration of component version resolutions.
// Component name and version are deliberately not labels: series are never deleted,
// so every resolved version would add a histogram for the lifetime of the process.
var ResolutionDurationHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: MetricsNamespace,
	Subsystem: OcmComponent,
	Name:      ResolutionDurationHistogramLabel,
	Help:      "Duration of component version resolutions in seconds.",
	Buckets:   []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
}, []string{VerificationStateLabel, ResultLabel})

// resultLabel treats a component version that is not safely digestible as resolved, because it is returned.
func resultLabel(err error) string {
	if err == nil || errors.Is(err, ErrNotSafelyDigestible) {
		return "success"
	}

	return "error"
}
