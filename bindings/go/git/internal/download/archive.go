package download

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

const (
	mediaTypeTGZ = "application/x-tgz"
	// gitDir is the archive directory holding the Git storage next to the files.
	gitDir = ".git"
)

// archive writes the files of the selected commit and a Git storage with their
// history as a reproducible tar.gz into file and closes it. The storage depends
// on the commit only: its history in one packfile and HEAD detached at the commit.
// The file belongs to the caller.
func archive(ctx context.Context, repo *git.Repository, selected *object.Commit, file *os.File, opts Options) (*filesystem.Blob, digest.Digest, error) {
	tree, err := selected.Tree()
	if err != nil {
		return nil, "", errors.Join(fmt.Errorf("cannot read git tree: %w", err), file.Close())
	}

	storage, err := os.MkdirTemp(opts.TempDir, "ocm-git-carrier-*")
	if err != nil {
		return nil, "", errors.Join(fmt.Errorf("cannot create git carrier storage: %w", err), file.Close())
	}
	defer func() {
		if rmErr := os.RemoveAll(storage); rmErr != nil {
			slog.WarnContext(ctx, "failed to remove temporary git carrier storage", "path", storage, "err", rmErr)
		}
	}()
	if err := writeGitStorage(repo, selected.Hash, storage); err != nil {
		return nil, "", errors.Join(err, file.Close())
	}

	digester := digest.Canonical.Digester()
	limited := &limitedWriter{Writer: file, limit: opts.MaxArchiveSize}
	gz := gzip.NewWriter(io.MultiWriter(limited, digester.Hash()))
	tw := tar.NewWriter(gz)
	err = filesystem.WriteTar(ctx, carrierFS{files: treeFS{tree: tree}, git: filepath.Join(storage, gitDir)}, tw, filesystem.DirOptions{
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

// writeGitStorage initialises a repository in dir, stores the history of commit
// as one packfile with objects in hash order and detaches HEAD at the commit.
func writeGitStorage(source *git.Repository, commit plumbing.Hash, dir string) error {
	target, err := git.PlainInit(dir, false)
	if err != nil {
		return fmt.Errorf("cannot create git carrier storage: %w", err)
	}

	_, objects, err := VerifyObjectClosure(source, commit)
	if err != nil {
		return err
	}
	hashes := slices.SortedFunc(maps.Keys(objects), func(a, b plumbing.Hash) int { return strings.Compare(a.String(), b.String()) })

	packer, ok := target.Storer.(storer.PackfileWriter)
	if !ok {
		return fmt.Errorf("git carrier storage cannot write packfiles")
	}
	pack, err := packer.PackfileWriter()
	if err != nil {
		return fmt.Errorf("cannot create git packfile: %w", err)
	}
	if _, err := packfile.NewEncoder(pack, contentStorer{source.Storer}, false).Encode(hashes, config.DefaultPackWindow); err != nil {
		return errors.Join(fmt.Errorf("cannot write git packfile: %w", err), pack.Close())
	}
	if err := pack.Close(); err != nil {
		return fmt.Errorf("cannot write git packfile: %w", err)
	}

	if err := target.Storer.SetReference(plumbing.NewHashReference(plumbing.HEAD, commit)); err != nil {
		return fmt.Errorf("cannot store git HEAD: %w", err)
	}

	return nil
}

// contentStorer hides the deltas of the source storage from the packfile encoder,
// so the packfile depends on object content and not on how the server packed it.
type contentStorer struct{ storer.EncodedObjectStorer }

// carrierFS serves the commit files with the Git storage directory as .git.
// Storage entries get the same normalized metadata as tree entries.
type carrierFS struct {
	files treeFS
	git   string
}

var (
	_ fs.ReadDirFS  = carrierFS{}
	_ fs.ReadLinkFS = carrierFS{}
)

func (f carrierFS) Open(name string) (fs.File, error) {
	if rel, ok := gitPath(name); ok {
		return os.DirFS(f.git).Open(rel)
	}
	return f.files.Open(name)
}

func (f carrierFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if rel, ok := gitPath(name); ok {
		osEntries, err := fs.ReadDir(os.DirFS(f.git), rel)
		if err != nil {
			return nil, err
		}
		entries := make([]fs.DirEntry, 0, len(osEntries))
		for _, entry := range osEntries {
			info, err := entry.Info()
			if err != nil {
				return nil, err
			}
			if info, err = storageInfo(entry.Name(), info); err != nil {
				return nil, err
			}
			entries = append(entries, fs.FileInfoToDirEntry(info))
		}
		return entries, nil
	}

	entries, err := f.files.ReadDir(name)
	if err != nil || name != "." {
		return entries, err
	}
	if slices.ContainsFunc(entries, func(entry fs.DirEntry) bool { return entry.Name() == gitDir }) {
		return nil, fmt.Errorf("git tree contains a %s entry", gitDir)
	}
	entries = append(entries, fs.FileInfoToDirEntry(treeInfo(gitDir, filemode.Dir, 0)))
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return entries, nil
}

func (f carrierFS) ReadLink(name string) (string, error) {
	if _, ok := gitPath(name); ok {
		return "", &fs.PathError{Op: "readlink", Path: name, Err: fs.ErrInvalid}
	}
	return f.files.ReadLink(name)
}

func (f carrierFS) Lstat(name string) (fs.FileInfo, error) {
	if rel, ok := gitPath(name); ok {
		info, err := fs.Stat(os.DirFS(f.git), rel)
		if err != nil {
			return nil, err
		}
		return storageInfo(path.Base(name), info)
	}
	return f.files.Lstat(name)
}

func gitPath(name string) (string, bool) {
	if name == gitDir {
		return ".", true
	}
	return strings.CutPrefix(name, gitDir+"/")
}

// storageInfo describes a Git storage entry like a tree entry: no owner, epoch
// time, directories 0755 and files 0644.
func storageInfo(name string, info fs.FileInfo) (fs.FileInfo, error) {
	switch {
	case info.IsDir():
		return treeInfo(name, filemode.Dir, 0), nil
	case info.Mode().IsRegular():
		return treeInfo(name, filemode.Regular, info.Size()), nil
	default:
		return nil, fmt.Errorf("unexpected git storage entry %q", name)
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
