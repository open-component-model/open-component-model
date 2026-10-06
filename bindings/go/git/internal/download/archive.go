package download

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

const mediaTypeTGZ = "application/x-tgz"

// archive writes the commit tree as a reproducible tar.gz into file and closes it.
// The file belongs to the caller.
func archive(ctx context.Context, commit *object.Commit, file *os.File, opts Options) (_ *filesystem.Blob, _ digest.Digest, err error) {
	tree, err := commit.Tree()
	if err != nil {
		return nil, "", errors.Join(fmt.Errorf("cannot read git tree: %w", err), file.Close())
	}

	digester := digest.Canonical.Digester()
	limited := &limitedWriter{Writer: file, limit: opts.MaxArchiveSize}
	gz := gzip.NewWriter(io.MultiWriter(limited, digester.Hash()))
	tw := tar.NewWriter(gz)
	err = filesystem.WriteTar(ctx, treeFS{tree: tree}, tw, filesystem.DirOptions{
		Reproducible:         true,
		PreserveSymlinks:     true,
		OmitRoot:             true,
		OmitDirTrailingSlash: true,
	})
	err = errors.Join(err, tw.Close(), gz.Close(), file.Close())
	if err != nil {
		return nil, "", fmt.Errorf("cannot create git archive: %w", err)
	}

	b, err := filesystem.GetBlobFromOSPath(file.Name())
	if err != nil {
		return nil, "", fmt.Errorf("cannot open git archive file %q: %w", file.Name(), err)
	}

	b.SetMediaType(mediaTypeTGZ)

	return b, digester.Digest(), nil
}

// ArchiveDigest computes the same digest as Download without retaining its archive.
func ArchiveDigest(ctx context.Context, commit *object.Commit, opts Options) (digest.Digest, error) {
	file, err := os.CreateTemp(opts.TempDir, "ocm-git-digest-*.tar.gz")
	if err != nil {
		return "", fmt.Errorf("cannot create git digest file: %w", err)
	}
	defer func() {
		if err := os.Remove(file.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.WarnContext(ctx, "failed to remove git digest file", "path", file.Name(), "err", err)
		}
	}()
	_, archiveDigest, err := archive(ctx, commit, file, opts)
	return archiveDigest, err
}

type limitedWriter struct {
	io.Writer
	limit, written int64
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.limit > 0 && int64(len(p)) > w.limit-w.written {
		return 0, fmt.Errorf("git archive exceeds the maximum size of %d bytes", w.limit)
	}

	n, err := w.Writer.Write(p)
	w.written += int64(n)

	return n, err
}
