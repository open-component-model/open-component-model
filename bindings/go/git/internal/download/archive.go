package download

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/opencontainers/go-digest"
)

// archive writes the files of the commit tree as tar, so names, modes and symlinks
// come from git and not from a checkout on the host file system. The archive stays
// uncompressed: its digest is pinned into the descriptor and verified on other
// machines, and compress/flate output is not stable across Go releases.
// Directories are implied by the file paths, submodules are not part of the tree content.
func archive(ctx context.Context, commit *object.Commit, opts Options) (_ *Blob, err error) {
	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("cannot read git tree: %w", err)
	}

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
	tw := tar.NewWriter(&limitedWriter{Writer: io.MultiWriter(file, digester.Hash()), limit: opts.MaxDownloadSize})
	err = tree.Files().ForEach(func(f *object.File) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		return writeFile(tw, f)
	})
	err = errors.Join(err, tw.Close(), file.Close())
	if err != nil {
		return nil, fmt.Errorf("cannot create git archive: %w", err)
	}

	return newBlob(file.Name(), digester.Digest().String())
}

func writeFile(tw *tar.Writer, f *object.File) error {
	header := &tar.Header{Name: f.Name, ModTime: time.Unix(0, 0)}
	switch f.Mode {
	case filemode.Symlink:
		target, err := f.Contents()
		if err != nil {
			return err
		}

		header.Typeflag, header.Mode, header.Linkname = tar.TypeSymlink, 0o777, target
		return tw.WriteHeader(header)
	case filemode.Executable:
		header.Mode = 0o755
	default:
		header.Mode = 0o644
	}

	header.Typeflag, header.Size = tar.TypeReg, f.Size
	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	rc, err := f.Reader()
	if err != nil {
		return err
	}

	_, err = io.Copy(tw, rc)
	return errors.Join(err, rc.Close())
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
