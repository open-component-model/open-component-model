package resolution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	kmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/internal/metrics"
)

func init() {
	kmetrics.Registry.MustRegister(
		CacheMissCounterTotal,
		CacheHitCounterTotal,
		QueueSizeGauge,
		InProgressGauge,
		ResolutionDurationHistogram,
	)
}

const (
	// CacheMissCounterLabel tracks how many cache misses happened.
	CacheMissCounterLabel = "cache_miss"
	// CacheHitCounterLabel tracks how many cache hits happened.
	CacheHitCounterLabel = "cache_hit"
	// QueueSizeGaugeLabel tracks the current size of the lookup queue.
	QueueSizeGaugeLabel = "queue_size"
	// InProgressGaugeLabel tracks the number of resolutions currently in progress.
	InProgressGaugeLabel = "in_progress"
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
)

// CacheMissCounterTotal counts the number of times a cache miss occurred.
// [component, version].
var CacheMissCounterTotal = metrics.MustRegisterCounterVec(
	MetricsNamespace,
	OcmComponent,
	CacheMissCounterLabel,
	"Number of times a cache miss occurred.",
	ComponentLabel, VersionLabel,
)

// CacheHitCounterTotal counts the number of times a cache hit occurred.
// [component, version].
var CacheHitCounterTotal = metrics.MustRegisterCounterVec(
	MetricsNamespace,
	OcmComponent,
	CacheHitCounterLabel,
	"Number of times a cache hit occurred.",
	ComponentLabel, VersionLabel,
)

// QueueSizeGauge tracks the current size of the lookup queue.
var QueueSizeGauge = metrics.MustRegisterGauge(
	MetricsNamespace,
	OcmComponent,
	QueueSizeGaugeLabel,
	"Current size of the component version lookup queue.",
)

// InProgressGauge tracks the number of resolutions currently in progress.
var InProgressGauge = metrics.MustRegisterGauge(
	MetricsNamespace,
	OcmComponent,
	InProgressGaugeLabel,
	"Number of component version resolutions currently in progress.",
)

// ResolutionDurationHistogram tracks the duration of component version resolutions.
// [component, version].
var ResolutionDurationHistogram = metrics.MustRegisterHistogramVec(
	MetricsNamespace,
	OcmComponent,
	ResolutionDurationHistogramLabel,
	"Duration of component version resolutions in seconds.",
	[]float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
	ComponentLabel, VersionLabel,
)

func buildCacheKey(configHash []byte, repoSpec runtime.Typed, component, version string) (string, error) {
	repoJSON, err := json.Marshal(repoSpec)
	if err != nil {
		return "", fmt.Errorf("failed to marshal repository spec: %w", err)
	}

	hasher := sha256.New()
	hasher.Write(configHash)
	hasher.Write(repoJSON)
	hasher.Write([]byte(component))
	hasher.Write([]byte(version))
	return hex.EncodeToString(hasher.Sum(nil)), err
}

// Result contains the result of a resolution including any errors that might have occurred.
type Result struct {
	result *ResolveResult
	err    error
}
