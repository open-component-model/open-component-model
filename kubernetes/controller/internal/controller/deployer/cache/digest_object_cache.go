package cache

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/utils/lru"
)

var (
	objectCacheSize *prometheus.GaugeVec
	missCount       *prometheus.CounterVec
	hitCount        *prometheus.CounterVec
	evictCount      *prometheus.CounterVec
)

func MustRegisterMetrics(registerer prometheus.Registerer) {
	if err := RegisterMetrics(registerer); err != nil {
		panic(err)
	}
}

func RegisterMetrics(registerer prometheus.Registerer) error {
	return errors.Join(
		registerer.Register(objectCacheSize),
		registerer.Register(missCount),
		registerer.Register(hitCount),
		registerer.Register(evictCount),
	)
}

func init() {
	objectCacheSize = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "cache_size",
		Help: "number of objects in cache",
	}, []string{"name"})
	missCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "cache_miss_total",
		Help: "number of cache misses",
	}, []string{"name"})
	hitCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "cache_hit_total",
		Help: "number of cache hits",
	}, []string{"name"})
	evictCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "cache_evict_total",
		Help: "number of cache evictions",
	}, []string{"name"})
}

const DefaultMemoryDigestObjectCacheSize = 1000

type DigestObjectCache[K any, V any] interface {
	Load(key K, fallback func() (V, error)) (V, error)
}

func NewMemoryDigestObjectCache[K any, V any](name string, size int, onEvict func(k K, v V)) *MemoryDigestObjectCache[K, V] {
	return &MemoryDigestObjectCache[K, V]{
		name: name,
		cache: lru.NewWithEvictionFunc(size, func(key lru.Key, value interface{}) {
			objectCacheSize.WithLabelValues(name).Dec()
			evictCount.WithLabelValues(name).Inc()

			//nolint:forcetypeassert // we know the type is correct because we are the only ones setting it
			onEvict(key.(K), value.(V))
		}),
	}
}

type MemoryDigestObjectCache[K any, V any] struct {
	name  string
	cache *lru.Cache
}

func (m *MemoryDigestObjectCache[K, V]) Load(key K, fallback func() (V, error)) (V, error) {
	if m == nil || m.cache == nil {
		*m = *NewMemoryDigestObjectCache[K, V]("default", DefaultMemoryDigestObjectCacheSize, nil)
	}

	v, ok := m.cache.Get(key)
	if ok {
		hitCount.WithLabelValues(m.name).Inc()

		//nolint:forcetypeassert // we know the type is correct because we are the only ones setting it
		return v.(V), nil
	}
	missCount.WithLabelValues(m.name).Inc()
	v, err := fallback()
	if err != nil {
		return *new(V), err
	}

	m.cache.Add(key, v)
	objectCacheSize.WithLabelValues(m.name).Set(float64(m.cache.Len()))

	//nolint:forcetypeassert // we know the type is correct because we just added it
	return v.(V), nil
}
