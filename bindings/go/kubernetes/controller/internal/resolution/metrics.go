package resolution

import (
	kmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/metrics"
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
	// ComponentLabel is the name of the label for the passed in component's name.
	ComponentLabel = "component"
	// VersionLabel is the name of the label for the passed in component's version.
	VersionLabel = "version"
	// VerificationStateLabel is the name of the label for the verification state of a resolved component version.
	VerificationStateLabel = "verification_state"
)

// ResolutionDurationHistogram tracks the duration of component version resolutions.
// [component, version, verification_state].
var ResolutionDurationHistogram = metrics.MustRegisterHistogramVec(
	MetricsNamespace,
	OcmComponent,
	ResolutionDurationHistogramLabel,
	"Duration of component version resolutions in seconds.",
	[]float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
	ComponentLabel, VersionLabel, VerificationStateLabel,
)
