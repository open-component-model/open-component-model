package resource

import (
	kmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/metrics"
)

const (
	verificationMetricsNamespace = "ocm_system"
	verificationMetricsComponent = "ocm_k8s_toolkit"

	// access_type is bounded by boundedAccessType: a supported access type keeps its name,
	// anything else collapses to verificationAccessTypeOther. The name comes from untrusted
	// descriptor data (the access block is not signature-covered), so it must not be used raw.
	// component/version/resource name are intentionally not labels to bound cardinality.
	verificationAccessTypeLabel = "access_type"
	verificationResultLabel     = "result"

	verificationResultVerified = "verified"
	verificationResultSkipped  = "skipped"
	verificationResultFailed   = "failed"

	verificationAccessTypeOther = "other"
)

// ResourceVerificationDurationHistogram records resource digest verification duration by access
// type and outcome. Cost varies sharply by access type (localBlob hashes a local blob;
// by-reference types fetch remote content), and the per-label _count yields the outcome counts.
var ResourceVerificationDurationHistogram = metrics.MustRegisterHistogramVec(
	verificationMetricsNamespace,
	verificationMetricsComponent,
	"resource_verification_duration_seconds",
	"Duration of resource digest verification in seconds, by access type and result.",
	[]float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
	verificationAccessTypeLabel, verificationResultLabel,
)

func init() {
	kmetrics.Registry.MustRegister(ResourceVerificationDurationHistogram)
}
