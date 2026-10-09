package cache

import (
	"bytes"
	"os"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	identityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
)

func TestCaches(t *testing.T) {
	identity := &identityv1.OCIRegistryIdentity{Hostname: "registry.example.com", Path: "org/repo"}
	other := &identityv1.OCIRegistryIdentity{Hostname: "registry.example.com", Path: "org/other"}

	t.Run("returns one instance per cache", func(t *testing.T) {
		r := require.New(t)
		caches := NewCaches(t.TempDir(), &Options{}, &Options{})

		blob := caches.Blob()
		r.NotNil(blob)
		r.Same(blob, caches.Blob())

		reference := caches.Reference(identity)
		r.NotNil(reference)
		r.Same(reference, caches.Reference(identity))
		r.NotSame(reference, caches.Reference(other))
	})

	t.Run("nil options disable the cache", func(t *testing.T) {
		r := require.New(t)
		caches := NewCaches(t.TempDir(), nil, nil)

		r.Nil(caches.Blob())
		r.Nil(caches.Reference(identity))
	})

	t.Run("close disables the caches", func(t *testing.T) {
		r := require.New(t)
		caches := NewCaches(t.TempDir(), &Options{}, &Options{})
		r.NotNil(caches.Blob())
		r.NotNil(caches.Reference(identity))

		caches.Close()
		caches.Close()

		r.Nil(caches.Blob())
		r.Nil(caches.Reference(identity))
		r.Nil(caches.Reference(other))
	})

	t.Run("close does not build unused caches", func(t *testing.T) {
		r := require.New(t)
		dir := t.TempDir()
		caches := NewCaches(dir, &Options{}, &Options{})

		caches.Close()

		entries, err := os.ReadDir(dir)
		r.NoError(err)
		r.Empty(entries)
	})

	t.Run("close stops the expiry goroutines", func(t *testing.T) {
		r := require.New(t)
		// Eventually runs its own goroutines, so count the LRU ones by stack instead.
		expiryGoroutines := func() int {
			var buf bytes.Buffer
			r.NoError(pprof.Lookup("goroutine").WriteTo(&buf, 2))
			return strings.Count(buf.String(), "golang-lru/v2/expirable.")
		}
		before := expiryGoroutines()

		caches := NewCaches(t.TempDir(), &Options{}, &Options{})
		r.NotNil(caches.Blob())
		r.NotNil(caches.Reference(identity))
		r.Greater(expiryGoroutines(), before)
		caches.Close()

		r.Eventually(func() bool {
			return expiryGoroutines() <= before
		}, 5*time.Second, 10*time.Millisecond)
	})
}
