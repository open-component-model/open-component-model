// Package bimap provides a compact bidirectional map implementation
// with O(1) lookup from left→right and right→left.
// Values are stored only once internally.
package bimap

import "iter"

// Map is a bidirectional mapping between a Left and Right value.
// Each Left maps to exactly one Right and vice-versa.
type Map[L, R comparable] struct {
	pairs []pair[L, R]
	index index[L, R]
}

type index[L, R comparable] struct {
	left  map[L]int
	right map[R]int
}

type pair[L, R comparable] struct {
	left  L
	right R
}

// New creates an empty Map.
// Both directions support O(1) lookup.
func New[L, R comparable]() *Map[L, R] {
	return &Map[L, R]{
		index: index[L, R]{left: make(map[L]int), right: make(map[R]int)},
	}
}

// Set inserts or updates the mapping for left↔right.
// If left already exists, its old right mapping is replaced.
// If right already exists, its old left mapping is replaced.
// If neither exists, a new pair is appended.
// Exactly one final mapping per left and right is maintained.
func (m *Map[L, R]) Set(left L, right R) {
	if i, ok := m.index.left[left]; ok {
		delete(m.index.right, m.pairs[i].right)
		m.pairs[i].right = right
		m.index.right[right] = i
		return
	}
	if i, ok := m.index.right[right]; ok {
		delete(m.index.left, m.pairs[i].left)
		m.pairs[i].left = left
		m.index.left[left] = i
		return
	}
	i := len(m.pairs)
	m.pairs = append(m.pairs, pair[L, R]{left, right})
	m.index.left[left] = i
	m.index.right[right] = i
}

// GetLeft returns the Right value associated with l.
// The boolean indicates whether the mapping exists.
func (m *Map[L, R]) GetLeft(l L) (R, bool) {
	i, ok := m.index.left[l]
	if ok {
		return m.pairs[i].right, true
	}
	var zero R
	return zero, false
}

// GetRight returns the Left value associated with r.
// The boolean indicates whether the mapping exists.
func (m *Map[L, R]) GetRight(r R) (L, bool) {
	i, ok := m.index.right[r]
	if ok {
		return m.pairs[i].left, true
	}
	var zero L
	return zero, false
}

// Len returns the number of stored left↔right mappings.
func (m *Map[L, R]) Len() int {
	return len(m.pairs)
}

// Iter returns an iterator over all (left, right) pairs.
// Iteration order is stable based on insertion/update order.
func (m *Map[L, R]) Iter() iter.Seq2[L, R] {
	return func(yield func(L, R) bool) {
		for _, pair := range m.pairs {
			if !yield(pair.left, pair.right) {
				return
			}
		}
	}
}

// Clone returns a deep copy of the Map,
// preserving all mappings while maintaining new internal storage.
func (m *Map[L, R]) Clone() *Map[L, R] {
	clone := New[L, R]()
	for l, r := range m.Iter() {
		clone.Set(l, r)
	}
	return clone
}
