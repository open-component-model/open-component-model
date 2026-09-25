package builder

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/graph/internal/testutils"
	graphRuntime "ocm.software/open-component-model/bindings/go/transform/graph/runtime"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

// concurrencyRecordingTransformer wraps another transformer and records how many
// Transform calls run at the same time.
type concurrencyRecordingTransformer struct {
	delegate      graphRuntime.Transformer
	current       int32
	maxConcurrent int32
	hold          time.Duration
}

func (t *concurrencyRecordingTransformer) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	cur := atomic.AddInt32(&t.current, 1)
	defer atomic.AddInt32(&t.current, -1)
	for {
		observed := atomic.LoadInt32(&t.maxConcurrent)
		if cur <= observed || atomic.CompareAndSwapInt32(&t.maxConcurrent, observed, cur) {
			break
		}
	}
	time.Sleep(t.hold)
	return t.delegate.Transform(ctx, step)
}

// independentGraph builds a spec with n independent get transformations. They
// share no dependency, so a topological processor places them in a single
// batch where they may run in parallel.
func independentGraph(n int) string {
	var b strings.Builder
	b.WriteString("environment:\n  name: \"obj\"\n  version: \"1.0.0\"\ntransformations:\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "- id: get%d\n  type: MockGetObjectTransformer/v1alpha1\n  spec:\n    name: \"${environment.name}\"\n    version: \"${environment.version}\"\n", i)
	}
	return b.String()
}

func newConcurrencyBuilder(t *testing.T, rec *concurrencyRecordingTransformer) *Builder {
	t.Helper()
	scheme := runtime.NewScheme()
	scheme.MustRegisterScheme(testutils.Scheme)
	rec.delegate = &testutils.MockGetObject{Scheme: scheme}
	return NewBuilder(scheme).WithTransformer(&testutils.MockGetObjectTransformer{}, rec)
}

func TestGraph_Process_RunsIndependentNodesConcurrently(t *testing.T) {
	r := require.New(t)

	rec := &concurrencyRecordingTransformer{hold: 50 * time.Millisecond}
	b := newConcurrencyBuilder(t, rec).WithConcurrency(4)

	tgd := &v1alpha1.TransformationGraphDefinition{}
	r.NoError(yaml.Unmarshal([]byte(independentGraph(8)), tgd))

	graph, err := b.BuildAndCheck(tgd)
	r.NoError(err)

	r.NoError(graph.Process(t.Context()))

	max := atomic.LoadInt32(&rec.maxConcurrent)
	r.Greater(int(max), 1, "independent nodes should run in parallel")
	r.LessOrEqual(int(max), 4, "must never exceed the configured concurrency limit")
}

func TestGraph_Process_RespectsConcurrencyLimitOne(t *testing.T) {
	r := require.New(t)

	rec := &concurrencyRecordingTransformer{hold: 20 * time.Millisecond}
	b := newConcurrencyBuilder(t, rec).WithConcurrency(1)

	tgd := &v1alpha1.TransformationGraphDefinition{}
	r.NoError(yaml.Unmarshal([]byte(independentGraph(6)), tgd))

	graph, err := b.BuildAndCheck(tgd)
	r.NoError(err)

	r.NoError(graph.Process(t.Context()))

	r.Equal(int32(1), atomic.LoadInt32(&rec.maxConcurrent), "concurrency limit 1 must serialize processing")
}

func TestBuilder_DefaultConcurrency(t *testing.T) {
	r := require.New(t)
	b := NewBuilder(runtime.NewScheme())
	r.Equal(DefaultConcurrency, b.resolvedConcurrency(), "unset concurrency falls back to the default")
	r.Positive(DefaultConcurrency)

	r.Equal(6, b.WithConcurrency(6).resolvedConcurrency())
	r.Equal(DefaultConcurrency, b.WithConcurrency(0).resolvedConcurrency(), "non-positive resets to default")
	r.Equal(DefaultConcurrency, b.WithConcurrency(-3).resolvedConcurrency())
}
