//go:build !race

// The race detector makes sync.Pool drop items at random, so pooled-buffer reuse
// is only measurable without it.

package blob_test

import (
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
)

func TestCopyReaderIntoFileReusesPooledBuffer(t *testing.T) {
	r := require.New(t)
	data := strings.Repeat("0123456789abcdef", 64*1024) // 1 MiB
	f, err := os.CreateTemp(t.TempDir(), "")
	r.NoError(err)
	t.Cleanup(func() { r.NoError(f.Close()) })

	copyOnce := func() {
		_, err := f.Seek(0, io.SeekStart)
		r.NoError(err)
		r.NoError(f.Truncate(0))
		_, err = blob.CopyReader(f, struct{ io.Reader }{strings.NewReader(data)})
		r.NoError(err)
	}

	// Disable GC so the pooled buffer cannot be dropped mid-measurement.
	defer debug.SetGCPercent(debug.SetGCPercent(-1))
	copyOnce() // warm the pool

	const runs = 20
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range runs {
		copyOnce()
	}
	runtime.ReadMemStats(&after)
	// os.File.ReadFrom's generic fallback would allocate a fresh 32 KiB buffer per copy.
	r.Less((after.TotalAlloc-before.TotalAlloc)/runs, uint64(4096))
}
