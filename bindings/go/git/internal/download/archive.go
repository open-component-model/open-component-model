package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

func archive(ctx context.Context, root string, opts Options) (_ *Blob, err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// The archive stays uncompressed: its digest is pinned into the descriptor and
	// verified on other machines, and compress/flate output is not stable across Go releases.
	tree, err := filesystem.GetBlobFromPath(ctx, root, filesystem.DirOptions{
		Reproducible:     true,
		PreserveSymlinks: true,
		ExcludePatterns:  []string{".git"},
	})
	if err != nil {
		return nil, fmt.Errorf("cannot create git archive: %w", err)
	}

	rc, err := tree.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("cannot create git archive: %w", err)
	}
	defer func() {
		if err != nil {
			// The tar is written by a goroutine that only checks ctx between entries and
			// blocks on the pipe until read, so drain it after cancelling to let it exit.
			cancel()
			_, _ = io.Copy(io.Discard, rc)
		}
		err = errors.Join(err, rc.Close())
	}()

	file, err := os.CreateTemp(opts.TempDir, "ocm-git-archive-*.tar")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(file.Name())
		}
	}()

	digester := digest.SHA256.Digester()
	writer := &limitedWriter{Writer: io.MultiWriter(file, digester.Hash()), limit: opts.MaxDownloadSize}
	_, err = io.Copy(writer, &contextReader{ctx: ctx, Reader: rc})
	err = errors.Join(err, file.Close())
	if err != nil {
		return nil, fmt.Errorf("cannot create git archive: %w", err)
	}

	return newBlob(file.Name(), digester.Digest().String())
}

type contextReader struct {
	ctx context.Context
	io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}

	return r.Reader.Read(p)
}

type limitedWriter struct {
	io.Writer
	limit, written int64
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.limit > 0 && int64(len(p)) > w.limit-w.written {
		return 0, fmt.Errorf("git archive exceeds maximum download size of %d bytes", w.limit)
	}

	n, err := w.Writer.Write(p)
	w.written += int64(n)

	return n, err
}
