package download

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

// mediaTypeTGZ matches the media type of OCM v1 Git access archives.
const mediaTypeTGZ = "application/x-tgz"

// archive writes the commit tree as tar.gz without a host checkout. Content and
// layout match OCM v1, including empty submodule directories, but metadata remains
// normalized. Default gzip output is repeatable within a Go version, not guaranteed
// across Go releases; normalized metadata also precludes legacy digest equivalence.
// It writes into file, which it closes but never removes; the caller owns it.
// The digest is taken while writing, so no caller has to read the archive back.
func archive(ctx context.Context, commit *object.Commit, file *os.File, opts Options) (_ *filesystem.Blob, _ digest.Digest, err error) {
	tree, err := commit.Tree()
	if err != nil {
		return nil, "", errors.Join(fmt.Errorf("cannot read git tree: %w", err), file.Close())
	}

	digester := digest.Canonical.Digester()
	limited := &limitedWriter{Writer: file, limit: opts.MaxArchiveSize}
	gz := gzip.NewWriter(io.MultiWriter(limited, digester.Hash()))
	tw := tar.NewWriter(gz)
	err = walkTree(ctx, tree, "", treeFS{tree: tree}, tw)
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

// walkTree follows the lexical, depth-first filesystem order used by OCM v1,
// not Git's tree order (which compares directories as if suffixed with a slash).
func walkTree(ctx context.Context, tree *object.Tree, prefix string, source treeFS, tw *tar.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	opt := filesystem.DirOptions{
		Reproducible:         true,
		PreserveSymlinks:     true,
		OmitRoot:             true,
		OmitDirTrailingSlash: true,
	}
	entries := slices.Clone(tree.Entries)
	slices.SortFunc(entries, func(a, b object.TreeEntry) int { return strings.Compare(a.Name, b.Name) })
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}

		name := prefix + entry.Name
		var size int64
		if entry.Mode != filemode.Dir && entry.Mode != filemode.Submodule {
			file, err := tree.TreeEntryFile(&entry)
			if err != nil {
				return err
			}
			size = file.Size
		}
		if err := filesystem.WriteTarEntry(ctx, name, treeInfo(name, entry.Mode, size), source, opt, tw); err != nil {
			return err
		}
		// Gitlinks are placeholders only: their target objects need not be present.
		if entry.Mode == filemode.Dir {
			subtree, err := tree.Tree(entry.Name)
			if err != nil {
				return err
			}
			if err := walkTree(ctx, subtree, name+"/", source, tw); err != nil {
				return err
			}
		}
	}
	return nil
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
