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
		// Caches closed by earlier tests stop their goroutines asynchronously, so a plain
		// count moves on its own. Track the goroutines this test starts by header instead.
		expiryGoroutines := func() map[string]bool {
			var buf bytes.Buffer
			r.NoError(pprof.Lookup("goroutine").WriteTo(&buf, 2))
			found := map[string]bool{}
			for stack := range strings.SplitSeq(buf.String(), "\n\n") {
				if strings.Contains(stack, "golang-lru/v2/expirable.") {
					header, _, _ := strings.Cut(stack, " [")
					found[header] = true
				}
			}
			return found
		}
		before := expiryGoroutines()
		started := func() []string {
			var ids []string
			for id := range expiryGoroutines() {
				if !before[id] {
					ids = append(ids, id)
				}
			}
			return ids
		}

		caches := NewCaches(t.TempDir(), &Options{}, &Options{})
		r.NotNil(caches.Blob())
		r.NotNil(caches.Reference(identity))
		r.NotEmpty(started())
		caches.Close()

		r.Eventually(func() bool {
			return len(started()) == 0
		}, 5*time.Second, 10*time.Millisecond)
	})
}
