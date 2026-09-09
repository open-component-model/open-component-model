package download

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/opencontainers/go-digest"
)

func archive(ctx context.Context, root string, opts Options) (_ *Blob, err error) {
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
	// The archive stays uncompressed: its digest is pinned into the descriptor and
	// verified on other machines, and compress/flate output is not stable across Go releases.
	tw := tar.NewWriter(writer)
	err = writeWorktree(ctx, tw, root)
	err = errors.Join(err, tw.Close(), file.Close())
	if err != nil {
		return nil, fmt.Errorf("cannot create git archive: %w", err)
	}

	return newBlob(file.Name(), digester.Digest().String())
}

func writeWorktree(ctx context.Context, tw *tar.Writer, root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		if relative == "." {
			return nil
		}

		if relative == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}

			return nil
		}

		info, err := os.Lstat(path)
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}

		header.Name = filepath.ToSlash(relative)
		header.ModTime = time.Time{}
		// Ownership comes from the checkout user and must not reach the digest.
		header.Uid, header.Gid = 0, 0
		header.Uname, header.Gname = "", ""
		switch {
		case info.IsDir():
			return tw.WriteHeader(header)
		case info.Mode().IsRegular():
			if err := tw.WriteHeader(header); err != nil {
				return err
			}

			return writeFile(ctx, tw, path)
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}

			header.Linkname = target
			return tw.WriteHeader(header)
		default:
			return fmt.Errorf("unsupported git worktree file mode %s", info.Mode())
		}
	})
}

func writeFile(ctx context.Context, tw *tar.Writer, path string) (err error) {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	_, err = io.Copy(tw, &contextReader{ctx: ctx, Reader: file})

	return err
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
