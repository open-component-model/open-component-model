package builder

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/runtime"
	graphRuntime "ocm.software/open-component-model/bindings/go/transform/graph/runtime"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

var errTransient = errors.New("transient")

// flakyTransformer fails its first `failures` calls with err, then delegates.
type flakyTransformer struct {
	delegate graphRuntime.Transformer
	failures int32
	err      error
	calls    atomic.Int32
}

func (t *flakyTransformer) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	if t.calls.Add(1) <= t.failures {
		return nil, t.err
	}
	return t.delegate.Transform(ctx, step)
}

func TestGraph_Process_RetriesTransientTransformErrors(t *testing.T) {
	policy := &graphRuntime.RetryPolicy{
		MaxAttempts: 3,
		Retryable:   func(err error) bool { return errors.Is(err, errTransient) },
	}
	errPermanent := errors.New("permanent")
	cases := []struct {
		name      string
		failures  int32
		err       error
		policy    *graphRuntime.RetryPolicy
		wantErr   error
		wantCalls int32
	}{
		{name: "transient failure is retried until success", failures: 2, err: errTransient, policy: policy, wantCalls: 3},
		{name: "attempts are bounded", failures: 5, err: errTransient, policy: policy, wantErr: errTransient, wantCalls: 3},
		{name: "non-transient failure is not retried", failures: 1, err: errPermanent, policy: policy, wantErr: errPermanent, wantCalls: 1},
		{name: "no policy means no retry", failures: 1, err: errTransient, wantErr: errTransient, wantCalls: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			ft := &flakyTransformer{delegate: newDelegate(t), failures: tc.failures, err: tc.err}
			b := newConcurrencyBuilder(t, ft)
			if tc.policy != nil {
				b = b.WithRetry(tc.policy)
			}
			tgd := &v1alpha1.TransformationGraphDefinition{}
			r.NoError(yaml.Unmarshal([]byte(independentGraph(1)), tgd))
			graph, err := b.BuildAndCheck(tgd)
			r.NoError(err)

			err = graph.Process(t.Context())
			if tc.wantErr != nil {
				r.ErrorIs(err, tc.wantErr)
			} else {
				r.NoError(err)
			}
			r.Equal(tc.wantCalls, ft.calls.Load())
		})
	}

	t.Run("cancellation stops retrying", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithCancel(t.Context())
		ft := &flakyTransformer{delegate: newDelegate(t), failures: 100, err: errTransient}
		b := newConcurrencyBuilder(t, &cancelOnCall{inner: ft, cancel: cancel}).WithRetry(policy)
		tgd := &v1alpha1.TransformationGraphDefinition{}
		r.NoError(yaml.Unmarshal([]byte(independentGraph(1)), tgd))
		graph, err := b.BuildAndCheck(tgd)
		r.NoError(err)
		r.Error(graph.Process(ctx))
		r.Equal(int32(1), ft.calls.Load())
	})

	t.Run("cancellation during backoff returns the transform error", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithCancel(t.Context())
		ft := &flakyTransformer{delegate: newDelegate(t), failures: 100, err: errTransient}
		b := newConcurrencyBuilder(t, ft).WithRetry(&graphRuntime.RetryPolicy{
			MaxAttempts: 3,
			Backoff:     time.Hour,
			Retryable:   policy.Retryable,
		})
		tgd := &v1alpha1.TransformationGraphDefinition{}
		r.NoError(yaml.Unmarshal([]byte(independentGraph(1)), tgd))
		graph, err := b.BuildAndCheck(tgd)
		r.NoError(err)

		done := make(chan error, 1)
		go func() { done <- graph.Process(ctx) }()
		r.Eventually(func() bool { return ft.calls.Load() == 1 }, 5*time.Second, time.Millisecond)
		cancel()
		select {
		case err := <-done:
			r.ErrorIs(err, errTransient, "the caller sees why the node failed, not just the cancellation")
		case <-time.After(5 * time.Second):
			r.FailNow("Process kept waiting out the backoff after cancellation")
		}
		r.Equal(int32(1), ft.calls.Load())
	})

	t.Run("backoff doubles between attempts", func(t *testing.T) {
		r := require.New(t)
		var stamps []time.Time
		ft := &flakyTransformer{delegate: newDelegate(t), failures: 2, err: errTransient}
		rec := &stampTransformer{inner: ft, stamps: &stamps}
		b := newConcurrencyBuilder(t, rec).WithRetry(&graphRuntime.RetryPolicy{
			MaxAttempts: 3,
			Backoff:     50 * time.Millisecond,
			Retryable:   policy.Retryable,
		})
		tgd := &v1alpha1.TransformationGraphDefinition{}
		r.NoError(yaml.Unmarshal([]byte(independentGraph(1)), tgd))
		graph, err := b.BuildAndCheck(tgd)
		r.NoError(err)
		r.NoError(graph.Process(t.Context()))
		r.Len(stamps, 3)
		r.GreaterOrEqual(stamps[1].Sub(stamps[0]), 50*time.Millisecond)
		r.GreaterOrEqual(stamps[2].Sub(stamps[1]), 100*time.Millisecond)
	})
}

type cancelOnCall struct {
	inner  graphRuntime.Transformer
	cancel context.CancelFunc
}

func (c *cancelOnCall) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	defer c.cancel()
	return c.inner.Transform(ctx, step)
}

type stampTransformer struct {
	inner  graphRuntime.Transformer
	stamps *[]time.Time
}

func (s *stampTransformer) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	*s.stamps = append(*s.stamps, time.Now())
	return s.inner.Transform(ctx, step)
}
