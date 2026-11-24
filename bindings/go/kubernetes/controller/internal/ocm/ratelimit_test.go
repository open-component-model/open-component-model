package ocm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestRateLimitLimiter(t *testing.T) {
	// The bucket delays the first item past its burst by 1/QPS.
	tests := []struct {
		name  string
		limit RateLimit
		burst int
		want  time.Duration
	}{
		{name: "zero value uses the defaults", burst: DefaultRateLimitBurst, want: time.Second / DefaultRateLimitQPS},
		{name: "configured", limit: RateLimit{QPS: 2, Burst: 3}, burst: 3, want: time.Second / 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			limiter := tc.limit.Limiter()
			request := func(i int) reconcile.Request {
				return reconcile.Request{NamespacedName: types.NamespacedName{Name: string(rune('a' + i))}}
			}
			for i := range tc.burst {
				r.Less(limiter.When(request(i)), 50*time.Millisecond, "items within the burst only see the per-item backoff")
			}
			r.InDelta(tc.want, limiter.When(request(tc.burst)), float64(50*time.Millisecond))
		})
	}
}
