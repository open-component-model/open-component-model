package main

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache"
)

// tracker watches the target kind of a run and records when each object is
// first seen ready. A watch instead of polling keeps the timing resolution
// independent of object count.
type tracker struct {
	expected int
	state    func(*unstructured.Unstructured) objectState
	stop     context.CancelFunc

	mu      sync.Mutex
	start   time.Time
	readyAt map[string]time.Time
	last    map[string]objectState
	done    chan struct{}
}

func startTracker(ctx context.Context, c *cluster, w workload, gvk schema.GroupVersionKind,
	state func(*unstructured.Unstructured) objectState, expected int,
) (_ *tracker, err error) {
	t := &tracker{
		expected: expected,
		state:    state,
		readyAt:  map[string]time.Time{},
		last:     map[string]objectState{},
		done:     make(chan struct{}),
	}
	ctx, t.stop = context.WithCancel(ctx)
	defer func() {
		if err != nil {
			t.stop()
		}
	}()

	informers, err := cache.New(c.cfg, cache.Options{
		Scheme:               c.scheme,
		DefaultNamespaces:    map[string]cache.Config{w.namespace: {}},
		DefaultLabelSelector: labels.SelectorFromSet(labels.Set{runLabel: w.runID}),
	})
	if err != nil {
		return nil, fmt.Errorf("creating cache: %w", err)
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	inf, err := informers.GetInformer(ctx, obj)
	if err != nil {
		return nil, fmt.Errorf("getting informer for %s: %w", gvk.Kind, err)
	}
	if _, err := inf.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
		AddFunc:    t.observe,
		UpdateFunc: func(_, obj any) { t.observe(obj) },
	}); err != nil {
		return nil, fmt.Errorf("adding event handler: %w", err)
	}

	go func() { _ = informers.Start(ctx) }()
	if !informers.WaitForCacheSync(ctx) {
		return nil, fmt.Errorf("cache for %s did not sync", gvk.Kind)
	}
	return t, nil
}

// markStart sets the reference time that all readiness latencies are measured from.
func (t *tracker) markStart() time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.start = time.Now()
	return t.start
}

func (t *tracker) observe(obj any) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return
	}
	s := t.state(u)

	t.mu.Lock()
	defer t.mu.Unlock()
	t.last[u.GetName()] = s
	if !s.ready {
		return
	}
	if _, seen := t.readyAt[u.GetName()]; seen {
		return
	}
	t.readyAt[u.GetName()] = time.Now()
	if len(t.readyAt) == t.expected {
		close(t.done)
	}
}

// wait blocks until every expected object was ready once, or the timeout hits.
func (t *tracker) wait(ctx context.Context, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-t.done:
		return true
	case <-timer.C:
		return false
	case <-ctx.Done():
		return false
	}
}

type trackerSummary struct {
	ready          int
	failed         int
	pending        int
	failureReasons map[string]int
	// latencies are the sorted times from start until each object was ready.
	latencies []time.Duration
}

func (t *tracker) summary() trackerSummary {
	t.mu.Lock()
	defer t.mu.Unlock()

	s := trackerSummary{ready: len(t.readyAt), failureReasons: map[string]int{}}
	for _, at := range t.readyAt {
		s.latencies = append(s.latencies, at.Sub(t.start))
	}
	slices.Sort(s.latencies)

	for name, st := range t.last {
		if _, ok := t.readyAt[name]; ok {
			continue
		}
		if st.failed {
			s.failed++
			s.failureReasons[st.reason]++
		}
	}
	// Objects never observed at all count as pending too.
	s.pending = t.expected - s.ready - s.failed
	return s
}
