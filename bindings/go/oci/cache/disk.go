package cache

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/opencontainers/go-digest"
)

// pathFor returns the deterministic on-disk path for a cached entry.
// The layout is dir/<algorithm>/<hex>, e.g. dir/sha256/abcd...ef.
func pathFor(dir string, dgst digest.Digest) string {
	return filepath.Join(dir, dgst.Algorithm().String(), dgst.Encoded())
}

// writeAtomic streams r into a fresh temp file under dir, fsyncs it,
// and renames it into pathFor(dir, dgst). It returns the final path
// and the number of bytes copied.
//
// If max > 0 and the copied byte count exceeds max, the temp file is
// removed and an error is returned. The destination file is never
// created in that case.
//
// Concurrent writers for the same digest race on the rename; the last
// rename wins. Both yield identical content, so the LRU bookkeeping is
// expected to dedupe via singleflight before reaching writeAtomic.
func writeAtomic(dir string, dgst digest.Digest, maxSize int64, r io.Reader) (path string, n int64, err error) {
	if err := dgst.Validate(); err != nil {
		return "", 0, fmt.Errorf("invalid digest: %w", err)
	}

	final := pathFor(dir, dgst)
	if err := os.MkdirAll(filepath.Dir(final), 0o700); err != nil {
		return "", 0, fmt.Errorf("create algo dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".incoming-*")
	if err != nil {
		return "", 0, fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	copyR := r
	if maxSize > 0 {
		// LimitReader stops at max+1 so we can detect oversize without
		// reading unbounded amounts.
		copyR = io.LimitReader(r, maxSize+1)
	}
	n, err = io.Copy(tmp, copyR)
	if err != nil {
		cleanup()
		return "", 0, fmt.Errorf("copy to temp file: %w", err)
	}
	if maxSize > 0 && n > maxSize {
		cleanup()
		return "", 0, fmt.Errorf("blob exceeds max size %d", maxSize)
	}

	if err := tmp.Sync(); err != nil {
		cleanup()
		return "", 0, fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", 0, fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, final); err != nil {
		_ = os.Remove(tmpName)
		return "", 0, fmt.Errorf("rename temp file: %w", err)
	}
	return final, n, nil
}

// removeQuiet removes path and silently ignores fs.ErrNotExist. Other
// errors are returned so the caller can log them.
func removeQuiet(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// ensureDir creates dir (and any missing parents) with 0o700
// permissions. It is a no-op if dir already exists.
func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("ensure dir: %w", err)
	}
	return nil
}

// scanExisting walks dir and yields every file that already lives at
// the canonical pathFor layout (dir/<algorithm>/<hex>). Files with
// unparseable names or that fail [digest.Validate] are removed so the
// cache does not carry over corruption from a previous run. The
// returned entries are not sorted.
func scanExisting(dir string) ([]existingEntry, error) {
	algoDirs, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read cache dir: %w", err)
	}
	var out []existingEntry
	for _, ad := range algoDirs {
		if !ad.IsDir() {
			continue
		}
		algo := ad.Name()
		algoPath := filepath.Join(dir, algo)
		entries, err := os.ReadDir(algoPath)
		if err != nil {
			return nil, fmt.Errorf("read algo dir %q: %w", algoPath, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			// Skip stray temp files left from a crashed write.
			if strings.HasPrefix(name, ".incoming-") {
				_ = os.Remove(filepath.Join(algoPath, name))
				continue
			}
			dgst := digest.NewDigestFromEncoded(digest.Algorithm(algo), name)
			if err := dgst.Validate(); err != nil {
				// unrecognised file; drop it so the cache directory
				// stays a clean digest-keyed store.
				_ = os.Remove(filepath.Join(algoPath, name))
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			out = append(out, existingEntry{
				digest: dgst,
				path:   filepath.Join(algoPath, name),
				size:   info.Size(),
			})
		}
	}
	return out, nil
}

// existingEntry describes a file discovered by [scanExisting].
type existingEntry struct {
	digest digest.Digest
	path   string
	size   int64
}
