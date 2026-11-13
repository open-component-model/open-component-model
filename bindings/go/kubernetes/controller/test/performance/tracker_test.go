package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func newTestTracker(expected int, state func(*unstructured.Unstructured) objectState) *tracker {
	tr := &tracker{
		expected: expected,
		state:    state,
		readyAt:  map[string]time.Time{},
		last:     map[string]objectState{},
		done:     make(chan struct{}),
	}
	tr.markStart()
	return tr
}

func requireOpen(r *require.Assertions, tr *tracker) {
	select {
	case <-tr.done:
		r.Fail("tracker must not finish before all objects are done")
	default:
	}
}

func TestTrackerSummary(t *testing.T) {
	r := require.New(t)
	tr := newTestTracker(4, readyState)

	obj := func(name, status, reason string) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{
			"metadata": map[string]any{"name": name},
			"status": map[string]any{"conditions": []any{
				map[string]any{"type": "Ready", "status": status, "reason": reason},
			}},
		}}
	}
	tr.observe(obj("a", "False", "Progressing"), false)
	tr.observe(obj("a", "True", "Succeeded"), false)
	tr.observe(obj("b", "True", "Succeeded"), false)
	// Once ready, a later failure does not undo the recorded time.
	tr.observe(obj("b", "False", "Broken"), false)
	tr.observe(obj("c", "False", "GetComponentVersionFailed"), false)

	sum := tr.summary()
	r.Equal(2, sum.ready)
	r.Equal(1, sum.failed)
	r.Equal(1, sum.pending, "d was never observed")
	r.Equal(map[string]int{"GetComponentVersionFailed": 1}, sum.failureReasons)
	r.Len(sum.latencies, 2)
	r.LessOrEqual(sum.latencies[0], sum.latencies[1])

	requireOpen(r, tr)
	tr.observe(obj("c", "True", "Succeeded"), false)
	tr.observe(obj("d", "True", "Succeeded"), false)
	r.True(tr.wait(t.Context(), time.Second))
}

func TestTrackerIgnoresTheInitialList(t *testing.T) {
	cm := func(name, version string) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{
			"metadata": map[string]any{"name": name, "namespace": "ns"},
			"data":     map[string]any{"version": version},
		}}
	}

	t.Run("objects done before the change are pending", func(t *testing.T) {
		r := require.New(t)
		tr := newTestTracker(2, configMapVersionState("1.0.0"))
		tr.observe(cm("a", "1.0.0"), true)
		tr.observe(cm("b", "1.0.0"), true)

		sum := tr.summary()
		r.Zero(sum.ready)
		r.Equal(2, sum.pending)
		requireOpen(r, tr)
	})

	t.Run("changed and changed back", func(t *testing.T) {
		r := require.New(t)
		tr := newTestTracker(2, configMapVersionState("1.0.0"))
		for _, name := range []string{"a", "b"} {
			tr.observe(cm(name, "1.0.0"), true)
			tr.observe(cm(name, driftedVersion), false)
			requireOpen(r, tr)
			tr.observe(cm(name, "1.0.0"), false)
		}
		r.True(tr.wait(t.Context(), time.Second))
		r.Len(tr.summary().latencies, 2)
	})

	t.Run("the change itself was not seen", func(t *testing.T) {
		r := require.New(t)
		tr := newTestTracker(2, configMapVersionState("1.0.0"))
		for _, name := range []string{"a", "b"} {
			tr.observe(cm(name, "1.0.0"), true)
			// Events can be coalesced: an update at the done state is enough.
			tr.observe(cm(name, "1.0.0"), false)
		}
		r.True(tr.wait(t.Context(), time.Second))
	})
}

func TestTrackerKeysByNamespace(t *testing.T) {
	r := require.New(t)
	tr := newTestTracker(2, readyState)
	for _, ns := range []string{"t0", "t1"} {
		tr.observe(&unstructured.Unstructured{Object: map[string]any{
			"metadata": map[string]any{"name": "d-0000", "namespace": ns},
			"status":   map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "True"}}},
		}}, false)
	}
	r.True(tr.wait(t.Context(), time.Second), "objects of the same name in different namespaces are different objects")
}
