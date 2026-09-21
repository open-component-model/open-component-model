package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

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
	tw := filesystem.NewTarWriter(io.MultiWriter(limited, digester.Hash()), filesystem.DirOptions{
		Reproducible:         true,
		PreserveSymlinks:     true,
		OmitRoot:             true,
		OmitDirTrailingSlash: true,
	})
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
func walkTree(ctx context.Context, tree *object.Tree, tw *filesystem.TarWriter) error {
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

		if entry.Mode == filemode.Submodule {
			continue
		}
		var size int64
		if entry.Mode != filemode.Dir {
			file, err := tree.TreeEntryFile(&entry)
			if err != nil {
				return err
			}
			size = file.Size
		}
		if err := tw.WriteEntry(ctx, name, treeInfo(name, entry.Mode, size), treeFS{tree: tree}); err != nil {
			return err
		}
	}
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
