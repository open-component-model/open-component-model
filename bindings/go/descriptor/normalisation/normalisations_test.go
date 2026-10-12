package normalisation

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// stubNorm is a minimal Normalisation implementation for testing delegation.
type stubNorm struct {
	out []byte
	err error
}

func (s stubNorm) Normalise(*runtime.Descriptor) ([]byte, error) {
	return s.out, s.err
}

// recordingNorm captures the descriptor it is handed so tests can assert passthrough.
type recordingNorm struct {
	got *runtime.Descriptor
	out []byte
}

func (n *recordingNorm) Normalise(cd *runtime.Descriptor) ([]byte, error) {
	n.got = cd
	return n.out, nil
}

func newAlgorithms(entries map[string]Normalisation) *Algorithms {
	a := &Algorithms{algos: map[string]Normalisation{}}
	for k, v := range entries {
		a.algos[k] = v
	}
	return a
}

func TestAlgorithms_Register_and_Get(t *testing.T) {
	r := require.New(t)

	a := newAlgorithms(nil)
	r.Nil(a.Get("foo"), "Get on empty registry should return nil")

	stub := stubNorm{out: []byte("ok")}
	a.Register("foo", stub)
	r.Equal(stub, a.Get("foo"))

	// Overwrite existing entry.
	stub2 := stubNorm{out: []byte("replaced")}
	a.Register("foo", stub2)
	r.Equal(stub2, a.Get("foo"), "Register must overwrite existing entry")

	// Unknown key still nil.
	r.Nil(a.Get("bar"))
}

func TestAlgorithms_Names_sorted(t *testing.T) {
	r := require.New(t)

	a := newAlgorithms(nil)
	r.Empty(a.Names(), "empty registry returns empty slice")

	a.Register("zeta", stubNorm{})
	a.Register("alpha", stubNorm{})
	a.Register("mu", stubNorm{})

	r.Equal([]string{"alpha", "mu", "zeta"}, a.Names())
}

func TestAlgorithms_Normalise_unknown_algorithm(t *testing.T) {
	r := require.New(t)

	a := newAlgorithms(nil)
	_, err := a.Normalise(&runtime.Descriptor{}, "nonexistent-algo")
	r.ErrorIs(err, ErrUnknownNormalisationAlgorithm)
	r.Contains(err.Error(), "nonexistent-algo",
		"error message must contain the unknown algorithm name")
}

func TestAlgorithms_Normalise_delegates_result(t *testing.T) {
	r := require.New(t)

	rec := &recordingNorm{out: []byte(`{"canonical":true}`)}
	a := newAlgorithms(map[string]Normalisation{
		"test-algo": rec,
	})
	cd := &runtime.Descriptor{}

	got, err := a.Normalise(cd, "test-algo")
	r.NoError(err)
	r.Equal(rec.out, got)
	r.Same(cd, rec.got, "descriptor must be passed through to the algorithm")
}

func TestAlgorithms_Normalise_delegates_error(t *testing.T) {
	r := require.New(t)

	errInner := errors.New("inner failure")
	a := newAlgorithms(map[string]Normalisation{
		"fail-algo": stubNorm{err: errInner},
	})

	_, err := a.Normalise(&runtime.Descriptor{}, "fail-algo")
	r.ErrorIs(err, errInner)
}

func TestNormalise_package_level_unknown(t *testing.T) {
	r := require.New(t)

	// The package-level Normalise function delegates to global Normalisations.
	// With no algorithm registered under this name, it must fail.
	_, err := Normalise(&runtime.Descriptor{}, "definitely-not-registered")
	r.Error(err)
	r.ErrorIs(err, ErrUnknownNormalisationAlgorithm)
}

func TestNormalise_package_level_uses_global(t *testing.T) {
	r := require.New(t)

	const name = "test-global-algo"
	Normalisations.Register(name, stubNorm{out: []byte("global")})
	t.Cleanup(func() {
		Normalisations.Lock()
		defer Normalisations.Unlock()
		delete(Normalisations.algos, name)
	})

	got, err := Normalise(&runtime.Descriptor{}, name)
	r.NoError(err)
	r.Equal([]byte("global"), got)
}

// TestAlgorithms_concurrent_access only fails under -race if locking is broken.
func TestAlgorithms_concurrent_access(t *testing.T) {
	a := newAlgorithms(nil)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			a.Register("x", stubNorm{})
			_ = a.Get("x")
			_ = a.Names()
			_, _ = a.Normalise(&runtime.Descriptor{}, "x")
		})
	}
	wg.Wait()
}
