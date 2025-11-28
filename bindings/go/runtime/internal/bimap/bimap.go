package bimap

import (
	"iter"
	"maps"
)

func New[K comparable, V comparable]() *Map[K, V] {
	return &Map[K, V]{
		forward: make(map[K]V),
		reverse: make(map[V]K),
	}
}

type Map[K comparable, V comparable] struct {
	forward map[K]V
	reverse map[V]K
}

func (m *Map[K, V]) Iter() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for k, v := range m.forward {
			if !yield(k, v) {
				return
			}
		}
	}
}

func (m *Map[K, V]) Set(k K, v V) {
	m.forward[k] = v
	m.reverse[v] = k
}

func (m *Map[K, V]) Get(k K) (V, bool) {
	v, ok := m.forward[k]
	return v, ok
}

func (m *Map[K, V]) GetByValue(v V) (K, bool) {
	k, ok := m.reverse[v]
	return k, ok
}

func (m *Map[K, V]) Keys() iter.Seq[K] {
	return func(yield func(K) bool) {
		for k := range m.forward {
			if !yield(k) {
				return
			}
		}
	}
}

func (m *Map[K, V]) Len() int {
	return len(m.forward)
}

func (m *Map[K, V]) Clone() *Map[K, V] {
	n := Map[K, V]{}
	n.forward = maps.Clone(m.forward)
	n.reverse = maps.Clone(m.reverse)
	return &n
}
