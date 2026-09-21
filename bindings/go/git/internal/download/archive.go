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

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

// mediaTypeTar is the media type of the uncompressed archive [archive] writes.
const mediaTypeTar = "application/x-tar"

// archive writes the commit tree as tar, so names, modes and symlinks come from
// git and not from a checkout on the host file system. The archive stays
// uncompressed because its digest is verified on other machines, and the output of
// the standard library compressors is not stable across Go releases.
// Directories get an entry of their own, submodules are not part of the tree content.
// It writes into file, which it closes but never removes; the caller owns it.
// The digest is taken while writing, so no caller has to read the archive back.
func archive(ctx context.Context, commit *object.Commit, file *os.File, opts Options) (_ *filesystem.Blob, _ digest.Digest, err error) {
	tree, err := commit.Tree()
	if err != nil {
		return nil, "", fmt.Errorf("cannot read git tree: %w", err)
	}

	digester := digest.Canonical.Digester()
	limited := &limitedWriter{Writer: file, limit: opts.MaxArchiveSize}
	tw := tar.NewWriter(io.MultiWriter(limited, digester.Hash()))
	err = walkTree(ctx, tree, tw)
	err = errors.Join(err, tw.Close(), file.Close())
	if err != nil {
		return nil, "", fmt.Errorf("cannot create git archive: %w", err)
	}

	b, err := filesystem.GetBlobFromOSPath(file.Name())
	if err != nil {
		return nil, "", fmt.Errorf("cannot open git archive file %q: %w", file.Name(), err)
	}

	b.SetMediaType(mediaTypeTar)

	return b, digester.Digest(), nil
}

// walkTree visits every entry of the tree, subtrees included, in the order git
// stores them. Each name is the path from the root of the commit tree.
func walkTree(ctx context.Context, tree *object.Tree, tw *tar.Writer) error {
	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()

	for {
		name, entry, err := walker.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		if err := writeEntry(tw, tree, name, entry); err != nil {
			return err
		}
	}
}

// writeEntry writes one tree entry. A submodule names a commit in another
// repository and has no content here, so it is left out entirely.
func writeEntry(tw *tar.Writer, tree *object.Tree, name string, entry object.TreeEntry) error {
	header := &tar.Header{Name: name, ModTime: time.Unix(0, 0)}
	switch entry.Mode {
	case filemode.Submodule:
		return nil
	case filemode.Dir:
		header.Typeflag, header.Mode = tar.TypeDir, 0o755
		return tw.WriteHeader(header)
	case filemode.Symlink:
		f, err := tree.TreeEntryFile(&entry)
		if err != nil {
			return err
		}

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

	f, err := tree.TreeEntryFile(&entry)
	if err != nil {
		return err
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
		return 0, fmt.Errorf("git archive exceeds the maximum size of %d bytes", w.limit)
	}

	n, err := w.Writer.Write(p)
	w.written += int64(n)

	return n, err
}
