package sync

import (
	"cmp"
	"sync"

	"ocm.software/open-component-model/bindings/go/dag"
)

func NewSyncedDirectedAcyclicGraph[T cmp.Ordered]() *SyncedDirectedAcyclicGraph[T] {
	return &SyncedDirectedAcyclicGraph[T]{
		dag: dag.NewDirectedAcyclicGraph[T](),
	}
}

func ToSyncedGraph[T cmp.Ordered](d *dag.DirectedAcyclicGraph[T]) *SyncedDirectedAcyclicGraph[T] {
	return &SyncedDirectedAcyclicGraph[T]{
		dag: d,
	}
}

type SyncedDirectedAcyclicGraph[T cmp.Ordered] struct {
	dagMu sync.RWMutex
	dag   *dag.DirectedAcyclicGraph[T]
}

func (g *SyncedDirectedAcyclicGraph[T]) WithReadLock(fn func(d *dag.DirectedAcyclicGraph[T]) error) error {
	g.dagMu.RLock()
	defer g.dagMu.RUnlock()
	return fn(g.dag)
}

func (g *SyncedDirectedAcyclicGraph[T]) WithWriteLock(fn func(d *dag.DirectedAcyclicGraph[T]) error) error {
	g.dagMu.Lock()
	defer g.dagMu.Unlock()
	return fn(g.dag)
}
