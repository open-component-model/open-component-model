package bimap_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/runtime/internal/bimap"
)

func TestSetAndGet(t *testing.T) {
	r := require.New(t)

	m := bimap.New[string, int]()

	m.Set("a", 1)
	m.Set("b", 2)

	right, ok := m.GetLeft("a")
	r.True(ok)
	r.Equal(1, right)

	left, ok := m.GetRight(2)
	r.True(ok)
	r.Equal("b", left)

	r.Equal(2, m.Len())
}

func TestOverwrite(t *testing.T) {
	t.Run("Left", func(t *testing.T) {
		r := require.New(t)

		m := bimap.New[string, int]()

		m.Set("a", 1)
		m.Set("a", 2) // overwrite right side of "a"

		v, ok := m.GetLeft("a")
		r.True(ok)
		r.Equal(2, v)

		_, ok = m.GetRight(1)
		r.False(ok)

		l, ok := m.GetRight(2)
		r.True(ok)
		r.Equal("a", l)

		r.Equal(1, m.Len())
	})

	t.Run("Right", func(t *testing.T) {
		r := require.New(t)

		m := bimap.New[string, int]()

		m.Set("a", 1)
		m.Set("b", 1) // overwrite left side of right value 1

		l, ok := m.GetRight(1)
		r.True(ok)
		r.Equal("b", l)

		_, ok = m.GetLeft("a")
		r.False(ok)

		v, ok := m.GetLeft("b")
		r.True(ok)
		r.Equal(1, v)

		r.Equal(1, m.Len())
	})
}

func TestIterOrder(t *testing.T) {
	r := require.New(t)

	m := bimap.New[string, int]()

	m.Set("a", 1)
	m.Set("b", 2)
	m.Set("c", 3)

	var seen []struct {
		L string
		R int
	}

	for l, v := range m.Iter() {
		seen = append(seen, struct {
			L string
			R int
		}{l, v})
	}

	r.Equal([]struct {
		L string
		R int
	}{
		{"a", 1},
		{"b", 2},
		{"c", 3},
	}, seen)
}

func TestClone(t *testing.T) {
	r := require.New(t)

	m := bimap.New[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)

	clone := m.Clone()

	// same content
	v, ok := clone.GetLeft("a")
	r.True(ok)
	r.Equal(1, v)

	v, ok = clone.GetLeft("b")
	r.True(ok)
	r.Equal(2, v)

	r.Equal(2, clone.Len())

	// but independent storage
	m.Set("c", 3)
	r.Equal(3, m.Len())
	r.Equal(2, clone.Len())
}

func TestGetMissing(t *testing.T) {
	r := require.New(t)

	m := bimap.New[string, int]()

	_, ok := m.GetLeft("missing-left")
	r.False(ok)

	_, ok = m.GetRight(999)
	r.False(ok)
}
