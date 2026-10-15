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

// handshakeTransformer blocks every Transform call until it is released. The
// test uses it to prove overlap deterministically: it waits until two calls
// have entered Transform, which can only happen when the graph processor runs
// nodes in parallel.
type handshakeTransformer struct {
	delegate graphRuntime.Transformer
	entered  chan struct{}
	release  chan struct{}
}

func (t *handshakeTransformer) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	t.entered <- struct{}{}
	select {
	case <-t.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return t.delegate.Transform(ctx, step)
}

func (t *handshakeTransformer) awaitEntries(tb testing.TB, n int) {
	tb.Helper()
	for range n {
		select {
		case <-t.entered:
		case <-time.After(5 * time.Second):
			tb.Fatalf("timed out waiting for %d concurrent Transform calls", n)
		}
	}
}

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

func newConcurrencyBuilder(t *testing.T, transformer graphRuntime.Transformer) *Builder {
	t.Helper()
	scheme := runtime.NewScheme()
	scheme.MustRegisterScheme(testutils.Scheme)
	return NewBuilder(scheme).WithTransformer(&testutils.MockGetObjectTransformer{}, transformer)
}

func newDelegate(t *testing.T) *testutils.MockGetObject {
	t.Helper()
	scheme := runtime.NewScheme()
	scheme.MustRegisterScheme(testutils.Scheme)
	return &testutils.MockGetObject{Scheme: scheme}
}

func TestGraph_Process_RunsIndependentNodesConcurrently(t *testing.T) {
	r := require.New(t)

	hs := &handshakeTransformer{
		delegate: newDelegate(t),
		entered:  make(chan struct{}, 4),
		release:  make(chan struct{}),
	}
	b := newConcurrencyBuilder(t, hs).WithConcurrency(4)

	tgd := &v1alpha1.TransformationGraphDefinition{}
	r.NoError(yaml.Unmarshal([]byte(independentGraph(4)), tgd))

	graph, err := b.BuildAndCheck(tgd)
	r.NoError(err)

	done := make(chan error, 1)
	go func() {
		done <- graph.Process(t.Context())
	}()

	hs.awaitEntries(t, 2)
	close(hs.release)

	r.NoError(<-done)
}

func TestGraph_Process_RespectsConcurrencyLimitOne(t *testing.T) {
	r := require.New(t)

	rec := &concurrencyRecordingTransformer{delegate: newDelegate(t)}
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
	r.Equal(1, b.resolvedConcurrency(), "unset concurrency falls back to serial processing")

	r.Equal(6, b.WithConcurrency(6).resolvedConcurrency())
	r.Equal(1, b.WithConcurrency(0).resolvedConcurrency(), "non-positive resets to serial processing")
	r.Equal(1, b.WithConcurrency(-3).resolvedConcurrency())
}
