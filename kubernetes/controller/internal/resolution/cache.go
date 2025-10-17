package resolution

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"

	kmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/metrics"
)

func init() {
	kmetrics.Registry.MustRegister(
		CacheMissCounterTotal,
		CacheShareCounterTotal,
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
	// CacheShareCounterLabel tracks how many cache share de-duplications happened with singleflight.
	CacheShareCounterLabel = "cache_share"
	// QueueSizeGaugeLabel tracks the current size of the lookup queue.
	QueueSizeGaugeLabel = "queue_size"
	// InProgressGaugeLabel tracks the number of resolutions currently in progress.
	InProgressGaugeLabel = "in_progress"
	// ResolutionDurationHistogramLabel tracks the duration of component version resolutions.
	ResolutionDurationHistogramLabel = "resolution_duration_seconds"
)

// CacheMissCounterTotal counts the number of times a cache miss occurred.
// [component, version].
var CacheMissCounterTotal = metrics.MustRegisterCounterVec(
	"ocm_system",
	"ocm_controller",
	CacheMissCounterLabel,
	"Number of times a cache miss occurred.",
	"component", "version",
)

// CacheHitCounterTotal counts the number of times a cache hit occurred.
// [component, version].
var CacheHitCounterTotal = metrics.MustRegisterCounterVec(
	"ocm_system",
	"ocm_controller",
	CacheHitCounterLabel,
	"Number of times a cache hit occurred.",
	"component", "version",
)

// CacheShareCounterTotal counts the number of times a cache share occurred.
// [component, version].
var CacheShareCounterTotal = metrics.MustRegisterCounterVec(
	"ocm_system",
	"ocm_controller",
	CacheShareCounterLabel,
	"Number of times a cache chare occurred.",
	"component", "version",
)

// QueueSizeGauge tracks the current size of the lookup queue.
var QueueSizeGauge = metrics.MustRegisterGauge(
	"ocm_system",
	"ocm_controller",
	QueueSizeGaugeLabel,
	"Current size of the component version lookup queue.",
)

// InProgressGauge tracks the number of resolutions currently in progress.
var InProgressGauge = metrics.MustRegisterGauge(
	"ocm_system",
	"ocm_controller",
	InProgressGaugeLabel,
	"Number of component version resolutions currently in progress.",
)

// ResolutionDurationHistogram tracks the duration of component version resolutions.
// [component, version].
var ResolutionDurationHistogram = metrics.MustRegisterHistogramVec(
	"ocm_system",
	"ocm_controller",
	ResolutionDurationHistogramLabel,
	"Duration of component version resolutions in seconds.",
	[]float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
	"component", "version",
)

type cacheKey struct {
	configHash string
	repoHash   string
	component  string
	version    string
}

func (k cacheKey) String() string {
	return fmt.Sprintf("%s:%s:%s:%s", k.configHash, k.repoHash, k.component, k.version)
}

func buildCacheKey(configHash []byte, repoSpec runtime.Typed, component, version string) (cacheKey, error) {
	repoJSON, err := json.Marshal(repoSpec)
	if err != nil {
		return cacheKey{}, fmt.Errorf("failed to marshal repository spec: %w", err)
	}

	hasher := sha256.New()
	hasher.Write(repoJSON)
	repoHash := hasher.Sum(nil)

	key := cacheKey{
		configHash: fmt.Sprintf("%x", configHash),
		repoHash:   fmt.Sprintf("%x", repoHash),
		component:  component,
		version:    version,
	}

	return key, nil
}

// Cache defines the interface for component version resolution caching.
type Cache interface {
	Get(key string) (*ResolveResult, bool)
	Set(key string, result *ResolveResult)
	Delete(key string)
}

// InMemoryCache implements Cache using a simple in-memory map.
type InMemoryCache struct {
	mu    sync.RWMutex
	store map[string]*ResolveResult
}

// NewInMemoryCache creates a new in-memory cache.
func NewInMemoryCache() *InMemoryCache {
	return &InMemoryCache{
		store: make(map[string]*ResolveResult),
	}
}

// Get retrieves a result from the cache.
func (c *InMemoryCache) Get(key string) (*ResolveResult, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result, ok := c.store[key]
	return result, ok
}

// Set stores a result in the cache.
func (c *InMemoryCache) Set(key string, result *ResolveResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[key] = result
}

// Delete removes a result from the cache.
func (c *InMemoryCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.store, key)
}
