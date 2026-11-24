package ocm

import (
	"time"

	"golang.org/x/time/rate"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	DefaultRateLimitQPS   = 10
	DefaultRateLimitBurst = 100
)

// RateLimit configures the token bucket every rate-limited requeue of a controller passes through.
// The zero value uses the defaults.
type RateLimit struct {
	QPS   float64
	Burst int
}

// Limiter returns a new workqueue rate limiter; every controller needs its own.
func (l RateLimit) Limiter() workqueue.TypedRateLimiter[reconcile.Request] {
	qps, burst := l.QPS, l.Burst
	if qps <= 0 {
		qps = DefaultRateLimitQPS
	}
	if burst <= 0 {
		burst = DefaultRateLimitBurst
	}
	return workqueue.NewTypedMaxOfRateLimiter(
		workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](5*time.Millisecond, 5*time.Minute),
		&workqueue.TypedBucketRateLimiter[reconcile.Request]{Limiter: rate.NewLimiter(rate.Limit(qps), burst)},
	)
}
