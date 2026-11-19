package resolution

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
	kmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	ocirepository "ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func init() {
	kmetrics.Registry.MustRegister(
		ResolutionDurationHistogram,
	)
	ocirepository.MustAddToScheme(repositoryLabelScheme)
	ocirepository.MustAddLegacyToScheme(repositoryLabelScheme)
}

// repositoryLabelScheme knows the legacy OCIRegistry types without relying on cmd/main.go registering them.
var repositoryLabelScheme = runtime.NewScheme()

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
	// RepositoryLabel is the name of the label for the registry host, or the repository type for non-registries.
	RepositoryLabel = "repository"
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
}, []string{VerificationStateLabel, RepositoryLabel, ResultLabel})

// repositoryLabel returns the registry host of an OCI repository and the type name of any other repository.
// Paths are never used: a CTF file path or a registry sub path would make the label unbounded again.
func repositoryLabel(spec runtime.Typed) string {
	if spec == nil {
		return "unknown"
	}
	obj, err := repositoryLabelScheme.NewObject(spec.GetType())
	if err != nil {
		return spec.GetType().Name
	}
	if err := repositoryLabelScheme.Convert(spec, obj); err != nil {
		return spec.GetType().Name
	}
	switch repo := obj.(type) {
	case *oci.Repository:
		u, err := runtime.ParseURLAndAllowNoScheme(repo.BaseUrl)
		if err != nil || u.Host == "" {
			return "unknown"
		}
		return u.Host
	case *ctf.Repository:
		return ctf.ShortType2
	default:
		return spec.GetType().Name
	}
}

// resultLabel treats a component version that is not safely digestible as resolved, because it is returned.
func resultLabel(err error) string {
	if err == nil || errors.Is(err, ErrNotSafelyDigestible) {
		return "success"
	}

	return "error"
}
